// 本文件：插件 meta 的上传预检规则（契约 backend/docs/plugin-contract.md §2）。
// 解码与校验一起做：每个字段单独解码，类型不对也能报出精确路径，所有问题一次报出。

package pluginmeta

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/provider/pluginproto"
)

// 预检用的其余限制。
const (
	MaxMetaBytes   = 64 << 10 // meta 编码后上限 64KB（契约 §2）
	MaxNameLen     = 128      // meta.name 字符数上限，与 ai_plugins.name 列宽一致
	MaxVersionLen  = 32       // meta.version 长度上限，与 ai_plugin_versions.version 列宽一致
	maxAuthNameLen = 64       // auth.name（头名 / 查询参数名）长度上限
)

var (
	keyRe         = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,29}$`)
	semverRe      = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)
	settingNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,31}$`)
	// headerNameRe 是 RFC 7230 的 token 字符集。
	headerNameRe = regexp.MustCompile("^[!#$%&'*+\\-.^_`|~0-9A-Za-z]+$")
)

// jsonObject 是解码到一半的 JSON 对象。
type jsonObject = map[string]json.RawMessage

// Validate 是上传预检里对 meta 与钩子的规则检查，runner 的预检接口调用它。raw 是插件导出的 meta（JSON），
// hooks 是插件导出的钩子名（契约不认识的名字会被忽略：多余导出只警告不拒绝）。
// 所有问题一次报出，Path 精确到 meta 字段（如 "meta.endpoints.video.mode"）；与钩子本身有关、
// 又对不上某个 meta 字段的问题用 "exports.<钩子名>" 作路径。
// 返回的 Meta 在有问题时可能不完整，只供展示；raw 不是对象时返回 nil。版本号是否已被占用由 service 层检查。
func Validate(raw []byte, hooks []string) (*Meta, []modelcfg.Issue) {
	v := &validator{hooks: map[string]bool{}}
	for _, h := range hooks {
		v.hooks[h] = true
	}
	obj := v.rootObject(raw)
	if obj == nil {
		return nil, v.issues
	}
	m := &Meta{}
	v.identity(obj, m)
	v.auth(obj, m)
	v.endpoints(obj, m)
	v.allowedHosts(obj, m)
	m.ChannelSettings = v.settings("meta.channelSettings", obj["channelSettings"])
	v.importMeta(obj, m)
	v.poll(obj, m)
	v.hookPairs()
	return m, v.issues
}

type validator struct {
	hooks  map[string]bool
	issues []modelcfg.Issue
}

func (v *validator) add(path, format string, args ...any) {
	v.issues = append(v.issues, modelcfg.Issue{Path: path, Message: fmt.Sprintf(format, args...)})
}

// rootObject 检查 meta 是 JSON 对象且不超过大小上限，返回顶层字段。
func (v *validator) rootObject(raw []byte) jsonObject {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		v.add("meta", "插件必须导出 meta 对象")
		return nil
	}
	var obj jsonObject
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		v.add("meta", "meta 不是合法的 JSON 对象")
		return nil
	}
	// 超限只记一条问题，其余字段照常检查，方便一次改完
	if len(trimmed) > MaxMetaBytes {
		v.add("meta", "meta 编码后超过 %dKB", MaxMetaBytes>>10)
	}
	return obj
}

// field 解码一个字段到 dst。字段不存在或为 null 返回 false；类型不对记一条问题并返回 false。
func (v *validator) field(obj jsonObject, path, name string, dst any) bool {
	raw, ok := obj[name]
	if !ok || isNull(raw) {
		return false
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		v.add(path+"."+name, "类型不对，应为%s", typeName(dst))
		return false
	}
	return true
}

// object 把一个字段解码成对象；不存在返回 nil, false；不是对象记一条问题。
func (v *validator) object(raw json.RawMessage, path string) (jsonObject, bool) {
	if len(raw) == 0 || isNull(raw) {
		return nil, false
	}
	var obj jsonObject
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		v.add(path, "类型不对，应为对象")
		return nil, false
	}
	return obj, true
}

