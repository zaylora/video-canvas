// 本文件：平台协议与模型配置的语义校验：解析 JSON 后补默认值，逐项检查鉴权、轮询、状态映射、操作（operation）、
// 输入 schema，以及模型与平台之间的引用是否一致，所有问题一次报出。

package dsl

import (
	"fmt"
	"math"
	"net"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"time"
)

var (
	providerKeyRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	modelKeyRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
	fieldNameRe   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
	headerNameRe  = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")
)

// 默认值。
const (
	DefaultModelDeadline = 30 * time.Minute
	maxOperationTimeout  = 10 * time.Minute
	maxModelDeadline     = 24 * time.Hour
)

var (
	validMethods       = []string{"GET", "POST", "PUT", "PATCH", "DELETE"}
	validAuthTypes     = []string{AuthNone, AuthBearer, AuthHeader, AuthQuery, AuthBodyField}
	validEncodings     = []string{EncodingJSON, EncodingMultipart}
	validStatuses      = []string{"queued", "running", "succeeded", "failed"}
	validErrorClasses  = []string{"retryable", "terminal", "moderation", "provider_balance"}
	validKinds         = []string{"video", "image", "audio"}
	validFieldTypes    = []string{FieldText, FieldNumber, FieldEnum, FieldBoolean, FieldImage, FieldVideo, FieldAudio}
	validPorts         = []string{"text", "image", "video", "audio"}
	forbiddenHeaders   = []string{"host", "content-length", "transfer-encoding", "connection"}
	submitExtractKeys  = []string{"provider_task_id", "error_code", "error_message"}
	queryExtractKeys   = []string{"status", "outputs", "error_code", "error_message", "provider_cost", "progress"}
	uploadExtractKeys  = []string{"ref"}
	cancelExtractKeys  = []string{}
	requiredSubmitKeys = []string{"provider_task_id"}
	requiredQueryKeys  = []string{"status", "outputs"}
	requiredUploadKeys = []string{"ref"}
)

