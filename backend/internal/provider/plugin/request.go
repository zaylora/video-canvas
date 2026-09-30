package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/http/httpguts"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/provider/netguard"
	"video-canvas/internal/provider/pluginmeta"
)

// 响应类型。
const (
	responseJSON   = "json"
	responseText   = "text"
	responseBinary = "binary"
)

// 请求体类型。
const (
	bodyNone      = ""
	bodyJSON      = "json"
	bodyForm      = "form"
	bodyMultipart = "multipart"
	fileRefPrefix = "input:"
)

// allowedMethods 是请求描述允许的 HTTP 方法。
var allowedMethods = map[string]bool{
	http.MethodGet: true, http.MethodPost: true, http.MethodPut: true, http.MethodPatch: true, http.MethodDelete: true,
}

// forbiddenHeaders 是插件不能自己写的请求头（小写）：鉴权由宿主注入，其余是传输层自己管理的头，
// 插件写了要么能伪造 / 泄露凭证，要么能破坏请求边界（请求走私）。
var forbiddenHeaders = map[string]bool{
	"authorization": true, "proxy-authorization": true, "cookie": true, "host": true,
	"content-length": true, "transfer-encoding": true, "connection": true, "upgrade": true,
}

// requestDesc 是插件 build* 钩子返回的请求描述（契约 §4）。未知字段直接拒绝：插件多写的字段宿主不会发送，
// 静默忽略只会让插件作者以为它生效了。
type requestDesc struct {
	Method       string                     `json:"method"`
	Path         *string                    `json:"path"`
	URL          *string                    `json:"url"`
	Query        map[string]json.RawMessage `json:"query"`
	Headers      map[string]json.RawMessage `json:"headers"`
	JSON         json.RawMessage            `json:"json"`
	Form         json.RawMessage            `json:"form"`
	Multipart    *multipartDesc             `json:"multipart"`
	ResponseType string                     `json:"responseType"`
	Timeout      *float64                   `json:"timeout"`
	Auth         *pluginmeta.Auth           `json:"auth"`
}

type multipartDesc struct {
	Fields json.RawMessage `json:"fields"`
	Parts  []partDesc      `json:"parts"`
}

type partDesc struct {
	Name     string `json:"name"`
	FileRef  string `json:"fileRef"`
	Filename string `json:"filename"`
}

// builtRequest 是通过校验、尚未注入鉴权与文件内容的请求。DryRun 与真实发送共用它，保证“所见即所发”。
type builtRequest struct {
	method       string
	url          *url.URL
	headers      map[string]string // 插件写的头（已校验），不含鉴权与 Content-Type 缺省值
	bodyKind     string
	jsonBody     any         // bodyJSON：解码后的树，文件引用已换成 *fileRef
	formBody     []formField // bodyForm / bodyMultipart 的普通字段
	parts        []partSpec  // bodyMultipart 的文件部分
	responseType string
	timeout      time.Duration
	auth         pluginmeta.Auth // 这次请求生效的注入方式
}

// formField 是一个表单字段：值是标量文本或文件引用。
type formField struct {
	name  string
	value string
	ref   *fileRef
}

type fileRef struct {
	field string
	as    string
}

// partSpec 是 multipart 的一个文件部分。
type partSpec struct {
	name     string
	field    string // 引用的媒体字段名
	filename string
}

// decodeStrict 解码请求描述：未知字段报错、数字保留为 json.Number（大整数不丢精度）。
func decodeStrict(raw []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	return dec.Decode(dst)
}

