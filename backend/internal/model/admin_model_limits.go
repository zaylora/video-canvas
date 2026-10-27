package model

// ModelLimitsReq 是 GET /admin/ai/models/limits 的查询参数。
type ModelLimitsReq struct {
	Name    string `form:"name" binding:"required,max=200" label:"模型名"` // 上游模型名，如 gpt-4o、anthropic/claude-sonnet-4-5
	Channel string `form:"channel" binding:"max=64" label:"渠道"`         // 可选：模型所在渠道的 key；带上就先问该渠道的上游
}

// ModelLimitsSourceBuiltin 是规格来自内置快照（models.dev）。
const ModelLimitsSourceBuiltin = "builtin" // 内置快照（来自 models.dev）

// ModelLimitsSourceUpstream 是规格来自渠道上游的 /v1/models。
const ModelLimitsSourceUpstream = "upstream"

// ModelLimitsView 是按模型名识别出的上下文规格；只是建议，运营可以改。查不到不算错误，Found 为 false。
type ModelLimitsView struct {
	Found         bool   `json:"found"`            // 是否认识这个模型
	ContextWindow int    `json:"context_window"`   // 上下文窗口（Token），没认出来是 0
	MaxOutput     int    `json:"max_output"`       // 最大输出（Token），没认出来是 0
	Source        string `json:"source,omitempty"` // 来源，没认出来为空
	Vision        *bool  `json:"vision,omitempty"` // 看图建议；未识别输入模态时省略，不覆盖运营配置
	Video         *bool  `json:"video,omitempty"`  // 看视频建议；只有 Gemini 格式可应用
}
