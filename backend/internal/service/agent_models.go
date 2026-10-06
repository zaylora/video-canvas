package service

import (
	"context"

	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
)

// RegistryAgentModels 把后台已发布、已上架的 agent 类型模型，作为画布 Agent 可选的大语言模型。
type RegistryAgentModels struct {
	reg provider.Registry
}

// NewRegistryAgentModels 创建基于模型注册表的 Agent 模型清单。
func NewRegistryAgentModels(reg provider.Registry) *RegistryAgentModels {
	return &RegistryAgentModels{reg: reg}
}

// List 返回当前可用的 Agent 模型。注册表只给已发布、已上架且渠道与插件可用的模型，
// 所以停用的渠道、被下架的模型不会出现在这里，也就选不到。
func (m *RegistryAgentModels) List(ctx context.Context) ([]model.AgentModelView, error) {
	infos, err := m.reg.ListModels(ctx, modelcfg.KindAgent)
	if err != nil {
		return nil, err
	}
	out := make([]model.AgentModelView, 0, len(infos))
	for _, in := range infos {
		out = append(out, model.AgentModelView{Key: in.Key, Name: in.Label, Vision: in.Capabilities.Vision})
	}
	return out, nil
}
