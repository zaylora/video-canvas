// 本文件：modelcfg 包对外暴露的函数签名（契约）。

package modelcfg

// ValidateInput 按 input_schema 校验并规范化用户输入：补默认值、类型转换（如 JSON 数字）、
// 必填、枚举、长度、范围；未知字段忽略。媒体字段（image/video/audio）的值是 asset id（数字或数字字符串），
// 规范化成 uint64；asset 归属校验由调用方（task service）负责。
func ValidateInput(schema InputSchema, input map[string]any) (map[string]any, []FieldError) {
	return validateInput(schema, input)
}

// MediaFieldNames 返回 schema 中所有媒体字段的名字（image/video/audio）。
func MediaFieldNames(schema InputSchema) []string { return mediaFieldNames(schema) }

// AsAssetID 把 JSON 数字、float64、json.Number、数字字符串规范成 uint64 素材 id；0、负数与非整数无效。
// 落库的任务输入经 JSON 解码后素材 id 是 float64，使用方要先规范化再当 asset id 用。
func AsAssetID(raw any) (uint64, bool) { return asAssetID(raw) }

// ParseModel 解析并校验模型配置正文：JSON 结构（未知字段、类型不符）、key / kind / label / credits / deadline、
// channels（首期恰好一个，渠道 key 与上游模型名合法）、params、input_schema。所有问题一次报出，Issue 精确到 JSON 路径。
// 成功时 deadline 缺省补成 30m，params 与 input_schema 里的数字已规范化。
// 不检查渠道是否存在、插件是否支持该 kind：那是 service 层的跨对象检查。没有 Issue 才算通过。
func ParseModel(body []byte) (*ModelConfig, []Issue) { return parseModel(body) }

// JSONSchema 返回模型配置正文的 JSON Schema（供前端编辑器补全）。
// 每次返回缓存的副本，调用方可以随意修改。
func JSONSchema() []byte {
	schemaOnce.Do(buildSchema)
	return append([]byte(nil), schemaCache...)
}
