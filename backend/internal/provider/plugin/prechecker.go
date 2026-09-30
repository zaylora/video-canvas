package plugin

import (
	"context"
	"errors"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
)

// prechecker 把 runner 的预检接口包装成 provider.Prechecker，service 不必认识线协议。
type prechecker struct {
	c RunnerClient
}

// NewPrechecker 创建基于 runner 客户端的预检器。
func NewPrechecker(c RunnerClient) provider.Prechecker {
	return &prechecker{c: c}
}

// Precheck 在 runner 里编译并执行插件，读出 meta 与导出的钩子并按契约检查。
// runner 连不上返回 *provider.Error（retryable + CodeRunnerUnavailable）；预检过程中 runner 崩溃，
// 说明是上传的代码把 runner 拖垮了（如死循环分配内存），按“预检不通过”返回，而不是让上传方重试。
func (p *prechecker) Precheck(ctx context.Context, code string) (*provider.PrecheckResult, error) {
	resp, err := p.c.Precheck(ctx, code)
	switch {
	case errors.Is(err, ErrRunnerCrashed):
		return &provider.PrecheckResult{Issues: []modelcfg.Issue{{Message: "预检时插件导致 plugin-runner 崩溃（可能是死循环或内存占用过大）"}}}, nil
	case err != nil:
		return nil, &provider.Error{Class: provider.ClassRetryable, Code: provider.CodeRunnerUnavailable, Message: "plugin-runner 不可用，无法预检", Cause: err}
	}
	out := &provider.PrecheckResult{OK: resp.OK, Meta: resp.Meta, Hooks: resp.Hooks, SHA256: resp.SHA256}
	for _, is := range resp.Issues {
		out.Issues = append(out.Issues, modelcfg.Issue{Path: is.Path, Message: is.Message})
	}
	// 没有任何问题却报不通过时补一条说明，避免管理端只看到“失败”而不知道原因
	if !out.OK && len(out.Issues) == 0 {
		out.Issues = []modelcfg.Issue{{Message: "预检未通过"}}
	}
	return out, nil
}
