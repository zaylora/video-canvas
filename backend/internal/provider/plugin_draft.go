package provider

import (
	"context"
	"encoding/json"

	"video-canvas/internal/provider/pluginmeta"
)

// PluginMeta 是插件纯数据契约，服务层无需依赖 runner 或宿主实现。
type PluginMeta = pluginmeta.Meta

// MaxPluginDraftBytes 与上传插件的源码大小上限一致。
const MaxPluginDraftBytes = pluginmeta.MaxPluginBytes

// PluginDraftInput 是尚未上传的插件与可选文档样例，执行器不得发起上游请求。
type PluginDraftInput struct {
	Code      string                     `json:"code"`
	CtxSample map[string]any             `json:"ctx_sample,omitempty"`
	Responses map[string]json.RawMessage `json:"responses,omitempty"`
}

// PluginDraftResult 是预检与样例钩子结果；凭证必须在离开 Go 前脱敏。
type PluginDraftResult struct {
	OK           bool                       `json:"ok"`
	Meta         json.RawMessage            `json:"meta,omitempty"`
	Issues       []string                   `json:"issues"`
	Request      map[string]any             `json:"request,omitempty"`
	Parsed       map[string]json.RawMessage `json:"parsed,omitempty"`
	Logs         []string                   `json:"logs"`
	Attempt      int                        `json:"attempt"`
	AttemptsLeft int                        `json:"attempts_left"`
	Coverage     *PluginDraftCoverage       `json:"coverage,omitempty"`
}

// PluginDraftCoverage 明确本次样例含哪些媒体、执行了哪些钩子，不代表已通过真实生成。
type PluginDraftCoverage struct {
	InputMedia   map[string]int `json:"input_media"`
	CheckedHooks []string       `json:"checked_hooks"`
}

// PluginDraftChecker 使用同一 runner 预检和沙箱执行钩子，不访问数据库或上游网络。
type PluginDraftChecker interface {
	Prechecker
	// CheckDraft 在预检通过的代码上执行提交请求和文档响应样例。
	CheckDraft(ctx context.Context, in PluginDraftInput, pre *PrecheckResult) (*PluginDraftResult, error)
}
