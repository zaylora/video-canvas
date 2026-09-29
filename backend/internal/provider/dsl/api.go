// 本文件：dsl 包对外暴露的函数签名（契约）：ParseProvider / ParseModel / ValidateInput / RenderValue 等入口，
// 以及 Issue、FieldError、RenderContext 这些跨包共用的类型。

package dsl

import "errors"

// 本文件是 dsl 包对外暴露的函数签名（契约）。dsl 实现方替换函数体，不要改签名；
// 其他包（config 服务、engine、task service）直接按这些签名调用。

// Issue 是一条校验问题，Path 是精确到字段的 JSON 路径，例如 "operations.submit.body.nodeInfoList"。
type Issue struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// FieldError 是用户输入的字段级错误，Field 是 input_schema 里的字段名。
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// RenderContext 是表达式可用的上下文变量（见设计 5.2）。上下文里没有任何 secret。
type RenderContext struct {
	Input  map[string]any // 已按 input_schema 校验过的用户输入
	Files  map[string]any // 上传步骤的结果，按输入名索引
	Model  map[string]any // 至少含 params、mapping
	Task   map[string]any // id、provider_task_id
	Ctx    map[string]any // webhook_url、now
	Resp   any            // 响应 JSON（响应处理阶段）
	Status int            // HTTP 状态码（响应处理阶段）
	Req    any            // 回调请求体（webhook 阶段）

	// 以下是对契约的增量扩展。
	Outputs any            // 平台产物列表（output.select 阶段的 outputs 变量）
	Upload  map[string]any // 上传请求的上下文：field / kind / file_name / mime_type（upload 阶段的 upload 变量）
}

// ErrNotImplemented 占位错误。
var ErrNotImplemented = errors.New("dsl: not implemented")

// ParseProvider 解析并校验 Provider 配置正文：JSON 结构、枚举取值、所有表达式编译、host 白名单格式等。
// 返回的 Issue 精确到 JSON 路径；没有 Issue 才算通过。
func ParseProvider(body []byte) (*ProviderConfig, []Issue) { return parseProvider(body) }

// ParseModel 解析并校验 Model 配置正文。provider 用来核对引用（如 mapping 里用到的 files.xxx 必须是
// input_schema 里的媒体字段、provider 必须存在 submit 操作）。provider 为 nil 时跳过跨对象检查。
func ParseModel(body []byte, provider *ProviderConfig) (*ModelConfig, []Issue) {
	return parseModel(body, provider)
}

// ValidateInput 按 input_schema 校验并规范化用户输入：补默认值、类型转换（如 JSON 数字）、
// 必填、枚举、长度、范围；未知字段忽略。媒体字段（image/video/audio）的值是 asset id（数字或数字字符串），
// 规范化成 uint64；asset 归属校验由调用方（task service）负责。
func ValidateInput(schema InputSchema, input map[string]any) (map[string]any, []FieldError) {
	return validateInput(schema, input)
}

// MediaFieldNames 返回 schema 中所有媒体字段的名字（image/video/audio）。
func MediaFieldNames(schema InputSchema) []string { return mediaFieldNames(schema) }

// RenderValue 渲染模板值：字符串 "${ expr }" 整体求值并保留类型；"前缀-${ expr }" 做字符串插值；
// map / slice 递归渲染；其他类型原样返回。
func RenderValue(tpl any, rc *RenderContext) (any, error) { return renderValue(tpl, rc) }

// EvalExpr 对纯表达式源码求值（success / extract.* / when / output.select 等）。
func EvalExpr(src string, rc *RenderContext) (any, error) { return evalExpr(src, rc) }

// EvalBool 对纯表达式求值并要求结果为 bool。
func EvalBool(src string, rc *RenderContext) (bool, error) { return evalBool(src, rc) }

// MapStatus 用 provider.status_map 把平台状态映射成统一状态（queued/running/succeeded/failed）。
// 找不到时用 "_default"，并返回 usedDefault=true 供调用方记录告警日志。
func MapStatus(p *ProviderConfig, providerStatus string) (status string, usedDefault bool) {
	return mapStatus(p, providerStatus)
}

// Redact 把 v 里出现的 secrets 明文替换成 "***"，用于 dry-run / 试跑 / 日志输出脱敏。
func Redact(v any, secrets ...string) any { return redact(v, secrets) }
