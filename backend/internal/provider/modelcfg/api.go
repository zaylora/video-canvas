// 本文件：modelcfg 包对外暴露的函数签名（契约）。

package modelcfg

// ValidateInput 按模型能力校验并规范化任务输入：提示词、生成方式、生成参数（补默认值、类型转换、枚举与范围）、
// 参考素材（按生成方式与 refs 过滤，检查数量）；未知键忽略。素材值规范成 uint64 数组，
// 素材归属、种类与大小由调用方（task service）负责。
func ValidateInput(kind string, caps Capabilities, input map[string]any) (map[string]any, []FieldError) {
	return validateInput(kind, caps, input)
}

// MediaRefs 按 images / videos / audios 的顺序提取任务输入里的参考素材（兼容落库后 JSON 解码出的数组形态）。
func MediaRefs(input map[string]any) []MediaRef { return mediaRefs(input) }

// AsAssetID 把 JSON 数字、float64、json.Number、数字字符串规范成 uint64 素材 id；0、负数与非整数无效。
// 落库的任务输入经 JSON 解码后素材 id 是 float64，使用方要先规范化再当 asset id 用。
func AsAssetID(raw any) (uint64, bool) { return asAssetID(raw) }

// ParseModel 解析并校验模型配置正文：JSON 结构（未知字段、类型不符）、key / kind / label / credits / deadline、
// channels（首期恰好一个，渠道 key 与上游模型名合法）、params、capabilities。所有问题一次报出，Issue 精确到 JSON 路径。
// 成功时 deadline 缺省补成 30m，params 与 input_schema 里的数字已规范化。
// 不检查渠道是否存在、插件是否支持该 kind：那是 service 层的跨对象检查。没有 Issue 才算通过。
func ParseModel(body []byte) (*ModelConfig, []Issue) { return parseModel(body) }

// JSONSchema 返回模型配置正文的 JSON Schema（供前端编辑器补全）。
// 每次返回缓存的副本，调用方可以随意修改。
func JSONSchema() []byte {
	schemaOnce.Do(buildSchema)
	return append([]byte(nil), schemaCache...)
}