// decodeTree 解码任意 JSON，数字保留为 json.Number。
func decodeTree(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// buildRequest 校验请求描述并组装请求（契约 §4“宿主对请求描述的校验”）。任何不合规都是插件级失败，此时不发请求。
func (o *operation) buildRequest(raw json.RawMessage) (*builtRequest, error) {
	if isNull(raw) {
		return nil, pluginFault("请求描述不能为空")
	}
	var d requestDesc
	if err := decodeStrict(raw, &d); err != nil {
		return nil, pluginFaultf("请求描述结构不合规：%s", err.Error())
	}
	b := &builtRequest{method: strings.ToUpper(d.Method)}
	if !allowedMethods[b.method] {
		return nil, pluginFaultf("请求描述的 method %q 不合法，只能是 GET / POST / PUT / PATCH / DELETE", d.Method)
	}
	steps := []func(*requestDesc, *builtRequest) error{o.buildURL, o.buildAuth, o.buildHeaders, o.buildBody, o.buildOptions}
	for _, step := range steps {
		if err := step(&d, b); err != nil {
			return nil, err
		}
	}
	return b, nil
}

// buildURL 由 path（相对 base_url）或 url（绝对地址）得到目标 URL，并合并 query。
func (o *operation) buildURL(d *requestDesc, b *builtRequest) error {
	var (
		u   *url.URL
		err error
	)
	switch {
	case d.Path != nil && d.URL != nil:
		return pluginFault("请求描述的 path 与 url 只能二选一")
	case d.Path != nil:
		u, err = o.urlFromPath(*d.Path)
	case d.URL != nil:
		u, err = o.urlFromAbsolute(*d.URL)
	default:
		return pluginFault("请求描述必须有 path 或 url")
	}
	if err != nil {
		return err
	}
	q := u.Query()
	for _, k := range sortedKeys(d.Query) {
		v, skip, err := scalarText(d.Query[k])
		if err != nil {
			return pluginFaultf("请求描述的 query.%s %s", k, err.Error())
		}
		if !skip {
			q.Set(k, v)
		}
	}
	u.RawQuery = q.Encode()
	u.Fragment, u.RawFragment = "", ""
	b.url = u
	return nil
}

// urlFromPath 把 path 拼到 base_url 后面。拼出来的主机与协议必须仍是 base_url 的：防止 path 里的 @、// 等字符把主机换掉。
func (o *operation) urlFromPath(p string) (*url.URL, error) {
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.Contains(p, "\\") {
		return nil, pluginFaultf("请求描述的 path %q 不合法：必须以 / 开头、不能以 // 开头", p)
	}
	base := *o.base
	base.RawQuery, base.Fragment = "", ""
	u, err := url.Parse(strings.TrimRight(base.String(), "/") + p)
	if err != nil {
		return nil, pluginFaultf("请求描述的 path %q 不合法", p)
	}
	if u.Scheme != o.base.Scheme || !strings.EqualFold(u.Host, o.base.Host) || u.User != nil {
		return nil, pluginFault("请求描述的 path 拼接后偏离了渠道 base_url")
	}
	if hasDotDot(u) {
		return nil, pluginFault("请求描述的 path 不能包含 .. 段")
	}
	return u, nil
}

// urlFromAbsolute 校验绝对 URL：只允许 http/https、不含用户名密码、主机在 allowedHosts 或 base_url 的主机上。
func (o *operation) urlFromAbsolute(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, pluginFaultf("请求描述的 url %q 不合法", raw)
	}
	if err := netguard.CheckURLAllowed(o.hosts, u.Scheme, u.Hostname(), u.User != nil); err != nil {
		return nil, &provider.Error{Class: provider.ClassTerminal, Code: provider.CodePluginError, PluginFault: true,
			Message: "请求描述的 url 不允许：" + err.Error(), Cause: redactError(err, o.red.str)}
	}
	if hasDotDot(u) {
		return nil, pluginFault("请求描述的 url 不能包含 .. 段")
	}
	return u, nil
}

// hasDotDot 判断路径里有没有 .. 段（解码前后都查，%2e%2e 也算）。
func hasDotDot(u *url.URL) bool {
	for _, p := range []string{u.Path, u.EscapedPath()} {
		for _, seg := range strings.Split(p, "/") {
			if seg == ".." || strings.EqualFold(seg, "%2e%2e") {
				return true
			}
		}
	}
	return false
}

// buildAuth 确定这次请求的注入方式：请求级 auth 覆盖 meta.auth；请求级只能是 bearer / header / query / none。
func (o *operation) buildAuth(d *requestDesc, b *builtRequest) error {
	b.auth = o.rt.Plugin.Meta.Auth
	if d.Auth == nil {
		return nil
	}
	a := *d.Auth
	switch a.Type {
	case pluginmeta.AuthBearer, pluginmeta.AuthNone:
	case pluginmeta.AuthHeader:
		if !httpguts.ValidHeaderFieldName(a.Name) || forbiddenHeaders[strings.ToLower(a.Name)] {
			return pluginFaultf("请求描述的 auth.name %q 不是可用的请求头名", a.Name)
		}
	case pluginmeta.AuthQuery:
		if strings.TrimSpace(a.Name) == "" {
			return pluginFault("请求描述的 auth.type 为 query 时必须有 auth.name")
		}
	default:
		return pluginFaultf("请求描述的 auth.type %q 不合法，只能是 bearer / header / query / none", a.Type)
	}
	b.auth = a
	return nil
}