func inStrings(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func oneOfMsg(list []string) string { return "必须是 " + strings.Join(list, " / ") + " 之一" }

// ---------- Provider ----------

// parseProvider 解析并校验 Provider 配置正文。
func parseProvider(body []byte) (*ProviderConfig, []Issue) {
	tree, iss := decodeTree(body)
	if iss != nil {
		return nil, []Issue{*iss}
	}
	var issues []Issue
	checkShape("", tree, reflect.TypeOf(ProviderConfig{}), &issues)
	if len(issues) > 0 {
		return nil, issues
	}
	var cfg ProviderConfig
	if err := decodeInto(body, &cfg); err != nil {
		return nil, []Issue{{Message: "配置解析失败：" + err.Error()}}
	}
	cfg.normalize()
	validateProvider(&cfg, &issues)
	if len(issues) > 0 {
		return nil, issues
	}
	return &cfg, nil
}

// normalize 规范化 any 类型字段里的数字，并把 method / encoding 补成规范写法。
func (p *ProviderConfig) normalize() {
	for _, op := range []*Operation{p.Operations.Upload, p.Operations.Submit, p.Operations.Query, p.Operations.Cancel} {
		if op == nil {
			continue
		}
		op.Body = normalizeConfigNumber(op.Body)
		op.Method = strings.ToUpper(strings.TrimSpace(op.Method))
		if op.Encoding.Type == "" {
			op.Encoding.Type = EncodingJSON
		}
	}
}

func validateProvider(p *ProviderConfig, issues *[]Issue) {
	add := func(path, msg string) { *issues = append(*issues, Issue{Path: path, Message: msg}) }

	if p.DSL != Version {
		add("dsl", fmt.Sprintf("必须等于 %d（当前支持的 DSL 版本）", Version))
	}
	if !providerKeyRe.MatchString(p.Key) {
		add("key", "必填，只能包含小写字母、数字、下划线和连字符，且以字母或数字开头（最长 64 位）")
	}
	if strings.TrimSpace(p.Name) == "" {
		add("name", "不能为空")
	}

	// allowed_hosts
	if len(p.AllowedHosts) == 0 {
		add("allowed_hosts", "至少填写一个允许访问的域名")
	}
	seenHost := map[string]bool{}
	for i, h := range p.AllowedHosts {
		path := fmt.Sprintf("allowed_hosts[%d]", i)
		if err := ValidateHostPattern(h); err != nil {
			add(path, err.Error())
			continue
		}
		if seenHost[h] {
			add(path, "重复的域名")
		}
		seenHost[h] = true
	}

	// base_url 本身也要在白名单内
	validateBaseURL(p, add)

	validateAuth(p.Auth, add)

	if p.RateLimit.RPS < 0 || p.RateLimit.RPS > 1000 || math.IsNaN(p.RateLimit.RPS) {
		add("rate_limit.rps", "必须在 0–1000 之间（0 表示不限速）")
	}
	if p.RateLimit.MaxConcurrency < 0 || p.RateLimit.MaxConcurrency > 1000 {
		add("rate_limit.max_concurrency", "必须在 0–1000 之间（0 表示不限制）")
	}

	validatePoll(p.Poll, add)

	// operations
	ops := p.Operations
	if ops.Submit == nil {
		add("operations.submit", "必须配置提交操作")
	}
	if ops.Query == nil {
		add("operations.query", "必须配置查询操作")
	}
	if ops.Upload != nil {
		validateOperation("upload", ops.Upload, issues)
	}
	if ops.Submit != nil {
		validateOperation("submit", ops.Submit, issues)
	}
	if ops.Query != nil {
		validateOperation("query", ops.Query, issues)
	}
	if ops.Cancel != nil {
		validateOperation("cancel", ops.Cancel, issues)
	}

	validateStatusMap(p.StatusMap, add)

	for i, r := range p.ErrorRules {
		base := fmt.Sprintf("error_rules[%d]", i)
		if strings.TrimSpace(r.When) == "" {
			add(base+".when", "不能为空")
		} else {
			checkExprSource(base+".when", r.When, stageResponse, issues, nil)
		}
		if !inStrings(validErrorClasses, r.Class) {
			add(base+".class", oneOfMsg(validErrorClasses))
		}
	}

	if p.Webhook != nil {
		if p.Webhook.Verify.Type != "path_secret" {
			add("webhook.verify.type", "目前只支持 path_secret")
		}
		if strings.TrimSpace(p.Webhook.TaskID) == "" {
			add("webhook.task_id", "不能为空，需要一个从回调体 req 里取平台任务 id 的表达式")
		} else {
			checkExprSource("webhook.task_id", p.Webhook.TaskID, stageWebhook, issues, nil)
		}
	}
}

func validateBaseURL(p *ProviderConfig, add func(path, msg string)) {
	if strings.TrimSpace(p.BaseURL) == "" {
		add("base_url", "不能为空")
		return
	}
	u, err := url.Parse(p.BaseURL)
	if err != nil || u.Host == "" {
		add("base_url", "不是合法的 URL")
		return
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		add("base_url", "协议必须是 https（或 http）")
		return
	}
	if u.User != nil {
		add("base_url", "不能包含用户名密码")
		return
	}
	if u.RawQuery != "" || u.Fragment != "" {
		add("base_url", "不能包含查询参数或锚点")
		return
	}
	if strings.Contains(p.BaseURL, "${") {
		add("base_url", "不能使用表达式")
		return
	}
	host := u.Hostname()
	if net.ParseIP(host) != nil {
		add("base_url", "不能使用 IP 地址，请使用域名")
		return
	}
	if len(p.AllowedHosts) > 0 && !MatchHost(p.AllowedHosts, host) {
		add("base_url", fmt.Sprintf("域名 %s 不在 allowed_hosts 白名单内", host))
	}
}

func validateAuth(a AuthConfig, add func(path, msg string)) {
	if !inStrings(validAuthTypes, a.Type) {
		add("auth.type", oneOfMsg(validAuthTypes))
		return
	}
	switch a.Type {
	case AuthNone:
		if a.Secret != "" {
			add("auth.secret", "auth.type 为 none 时不需要凭证")
		}
	case AuthBearer:
		if a.Secret == "" {
			add("auth.secret", "不能为空，填写凭证的名字")
		}
	case AuthHeader, AuthQuery, AuthBodyField:
		if a.Secret == "" {
			add("auth.secret", "不能为空，填写凭证的名字")
		}
		if a.Name == "" {
			add("auth.name", map[string]string{
				AuthHeader: "不能为空，填写请求头名称", AuthQuery: "不能为空，填写查询参数名", AuthBodyField: "不能为空，填写请求体字段名",
			}[a.Type])
		} else if a.Type == AuthHeader && !headerNameRe.MatchString(a.Name) {
			add("auth.name", "不是合法的请求头名称")
		}
	}
	if strings.Contains(a.Secret, "${") {
		add("auth.secret", "只能是凭证名字，不能使用表达式")
	}
}

func validatePoll(p PollConfig, add func(path, msg string)) {
	for _, d := range []struct {
		path string
		v    Duration
	}{{"poll.first_delay", p.FirstDelay}, {"poll.interval", p.Interval}, {"poll.max_interval", p.MaxInterval}} {
		if d.v < 0 || d.v.D() > time.Hour {
			add(d.path, "必须在 0 到 1h 之间")
		}
	}
	if p.Interval > 0 && p.MaxInterval > 0 && p.MaxInterval < p.Interval {
		add("poll.max_interval", "不能小于 poll.interval")
	}
	if p.Jitter < 0 || p.Jitter > 1 || math.IsNaN(p.Jitter) {
		add("poll.jitter", "必须在 0–1 之间")
	}
}

func validateStatusMap(m map[string]string, add func(path, msg string)) {
	if len(m) == 0 {
		add("status_map", "不能为空，至少需要映射 succeeded 与 failed")
		return
	}
	hasOK, hasFail := false, false
	for _, k := range sortedKeys(m) {
		v := m[k]
		if k == "" {
			add("status_map", "平台状态名不能为空")
			continue
		}
		if !inStrings(validStatuses, v) {
			add(joinPath("status_map", k), oneOfMsg(validStatuses))
			continue
		}
		hasOK = hasOK || v == "succeeded"
		hasFail = hasFail || v == "failed"
	}
	if !hasOK {
		add("status_map", "没有任何平台状态映射到 succeeded，任务永远无法成功")
	}
	if !hasFail {
		add("status_map", "没有任何平台状态映射到 failed，任务失败时会一直轮询")
	}
}

// validateOperation 校验一个操作；name 为 upload / submit / query / cancel。
func validateOperation(name string, op *Operation, issues *[]Issue) {
	base := "operations." + name
	add := func(path, msg string) { *issues = append(*issues, Issue{Path: path, Message: msg}) }

	if op.Method == "" {
		add(base+".method", "不能为空")
	} else if !inStrings(validMethods, op.Method) {
		add(base+".method", oneOfMsg(validMethods))
	}

	stage := stageQuery
	switch name {
	case "upload":
		stage = stageUpload
	case "submit":
		stage = stageSubmit
	}

	// path：必须以 / 开头，保证最终 URL 一定落在 base_url 的域名下
	if !strings.HasPrefix(op.Path, "/") {
		add(base+".path", "必须以 / 开头（相对 base_url 的路径）")
	} else {
		checkTemplate(base+".path", op.Path, stage, issues, nil)
	}

	for _, k := range sortedKeys(op.Headers) {
		hp := joinPath(base+".headers", k)
		if !headerNameRe.MatchString(k) {
			add(hp, "不是合法的请求头名称")
			continue
		}
		if inStrings(forbiddenHeaders, strings.ToLower(k)) {
			add(hp, "不允许自定义该请求头")
			continue
		}
		checkTemplate(hp, op.Headers[k], stage, issues, nil)
	}
	for _, k := range sortedKeys(op.Query) {
		checkTemplate(joinPath(base+".query", k), op.Query[k], stage, issues, nil)
	}

	// encoding
	if !inStrings(validEncodings, op.Encoding.Type) {
		add(base+".encoding.type", oneOfMsg(validEncodings))
	}
	if name == "upload" {
		if op.Encoding.Type != EncodingMultipart {
			add(base+".encoding.type", "上传操作必须是 multipart")
		}
		if op.Encoding.FileField == "" {
			add(base+".encoding.file_field", "不能为空，填写文件所在的表单字段名")
		}
	}

	// body
	if op.Body != nil {
		if op.Method == "GET" {
			add(base+".body", "GET 请求不能有请求体")
		}
		if op.Encoding.Type == EncodingMultipart {
			if _, ok := op.Body.(map[string]any); !ok {
				add(base+".body", "multipart 的 body 必须是对象（每个键是一个表单字段）")
			}
		}
		checkTemplate(base+".body", op.Body, stage, issues, nil)
	}

	// success
	if strings.TrimSpace(op.Success) == "" {
		if name != "cancel" {
			add(base+".success", "不能为空，需要一个返回 bool 的表达式")
		}
	} else {
		checkExprSource(base+".success", op.Success, stageResponse, issues, nil)
	}

	// extract
	var allowed, required []string
	switch name {
	case "upload":
		allowed, required = uploadExtractKeys, requiredUploadKeys
	case "submit":
		allowed, required = submitExtractKeys, requiredSubmitKeys
	case "query":
		allowed, required = queryExtractKeys, requiredQueryKeys
	case "cancel":
		allowed, required = cancelExtractKeys, nil
	}
	for _, k := range sortedKeys(op.Extract) {
		ep := joinPath(base+".extract", k)
		if !inStrings(allowed, k) {
			msg := "未知的提取项"
			if len(allowed) > 0 {
				msg += "（可用：" + strings.Join(allowed, "、") + "）"
			} else {
				msg += "，该操作不需要 extract"
			}
			add(ep, msg)
			continue
		}
		if strings.TrimSpace(op.Extract[k]) == "" {
			add(ep, "不能为空")
			continue
		}
		checkExprSource(ep, op.Extract[k], stageResponse, issues, nil)
	}
	for _, k := range required {
		if _, ok := op.Extract[k]; !ok {
			add(joinPath(base+".extract", k), "必填")
		}
	}

	if op.Timeout < 0 || op.Timeout.D() > maxOperationTimeout {
		add(base+".timeout", "必须在 0 到 10m 之间（0 表示默认 30s）")
	}
}

// ---------- Model ----------

// parseModel 解析并校验 Model 配置正文；provider 非 nil 时做跨对象校验。
func parseModel(body []byte, provider *ProviderConfig) (*ModelConfig, []Issue) {
	tree, iss := decodeTree(body)
	if iss != nil {
		return nil, []Issue{*iss}
	}
	var issues []Issue
	checkShape("", tree, reflect.TypeOf(ModelConfig{}), &issues)
	if len(issues) > 0 {
		return nil, issues
	}
	var cfg ModelConfig
	if err := decodeInto(body, &cfg); err != nil {
		return nil, []Issue{{Message: "配置解析失败：" + err.Error()}}
	}
	cfg.normalize()
	validateModel(&cfg, provider, &issues)
	if len(issues) > 0 {
		return nil, issues
	}
	return &cfg, nil
}

// normalize 规范化 any 字段里的数字，并补默认值：deadline 30m、output.media 取模型 kind、output.select 取全部产物。
func (m *ModelConfig) normalize() {
	m.Params = normalizeConfigNumber(m.Params).(map[string]any)
	m.Mapping = normalizeConfigNumber(m.Mapping)
	for i := range m.InputSchema {
		f := &m.InputSchema[i].InputField
		f.Default = normalizeConfigNumber(f.Default)
		for j := range f.Options {
			f.Options[j].Value = normalizeConfigNumber(f.Options[j].Value)
		}
	}
	if m.Deadline == 0 {
		m.Deadline = Duration(DefaultModelDeadline)
	}
	if m.Output.Media == "" {
		m.Output.Media = m.Kind
	}
	if strings.TrimSpace(m.Output.Select) == "" {
		m.Output.Select = "outputs"
	}
}

func validateModel(m *ModelConfig, provider *ProviderConfig, issues *[]Issue) {
	add := func(path, msg string) { *issues = append(*issues, Issue{Path: path, Message: msg}) }

	if !modelKeyRe.MatchString(m.Key) {
		add("key", "必填，只能包含字母、数字、下划线、点和连字符，且以字母或数字开头（最长 128 位）")
	}
	if !inStrings(validKinds, m.Kind) {
		add("kind", oneOfMsg(validKinds))
	}
	if strings.TrimSpace(m.Provider) == "" {
		add("provider", "不能为空，填写所属平台的 key")
	}
	if strings.TrimSpace(m.Label) == "" {
		add("label", "不能为空")
	}
	if m.Credits < 0 {
		add("credits", "不能为负数")
	}
	if m.Deadline < 0 || m.Deadline.D() > maxModelDeadline {
		add("deadline", "必须在 0 到 24h 之间（不填默认 30m）")
	}
	if !inStrings(validKinds, m.Output.Media) {
		add("output.media", oneOfMsg(validKinds))
	}

	validateInputSchema(m.InputSchema, issues)

	// 表达式：mapping 与 output.select
	var refs templateRefs
	if m.Mapping != nil {
		checkTemplate("mapping", m.Mapping, stageMapping, issues, &refs)
	}
	checkExprSource("output.select", m.Output.Select, stageOutput, issues, &refs)

	checkRefs(m.InputSchema, refs, true, issues)

	if provider != nil {
		validateModelAgainstProvider(m, provider, issues)
	}
}

// checkRefs 校验模板里的 input.xxx / files.xxx 引用：files.xxx 必须是媒体字段；
// checkInput 为 true 时 input.xxx 还必须存在于 input_schema。
func checkRefs(schema InputSchema, refs templateRefs, checkInput bool, issues *[]Issue) {
	if checkInput {
		for _, r := range refs.input {
			if _, ok := schema.Get(r.Name); !ok {
				*issues = append(*issues, Issue{Path: r.Path, Message: fmt.Sprintf("引用了 input.%s，但 input_schema 里没有这个字段", r.Name)})
			}
		}
	}
	for _, r := range refs.files {
		f, ok := schema.Get(r.Name)
		switch {
		case !ok:
			*issues = append(*issues, Issue{Path: r.Path, Message: fmt.Sprintf("引用了 files.%s，但 input_schema 里没有这个字段", r.Name)})
		case !isMediaType(f.Type):
			*issues = append(*issues, Issue{Path: r.Path, Message: fmt.Sprintf("files.%s 只能引用媒体字段（image / video / audio），而 %s 的类型是 %s", r.Name, r.Name, f.Type)})
		}
	}
}

// validateModelAgainstProvider 跨对象校验：平台 key 一致、平台具备 submit / query、平台模板里的 files 引用合法。
func validateModelAgainstProvider(m *ModelConfig, p *ProviderConfig, issues *[]Issue) {
	add := func(path, msg string) { *issues = append(*issues, Issue{Path: path, Message: msg}) }
	if m.Provider != "" && m.Provider != p.Key {
		add("provider", fmt.Sprintf("是 %q，但传入的平台 key 是 %q", m.Provider, p.Key))
	}
	if p.Operations.Submit == nil {
		add("provider", fmt.Sprintf("平台 %q 没有配置 submit 操作", p.Key))
	}
	if p.Operations.Query == nil {
		add("provider", fmt.Sprintf("平台 %q 没有配置 query 操作", p.Key))
	}

	// 平台模板里引用的 files.xxx 也必须是本模型的媒体字段；input.xxx 不强制（平台模板常用 ?? 提供默认值）
	var refs templateRefs
	var scratch []Issue // 平台自身的表达式问题由 ParseProvider 负责，这里只取引用
	for _, e := range []struct {
		name string
		op   *Operation
	}{{"upload", p.Operations.Upload}, {"submit", p.Operations.Submit}, {"query", p.Operations.Query}, {"cancel", p.Operations.Cancel}} {
		if e.op == nil {
			continue
		}
		base := "provider.operations." + e.name
		checkTemplate(base+".path", e.op.Path, stageAny, &scratch, &refs)
		for _, k := range sortedKeys(e.op.Headers) {
			checkTemplate(joinPath(base+".headers", k), e.op.Headers[k], stageAny, &scratch, &refs)
		}
		for _, k := range sortedKeys(e.op.Query) {
			checkTemplate(joinPath(base+".query", k), e.op.Query[k], stageAny, &scratch, &refs)
		}
		checkTemplate(base+".body", e.op.Body, stageAny, &scratch, &refs)
	}
	checkRefs(m.InputSchema, refs, false, issues)
}

func isMediaType(t string) bool { return t == FieldImage || t == FieldVideo || t == FieldAudio }

func validateInputSchema(schema InputSchema, issues *[]Issue) {
	seen := map[string]bool{}
	for _, e := range schema {
		base := joinPath("input_schema", e.Name)
		add := func(sub, msg string) { *issues = append(*issues, Issue{Path: joinPath(base, sub), Message: msg}) }
		addSelf := func(msg string) { *issues = append(*issues, Issue{Path: base, Message: msg}) }

		if !fieldNameRe.MatchString(e.Name) {
			addSelf("字段名只能包含字母、数字和下划线，且不能以数字开头（它会作为 input.<字段名> 出现在表达式里）")
		}
		if seen[e.Name] {
			addSelf("字段名重复")
		}
		seen[e.Name] = true

		f := e.InputField
		if !inStrings(validFieldTypes, f.Type) {
			add("type", oneOfMsg(validFieldTypes))
			continue
		}
		if strings.TrimSpace(f.Label) == "" {
			add("label", "不能为空（前端用它作为控件标题和错误提示）")
		}

		// 各类型专属属性
		if f.Type != FieldNumber {
			if f.Min != nil {
				add("min", "只有 number 类型可以设置")
			}
			if f.Max != nil {
				add("max", "只有 number 类型可以设置")
			}
		}
		if f.Type != FieldText && f.MaxLength != 0 {
			add("max_length", "只有 text 类型可以设置")
		}
		if f.MaxLength < 0 {
			add("max_length", "不能为负数")
		}
		if f.Type != FieldEnum && len(f.Options) > 0 {
			add("options", "只有 enum 类型可以设置")
		}
		if f.Min != nil && f.Max != nil && *f.Min > *f.Max {
			add("min", "不能大于 max")
		}

		if f.Type == FieldEnum {
			validateEnumOptions(f, add)
		}

		// port
		if f.Port != "" {
			switch {
			case !inStrings(validPorts, f.Port):
				add("port", oneOfMsg(validPorts))
			case f.Type == FieldText && f.Port != "text":
				add("port", "text 字段的 port 只能是 text")
			case isMediaType(f.Type) && f.Port != f.Type:
				add("port", fmt.Sprintf("%s 字段的 port 只能是 %s", f.Type, f.Type))
			case f.Type == FieldNumber || f.Type == FieldEnum || f.Type == FieldBoolean:
				add("port", "只有 text 和媒体字段可以由上游连线提供")
			}
		}

		validateDefault(f, add)
	}
}

func validateEnumOptions(f InputField, add func(sub, msg string)) {
	if len(f.Options) == 0 {
		add("options", "enum 类型至少需要一个选项")
		return
	}
	seen := map[string]bool{}
	for i, o := range f.Options {
		op := fmt.Sprintf("options[%d]", i)
		switch o.Value.(type) {
		case string, float64, int:
		default:
			add(op+".value", "必须是字符串或数字")
			continue
		}
		key := optionKey(o.Value)
		if seen[key] {
			add(op+".value", "选项值重复")
		}
		seen[key] = true
		if strings.TrimSpace(o.Label) == "" {
			add(op+".label", "不能为空")
		}
	}
}

// optionKey 把选项值规范成可比较的键，5 与 5.0 与 "5" 视为同一个值。
func optionKey(v any) string {
	if f, ok := toFloat(v); ok {
		return "n:" + stringify(f)
	}
	s := stringify(v)
	if f, ok := parseNumberString(s); ok {
		return "n:" + stringify(f)
	}
	return "s:" + s
}

func validateDefault(f InputField, add func(sub, msg string)) {
	if f.Default == nil {
		return
	}
	d := f.Default
	switch f.Type {
	case FieldText:
		s, ok := d.(string)
		if !ok {
			add("default", "text 字段的默认值必须是字符串")
		} else if f.MaxLength > 0 && len([]rune(s)) > f.MaxLength {
			add("default", "默认值超过 max_length")
		}
	case FieldNumber:
		n, ok := toFloat(d)
		switch {
		case !ok:
			add("default", "number 字段的默认值必须是数字")
		case f.Min != nil && n < *f.Min:
			add("default", "默认值小于 min")
		case f.Max != nil && n > *f.Max:
			add("default", "默认值大于 max")
		}
	case FieldBoolean:
		if _, ok := d.(bool); !ok {
			add("default", "boolean 字段的默认值必须是 true / false")
		}
	case FieldEnum:
		if _, ok := matchOption(f.Options, d); !ok && len(f.Options) > 0 {
			add("default", "默认值必须是 options 里的某个值")
		}
	default:
		add("default", "媒体字段不能设置默认值")
	}
}
