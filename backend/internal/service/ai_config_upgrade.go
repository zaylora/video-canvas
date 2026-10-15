package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
)

// UpgradeFailure 是批量升级里失败的一个模型。
type UpgradeFailure struct {
	Key    string `json:"key"`
	Reason string `json:"reason"`
}

// UpgradeModelsResult 是批量升级的结果：升级成功的模型，失败的模型与原因（失败的保持原样）。
type UpgradeModelsResult struct {
	ToVersion string           `json:"to_version"` // 实际升到的版本
	Upgraded  []string         `json:"upgraded"`
	Failed    []UpgradeFailure `json:"failed"`
}

// UpgradePluginModels 实现 AIPluginModels：把协议固定在插件 pluginKey 旧版本上的模型批量升到 toVersion（空串表示该插件的最新版本）。
// 逐个处理，失败不中断：协议设置项按新版本的声明处理——新版本不再声明的设置项丢弃，必填项缺失、模型已启用却过不了启用检查等
// 都记为该模型的失败，保持原样。升级后新任务用新版本，进行中的任务按快照里的旧版本跑完。
// 插件不存在返回 ErrPluginNotFound，目标版本不存在返回 ErrPluginNotFound（带版本号），插件已停用返回 ErrPluginDisabled。
func (s *AIConfigService) UpgradePluginModels(ctx context.Context, actorID uint64, pluginKey, toVersion string) (*UpgradeModelsResult, error) {
	// 1. 插件与目标版本
	target, metaJSON, err := s.upgradeTarget(ctx, pluginKey, toVersion)
	if err != nil {
		return nil, err
	}
	// 2. 逐个模型处理
	cfgs, err := s.repo.ListModelConfigs(ctx)
	if err != nil {
		return nil, err
	}
	res := &UpgradeModelsResult{ToVersion: target, Upgraded: []string{}, Failed: []UpgradeFailure{}}
	for _, c := range cfgs {
		d := aiDisplayOf(c.BodyJSON)
		if d.plugin != pluginKey || d.pluginVer == target {
			continue
		}
		if reason := s.upgradeOne(ctx, actorID, c.TargetKey, c.BodyJSON, pluginKey, target, metaJSON); reason != "" {
			res.Failed = append(res.Failed, UpgradeFailure{Key: c.TargetKey, Reason: reason})
			continue
		}
		res.Upgraded = append(res.Upgraded, c.TargetKey)
	}
	return res, nil
}

// upgradeTarget 确定批量升级的目标版本：插件要存在且启用；toVersion 为空取 semver 最高的版本，否则版本必须存在。
// 返回目标版本号与它的 meta_json。
func (s *AIConfigService) upgradeTarget(ctx context.Context, pluginKey, toVersion string) (string, []byte, error) {
	p, err := s.plugins.GetPlugin(ctx, pluginKey)
	if errors.Is(err, repository.ErrNotFound) {
		return "", nil, errcode.ErrPluginNotFound
	}
	if err != nil {
		return "", nil, err
	}
	if !p.Enabled {
		return "", nil, errcode.ErrPluginDisabled
	}
	vers, err := s.plugins.ListVersions(ctx, pluginKey)
	if err != nil {
		return "", nil, err
	}
	best := -1
	for i, v := range vers {
		switch {
		case toVersion != "" && v.Version == toVersion:
			best = i
		case toVersion == "" && (best < 0 || compareSemver(v.Version, vers[best].Version) > 0):
			best = i
		}
	}
	if best < 0 {
		return "", nil, errcode.ErrPluginNotFound.WithMsg(fmt.Sprintf("插件 %s 没有版本 %s", pluginKey, toVersion))
	}
	return vers[best].Version, []byte(vers[best].MetaJSON), nil
}

// upgradeOne 升级一个模型：改写 protocol 后走与手动保存同一条路径（已启用的模型会先过启用检查，过不了就拒绝，保持原样）。
// 成功返回空串，失败返回给运营看的原因。
func (s *AIConfigService) upgradeOne(ctx context.Context, actorID uint64, key string, body []byte, pluginKey, target string, metaJSON []byte) string {
	next, err := upgradeProtocolBody(body, target, metaJSON)
	if err != nil {
		return err.Error()
	}
	if _, err := s.SaveModel(ctx, ModelSaveInput{Key: key, Body: next,
		Note: fmt.Sprintf("协议插件 %s 升级到 %s", pluginKey, target), AdminID: actorID}); err != nil {
		return upgradeReason(err)
	}
	return ""
}

// upgradeReason 取错误里对运营有用的原因文案：业务错误取 Msg，其余统一说“保存失败”（内部错误不透传）。
func upgradeReason(err error) string {
	var e *errcode.Error
	if errors.As(err, &e) {
		return e.Msg
	}
	return "保存失败"
}

// upgradeProtocolBody 把模型正文里 channels[0].protocol 的版本改成 version，协议设置项按新版本的声明处理（metaJSON 是新版本的 meta_json）：
// 新版本不再声明的丢弃，其余按声明校验并补默认值（必填项缺失返回错误）。
// 只改 channels 这一项，其余字段按原样字节搬运：capabilities 里 input_schema 的书写顺序就是前端的渲染顺序，不能被重排。
func upgradeProtocolBody(body []byte, version string, metaJSON []byte) ([]byte, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		return nil, errors.New("配置正文不是合法的 JSON 对象")
	}
	var channels []map[string]json.RawMessage
	if err := json.Unmarshal(top["channels"], &channels); err != nil || len(channels) == 0 {
		return nil, errors.New("配置正文的 channels 不合法")
	}
	var proto struct {
		Plugin   string         `json:"plugin"`
		Version  string         `json:"version"`
		Settings map[string]any `json:"settings"`
	}
	if err := json.Unmarshal(channels[0]["protocol"], &proto); err != nil {
		return nil, errors.New("配置正文的 protocol 不合法")
	}
	settings, issues, err := provider.CarrySettings(metaJSON, proto.Settings)
	if err != nil {
		return nil, err
	}
	if len(issues) > 0 {
		return nil, fmt.Errorf("新版本的协议设置不合法：%s", aiFormatIssues(issues))
	}
	proto.Version, proto.Settings = version, settings
	rawProto, err := json.Marshal(proto)
	if err != nil {
		return nil, err
	}
	channels[0]["protocol"] = rawProto
	if top["channels"], err = json.Marshal(channels); err != nil {
		return nil, err
	}
	return json.Marshal(top)
}