// identity 检查 apiVersion、key、name、version、description。
func (v *validator) identity(obj jsonObject, m *Meta) {
	if !v.field(obj, "meta", "apiVersion", &m.APIVersion) {
		if _, present := obj["apiVersion"]; !present || isNull(obj["apiVersion"]) {
			v.add("meta.apiVersion", "缺少 apiVersion，当前只支持 %d", APIVersion)
		}
	} else if m.APIVersion != APIVersion {
		v.add("meta.apiVersion", "不支持的 apiVersion：%d，当前只支持 %d", m.APIVersion, APIVersion)
	}

	if v.required(obj, "key", &m.Key) && !keyRe.MatchString(m.Key) {
		v.add("meta.key", "只能包含小写字母、数字和连字符，以字母或数字开头，最长 %d 个字符", MaxKeyLen)
	}
	if v.required(obj, "name", &m.Name) {
		if strings.TrimSpace(m.Name) == "" {
			v.add("meta.name", "不能为空")
		} else if utf8.RuneCountInString(m.Name) > MaxNameLen {
			v.add("meta.name", "最长 %d 个字符", MaxNameLen)
		}
	}
	if v.required(obj, "version", &m.Version) {
		if len(m.Version) > MaxVersionLen {
			v.add("meta.version", "最长 %d 个字符", MaxVersionLen)
		} else if !semverRe.MatchString(m.Version) {
			v.add("meta.version", "必须是 semver 版本号，如 1.2.3 或 1.2.3-rc.1")
		}
	}
	v.field(obj, "meta", "description", &m.Description)
}

// required 解码一个必填的字符串字段；缺失时记一条问题。
func (v *validator) required(obj jsonObject, name string, dst *string) bool {
	if v.field(obj, "meta", name, dst) {
		return true
	}
	if raw, present := obj[name]; !present || isNull(raw) {
		v.add("meta."+name, "缺少 %s", name)
	}
	return false
}

// auth 检查鉴权声明：type 是枚举值；header / query 必须有合法的 name。
func (v *validator) auth(obj jsonObject, m *Meta) {
	a, ok := v.object(obj["auth"], "meta.auth")
	if !ok {
		if _, present := obj["auth"]; !present || isNull(obj["auth"]) {
			v.add("meta.auth", "缺少 auth，可选：%s", strings.Join(AuthTypes, " / "))
		}
		return
	}
	if !v.field(a, "meta.auth", "type", &m.Auth.Type) && isPresent(a, "type") {
		return // 类型不对，field 已记过问题
	}
	v.field(a, "meta.auth", "name", &m.Auth.Name)
	if !contains(AuthTypes, m.Auth.Type) {
		v.add("meta.auth.type", "必须是 %s 之一", strings.Join(AuthTypes, " / "))
		return
	}
	switch m.Auth.Type {
	case AuthHeader:
		if !headerNameRe.MatchString(m.Auth.Name) || len(m.Auth.Name) > maxAuthNameLen {
			v.add("meta.auth.name", "auth.type 为 header 时必须填写合法的请求头名（最长 %d 个字符）", maxAuthNameLen)
		}
	case AuthQuery:
		if strings.TrimSpace(m.Auth.Name) == "" || len(m.Auth.Name) > maxAuthNameLen {
			v.add("meta.auth.name", "auth.type 为 query 时必须填写查询参数名（最长 %d 个字符）", maxAuthNameLen)
		}
	}
}

// endpoints 检查 endpoint 声明，并按 mode 检查必须实现的钩子。
func (v *validator) endpoints(obj jsonObject, m *Meta) {
	eps, ok := v.object(obj["endpoints"], "meta.endpoints")
	if (ok && len(eps) == 0) || !isPresent(obj, "endpoints") {
		v.add("meta.endpoints", "至少声明一种 endpoint（%s）", strings.Join(modelcfg.Kinds, " / "))
	}
	m.Endpoints = map[string]Endpoint{}
	for _, kind := range sortedKeys(eps) {
		path := "meta.endpoints." + kind
		if !contains(modelcfg.Kinds, kind) {
			v.add(path, "不支持的 kind，只能是 %s", strings.Join(modelcfg.Kinds, " / "))
			continue
		}
		e, ok := v.object(eps[kind], path)
		if !ok {
			if isNull(eps[kind]) {
				v.add(path, "类型不对，应为对象")
			}
			continue
		}
		var ep Endpoint
		if !v.field(e, path, "mode", &ep.Mode) && isPresent(e, "mode") {
			continue // 类型不对，field 已记过问题
		}
		m.Endpoints[kind] = ep
		switch ep.Mode {
		case ModeSync:
		case ModeAsync:
			for _, h := range []string{pluginproto.HookBuildQueryRequest, pluginproto.HookParseQueryResponse} {
				if !v.hooks[h] {
					v.add(path+".mode", "%s 声明为 async，必须导出 %s", kind, h)
				}
			}
		default:
			v.add(path+".mode", "必须是 %s 或 %s", ModeSync, ModeAsync)
		}
	}
	// 任何 endpoint 都要走提交：没有提交钩子的插件什么也做不了
	for _, h := range []string{pluginproto.HookBuildSubmitRequest, pluginproto.HookParseSubmitResponse} {
		if !v.hooks[h] {
			v.add("exports."+h, "缺少钩子 %s：每种 endpoint 都要实现提交", h)
		}
	}
}