// buildHeaders 校验插件写的请求头：黑名单、与注入头同名、非法名字与换行都拒绝。
// 例外：auth: custom 且渠道开启 allow_credentials 的插件本来就持有 Key（签名类鉴权要写 Authorization，如 JWT），
// 允许它自己写 Authorization；这不扩大泄露面，因为请求只能去 base_url 与 allowedHosts。
func (o *operation) buildHeaders(d *requestDesc, b *builtRequest) error {
	injected := map[string]bool{}
	for _, a := range []pluginmeta.Auth{o.rt.Plugin.Meta.Auth, b.auth} {
		if a.Type == pluginmeta.AuthHeader && a.Name != "" {
			injected[strings.ToLower(a.Name)] = true
		}
	}
	b.headers = map[string]string{}
	for _, k := range sortedKeys(d.Headers) {
		lk := strings.ToLower(k)
		selfSigned := lk == "authorization" && o.wantsCredentials() && b.auth.Type != pluginmeta.AuthBearer
		if !httpguts.ValidHeaderFieldName(k) || (forbiddenHeaders[lk] && !selfSigned) || injected[lk] {
			return pluginFaultf("请求描述不能设置请求头 %q", k)
		}
		v, skip, err := scalarText(d.Headers[k])
		if err != nil {
			return pluginFaultf("请求描述的 headers.%s %s", k, err.Error())
		}
		if skip {
			continue
		}
		if !httpguts.ValidHeaderFieldValue(v) {
			return pluginFaultf("请求描述的 headers.%s 含换行或控制字符", k)
		}
		b.headers[http.CanonicalHeaderKey(k)] = v
	}
	return nil
}

// buildOptions 处理 responseType 与 timeout：timeout 缺省用默认值，超过上限按上限。
func (o *operation) buildOptions(d *requestDesc, b *builtRequest) error {
	switch d.ResponseType {
	case "", responseJSON:
		b.responseType = responseJSON
	case responseText, responseBinary:
		b.responseType = d.ResponseType
	default:
		return pluginFaultf("请求描述的 responseType %q 不合法，只能是 json / text / binary", d.ResponseType)
	}
	b.timeout = o.e.opts.DefaultTimeout
	if d.Timeout != nil && *d.Timeout > 0 && !math.IsInf(*d.Timeout, 0) {
		b.timeout = time.Duration(*d.Timeout * float64(time.Second))
	}
	if b.timeout > o.e.opts.MaxTimeout {
		b.timeout = o.e.opts.MaxTimeout
	}
	return nil
}

// buildBody 解析 json / form / multipart（三选一），把文件引用换成 *fileRef，并检查请求体大小（文件内容不计入）。
func (o *operation) buildBody(d *requestDesc, b *builtRequest) error {
	n := 0
	for _, has := range []bool{!isNull(d.JSON), !isNull(d.Form), d.Multipart != nil} {
		if has {
			n++
		}
	}
	if n > 1 {
		return pluginFault("请求描述的 json / form / multipart 只能三选一")
	}
	var err error
	switch {
	case !isNull(d.JSON):
		b.bodyKind = bodyJSON
		err = o.buildJSONBody(d.JSON, b)
	case !isNull(d.Form):
		b.bodyKind = bodyForm
		b.formBody, err = o.buildFormFields("form", d.Form)
	case d.Multipart != nil:
		b.bodyKind = bodyMultipart
		err = o.buildMultipart(d.Multipart, b)
	}
	if err != nil {
		return err
	}
	if size := b.plainBodySize(); size > o.e.opts.MaxRequestBodyBytes {
		return pluginFaultf("请求体编码后 %d 字节，超过上限 %d 字节", size, o.e.opts.MaxRequestBodyBytes)
	}
	return nil
}

func (o *operation) buildJSONBody(raw json.RawMessage, b *builtRequest) error {
	tree, err := decodeTree(raw)
	if err != nil {
		return pluginFaultf("请求描述的 json 不合法：%s", err.Error())
	}
	b.jsonBody, err = o.parseRefs(tree, "json")
	return err
}

