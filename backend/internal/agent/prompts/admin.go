package prompts

import _ "embed"

//go:embed admin.md
var admin string

// Admin 返回独立后台提示词并附加当前模型 JSON Schema，避免字段说明漂移。
func Admin(schema []byte) string {
	return admin + "\n<模型字段 Schema>\n" + string(schema) + "\n</模型字段 Schema>\n"
}
