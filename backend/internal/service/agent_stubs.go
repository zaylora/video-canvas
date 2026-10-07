package service

import (
	"context"
	"errors"

	"video-canvas/internal/model"
)

// NoAgentModels 是还没有接入 Agent 模型配置时的占位：清单永远为空，
// 所以发起运行一律返回「Agent 模型不可用」（60002），功能对用户是关着的。
type NoAgentModels struct{}

// List 返回空清单。
func (NoAgentModels) List(context.Context) ([]model.AgentModelView, error) { return nil, nil }

// ErrAgentRuntimeMissing 表示还没有接入 Agent 运行时。
var ErrAgentRuntimeMissing = errors.New("运行时尚未接入")

// NoAgentRuntime 是还没有接入运行时的占位：启动和继续都返回 ErrAgentRuntimeMissing。
type NoAgentRuntime struct{}

// Start 总是失败。
func (NoAgentRuntime) Start(context.Context, *model.AgentRun, model.AgentRunInput) error {
	return ErrAgentRuntimeMissing
}

// Interject 总是失败。
func (NoAgentRuntime) Interject(context.Context, uint64, string) error { return ErrAgentRuntimeMissing }

// Cancel 什么都不做：没有运行时就没有要中止的东西。
func (NoAgentRuntime) Cancel(context.Context, uint64) error { return nil }

// Resume 总是失败。
func (NoAgentRuntime) Resume(context.Context, *model.AgentRun, ResumeInfo) error {
	return ErrAgentRuntimeMissing
}