func (o *operation) parseRefs(value any, path string) (any, error) {
	if ref, ok, err := o.parseRefValue(value, path); ok || err != nil {
		return ref, err
	}
	switch v := value.(type) {
	case []any:
		out := make([]any, len(v))
		for i := range v {
			item, err := o.parseRefs(v[i], fmt.Sprintf("%s[%d]", path, i))
			if err != nil {
				return nil, err
			}
			out[i] = item
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, item := range v {
			parsed, err := o.parseRefs(item, path+"."+k)
			if err != nil {
				return nil, err
			}
			out[k] = parsed
		}
		return out, nil
	default:
		return value, nil
	}
}

func (o *operation) parseRefRaw(raw json.RawMessage, path string) (*fileRef, bool, error) {
	value, err := decodeTree(raw)
	if err != nil {
		return nil, false, pluginFaultf("请求描述的 %s 不是合法 JSON：%s", path, err.Error())
	}
	ref, ok, err := o.parseRefValue(value, path)
	return ref, ok, err
}

func (o *operation) parseRefValue(value any, path string) (*fileRef, bool, error) {
	obj, ok := value.(map[string]any)
	if !ok {
		return nil, false, nil
	}
	raw, exists := obj["__fileRef"]
	if !exists {
		return nil, false, nil
	}
	if len(obj) > 2 {
		return nil, true, pluginFaultf("请求描述的 %s 文件引用含未知字段", path)
	}
	field, ok := raw.(string)
	if !ok {
		return nil, true, pluginFaultf("请求描述的 %s.__fileRef 必须是字符串", path)
	}
	if !strings.HasPrefix(field, fileRefPrefix) {
		return nil, true, pluginFaultf("请求描述的 %s.__fileRef 必须是 input:<字段名>", path)
	}
	field = strings.TrimPrefix(field, fileRefPrefix)
	if _, ok := o.media[field]; !ok {
		return nil, true, pluginFaultf("请求描述的 %s 引用了未填写的媒体字段", path)
	}
	as := "url"
	if rawAs, exists := obj["as"]; exists {
		var ok bool
		as, ok = rawAs.(string)
		if !ok || (as != "url" && as != "base64" && as != "dataUrl") {
			return nil, true, pluginFaultf("请求描述的 %s.as 必须是 url / base64 / dataUrl", path)
		}
	}
	return &fileRef{field: field, as: as}, true, nil
}

// buildFormFields 解析 form / multipart.fields：值只能是字符串、数字、布尔或文件引用，null 的键被忽略。
func (o *operation) buildFormFields(path string, raw json.RawMessage) ([]formField, error) {
	if isNull(raw) {
		return nil, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, pluginFaultf("请求描述的 %s 必须是对象", path)
	}
	out := make([]formField, 0, len(m))
	for _, k := range sortedKeys(m) {
		if ref, isRef, err := o.parseRefRaw(m[k], path+"."+k); isRef || err != nil {
			if err != nil {
				return nil, err
			}
			out = append(out, formField{name: k, ref: ref})
			continue
		}
		v, skip, err := scalarText(m[k])
		if err != nil {
			return nil, pluginFaultf("请求描述的 %s.%s %s", path, k, err.Error())
		}
		if !skip {
			out = append(out, formField{name: k, value: v})
		}
	}
	return out, nil
}

func (o *operation) buildMultipart(m *multipartDesc, b *builtRequest) error {
	var err error
	if b.formBody, err = o.buildFormFields("multipart.fields", m.Fields); err != nil {
		return err
	}
	for i, p := range m.Parts {
		path := fmt.Sprintf("multipart.parts[%d]", i)
		if strings.TrimSpace(p.Name) == "" || strings.ContainsAny(p.Name+p.Filename, "\r\n") {
			return pluginFaultf("请求描述的 %s 的 name 不能为空，name / filename 不能含换行", path)
		}
		field, err := o.refField(p.FileRef, path+".fileRef")
		if err != nil {
			return err
		}
		b.parts = append(b.parts, partSpec{name: p.Name, field: field, filename: p.Filename})
	}
	return nil
}

func (o *operation) refField(raw, path string) (string, error) {
	if !strings.HasPrefix(raw, fileRefPrefix) {
		return "", pluginFaultf("请求描述的 %s 必须是 input:<字段名>", path)
	}
	field := strings.TrimPrefix(raw, fileRefPrefix)
	if _, ok := o.media[field]; !ok {
		return "", pluginFaultf("请求描述的 %s 引用了未填写的媒体字段", path)
	}
	return field, nil
}

// plainBodySize 是请求体不含文件内容时的编码大小：文件引用按它在请求描述里的原样计入。
func (b *builtRequest) plainBodySize() int64 {
	switch b.bodyKind {
	case bodyJSON:
		raw, err := json.Marshal(b.jsonBody)
		if err != nil {
			return 0
		}
		return int64(len(raw))
	case bodyForm, bodyMultipart:
		var n int64
		for _, f := range b.formBody {
			n += int64(len(f.name) + len(f.value) + 2)
			if f.ref != nil {
				n += int64(len(f.ref.field) + len(fileRefPrefix))
			}
		}
		return n
	}
	return 0
}

// scalarText 把 query / header / form 的值转成文本：只接受字符串、数字、布尔；null 返回 skip=true。
func scalarText(raw json.RawMessage) (text string, skip bool, err error) {
	v, err := decodeTree(raw)
	if err != nil {
		return "", false, fmt.Errorf("不是合法的 JSON")
	}
	switch t := v.(type) {
	case nil:
		return "", true, nil
	case string:
		return t, false, nil
	case json.Number, bool:
		return modelcfg.Stringify(t), false, nil
	}
	return "", false, fmt.Errorf("只能是字符串、数字或布尔")
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