// allowedHosts 检查结果下载域名白名单：每项是小写域名或 *.example.com，不含 IP 与过宽通配。
func (v *validator) allowedHosts(obj jsonObject, m *Meta) {
	var raws []json.RawMessage
	if !v.field(obj, "meta", "allowedHosts", &raws) {
		return
	}
	for i, raw := range raws {
		path := fmt.Sprintf("meta.allowedHosts[%d]", i)
		var h string
		if err := json.Unmarshal(raw, &h); err != nil {
			v.add(path, "类型不对，应为字符串")
			continue
		}
		if err := modelcfg.ValidateHostPattern(h); err != nil {
			v.add(path, "%s", err.Error())
			continue
		}
		m.AllowedHosts = append(m.AllowedHosts, h)
	}
}

// importMeta 检查“从渠道导入模型”的参数表单声明。
func (v *validator) importMeta(obj jsonObject, m *Meta) {
	im, ok := v.object(obj["import"], "meta.import")
	if !ok {
		return
	}
	m.Import = &ImportMeta{Args: v.settings("meta.import.args", im["args"])}
}

// poll 检查轮询节奏：各值非负，jitter 在 0–1。
func (v *validator) poll(obj jsonObject, m *Meta) {
	p, ok := v.object(obj["poll"], "meta.poll")
	if !ok {
		return
	}
	m.Poll = &PollMeta{}
	fields := []struct {
		name string
		dst  *float64
	}{
		{"firstDelay", &m.Poll.FirstDelay}, {"interval", &m.Poll.Interval},
		{"maxInterval", &m.Poll.MaxInterval}, {"jitter", &m.Poll.Jitter},
	}
	for _, f := range fields {
		if v.field(p, "meta.poll", f.name, f.dst) && *f.dst < 0 {
			v.add("meta.poll."+f.name, "不能为负数")
		}
	}
	if m.Poll.Jitter > 1 {
		v.add("meta.poll.jitter", "必须在 0 到 1 之间")
	}
}

// hookPairs 检查成对的钩子：准备阶段有 build 必须有 parse；导入的两个钩子必须同时存在。
func (v *validator) hookPairs() {
	if v.hooks[pluginproto.HookBuildPrepareRequests] && !v.hooks[pluginproto.HookParsePrepareResponse] {
		v.add("exports."+pluginproto.HookParsePrepareResponse, "实现了 %s 就必须同时实现 %s",
			pluginproto.HookBuildPrepareRequests, pluginproto.HookParsePrepareResponse)
	}
	build, parse := v.hooks[pluginproto.HookBuildImportRequest], v.hooks[pluginproto.HookParseImportResponse]
	if build && !parse {
		v.add("exports."+pluginproto.HookParseImportResponse, "%s 与 %s 必须同时实现",
			pluginproto.HookBuildImportRequest, pluginproto.HookParseImportResponse)
	}
	if parse && !build {
		v.add("exports."+pluginproto.HookBuildImportRequest, "%s 与 %s 必须同时实现",
			pluginproto.HookBuildImportRequest, pluginproto.HookParseImportResponse)
	}
}

func isNull(raw json.RawMessage) bool { return bytes.Equal(bytes.TrimSpace(raw), []byte("null")) }

func isPresent(obj jsonObject, name string) bool {
	raw, ok := obj[name]
	return ok && !isNull(raw)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func sortedKeys(obj jsonObject) []string {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// typeName 是类型不对时提示里的期望类型。
func typeName(dst any) string {
	switch dst.(type) {
	case *string:
		return "字符串"
	case *int:
		return "整数"
	case *float64:
		return "数字"
	case *bool:
		return "布尔值"
	case *[]string:
		return "字符串数组"
	case *[]json.RawMessage:
		return "数组"
	default:
		return "对象"
	}
}
