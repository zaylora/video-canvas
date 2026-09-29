package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"video-canvas/internal/provider/dsl"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/repository"

	"go.uber.org/zap"
)

// DryRun 用草稿模型（没有草稿则用已发布版本）组装快照，按 input_schema 校验示例输入，
// 交给引擎渲染上传和提交请求但不发送，返回脱敏后的渲染结果。useProviderDraft=true 时优先用平台草稿，否则优先用已发布的平台。
func (s *AIConfigService) DryRun(ctx context.Context, modelKey string, input map[string]any, useProviderDraft bool) (any, error) {
	// 1. 依赖检查：引擎适配器在接线时注入
	if s.dryRunner == nil {
		return nil, errcode.ErrInternal.WithMsg("dry-run 功能未启用")
	}
	// 2. 用草稿组装快照
	snap, err := s.buildTrialSnapshot(ctx, modelKey, useProviderDraft)
	if err != nil {
		return nil, err
	}
	// 3. 校验并规范化示例输入：与真实提交走同一套规则，dry-run 才能反映线上行为
	normalized, err := s.normalizeTrialInput(snap, input)
	if err != nil {
		return nil, err
	}
	// 4. 渲染；渲染失败几乎都是配置问题（表达式、字段引用），把原因带给运营
	secrets := s.trialSecrets(ctx, snap)
	res, err := s.dryRunner.DryRun(ctx, snap, normalized)
	if err != nil {
		return nil, errcode.ErrConfigInvalid.WithMsg("dry-run 失败：" + aiRedactString(err.Error(), secrets))
	}
	// 5. 引擎已经脱敏，这里再做一次防御性脱敏（凭证明文绝不能出现在任何管理接口响应里）
	return aiRedactResult(res, secrets), nil
}

// TestRun 用草稿真实跑一次：组装草稿快照，创建 is_test 任务（不扣用户积分），返回任务视图。
// 前端随后轮询 GetTestRun 查看进度和结果。
func (s *AIConfigService) TestRun(ctx context.Context, adminID uint64, modelKey string, input map[string]any, useProviderDraft bool) (*model.GenerationTaskView, error) {
	// 1. 依赖检查
	if s.testTasks == nil {
		return nil, errcode.ErrInternal.WithMsg("试跑功能未启用")
	}
	// 2. 组装草稿快照
	snap, err := s.buildTrialSnapshot(ctx, modelKey, useProviderDraft)
	if err != nil {
		return nil, err
	}
	// 3. 凭证必须已设置：试跑会真实调用平台，提前拦下比创建一个必然失败的任务更清楚
	if name := snap.Provider.Auth.Secret; snap.Provider.Auth.Type != dsl.AuthNone && name != "" {
		if _, err := s.repo.GetSecret(ctx, name); errors.Is(err, repository.ErrNotFound) {
			return nil, errcode.ErrSecretNotSet.WithMsg(fmt.Sprintf("凭证 %q 尚未设置", name))
		} else if err != nil {
			return nil, err
		}
	}
	// 4. 校验并规范化输入
	normalized, err := s.normalizeTrialInput(snap, input)
	if err != nil {
		return nil, err
	}
	// 5. 创建试跑任务；任务服务负责 is_test 标记、不扣积分、不推送
	view, err := s.testTasks.SubmitTest(ctx, adminID, snap, normalized)
	if err != nil {
		return nil, err
	}
	logger.Info("创建试跑任务", zap.String("model", modelKey), zap.Uint64("admin_id", adminID), zap.Uint64("task_id", view.ID))
	return view, nil
}

// GetTestRun 查询试跑任务的当前视图。只能查自己创建的试跑任务，查不到统一返回“任务不存在”。
func (s *AIConfigService) GetTestRun(ctx context.Context, adminID, taskID uint64) (*model.GenerationTaskView, error) {
	if s.testTasks == nil {
		return nil, errcode.ErrInternal.WithMsg("试跑功能未启用")
	}
	view, err := s.testTasks.GetTestTask(ctx, adminID, taskID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrTaskNotFound
	}
	if err != nil {
		return nil, err
	}
	return view, nil
}

// buildTrialSnapshot 用草稿模型 + 平台配置组装试跑快照。
// 模型：优先最新草稿，没有草稿用已发布版本；平台：useProviderDraft 决定草稿优先还是已发布优先。
// 配置本身校验不通过时直接报错，不拿有问题的配置去渲染。
func (s *AIConfigService) buildTrialSnapshot(ctx context.Context, modelKey string, useProviderDraft bool) (*dsl.Snapshot, error) {
	// 1. 模型 revision
	rev, err := s.repo.GetDraft(ctx, model.ConfigTargetModel, modelKey)
	if errors.Is(err, repository.ErrNotFound) {
		rev, err = s.repo.GetPublishedRevision(ctx, model.ConfigTargetModel, modelKey)
	}
	if err != nil {
		return nil, aiNotFound(err)
	}
	meta, err := aiParseMeta(rev.BodyJSON)
	if err != nil {
		return nil, errcode.ErrConfigInvalid.WithMsg("模型配置正文不是合法的 JSON 对象")
	}
	// 2. 平台配置
	order := aiProviderPublishedThenDraft
	if useProviderDraft {
		order = aiProviderDraftThenPublished
	}
	pcfg, prev, err := s.loadProvider(ctx, meta.Provider, order)
	if err != nil {
		return nil, err
	}
	// 3. 模型配置校验（含跨对象引用检查）
	mcfg, issues := s.validator.ParseModel(rev.BodyJSON, pcfg)
	if len(issues) > 0 || mcfg == nil {
		return nil, errcode.ErrConfigInvalid.WithMsg("模型配置校验未通过：" + aiFormatIssues(issues))
	}
	return &dsl.Snapshot{Provider: *pcfg, Model: *mcfg, ModelRevisionID: rev.ID, ProviderRevisionID: prev.ID}, nil
}

// normalizeTrialInput 按 input_schema 校验示例输入，字段级错误合并成一条提示。
func (s *AIConfigService) normalizeTrialInput(snap *dsl.Snapshot, input map[string]any) (map[string]any, error) {
	if input == nil {
		input = map[string]any{}
	}
	normalized, ferrs := s.validator.ValidateInput(snap.Model.InputSchema, input)
	if len(ferrs) > 0 {
		parts := make([]string, 0, len(ferrs))
		for _, fe := range ferrs {
			parts = append(parts, fe.Field+"："+fe.Message)
		}
		return nil, errcode.ErrTaskInput.WithMsg("示例输入不合法：" + strings.Join(parts, "；"))
	}
	return normalized, nil
}

// trialSecrets 取出快照里平台鉴权凭证的明文，仅用于对输出做脱敏；取不到（未设置、没有主密钥）就返回空。
func (s *AIConfigService) trialSecrets(ctx context.Context, snap *dsl.Snapshot) []string {
	name := snap.Provider.Auth.Secret
	if name == "" {
		return nil
	}
	v, err := s.Get(ctx, name)
	if err != nil || v == "" {
		return nil
	}
	return []string{v}
}

// aiRedactResult 对引擎结果做防御性脱敏：先转成通用 JSON 结构，再做一遍文本级替换（不依赖 dsl.Redact 的实现细节，
// 转义后的明文也能替换掉），最后交给 dsl.Redact 兜底。任何一步失败都不返回原结果，宁可丢结果也不泄露凭证。
func aiRedactResult(res any, secrets []string) any {
	b, err := json.Marshal(res)
	if err != nil {
		return nil
	}
	for _, sec := range secrets {
		if sec == "" {
			continue
		}
		esc, _ := json.Marshal(sec)
		b = bytes.ReplaceAll(b, esc[1:len(esc)-1], []byte("***"))
	}
	var generic any
	if err := json.Unmarshal(b, &generic); err != nil {
		return nil
	}
	return dsl.Redact(generic, secrets...)
}

// aiRedactString 把文本里出现的凭证明文替换成 ***。
func aiRedactString(text string, secrets []string) string {
	for _, sec := range secrets {
		if sec != "" {
			text = strings.ReplaceAll(text, sec, "***")
		}
	}
	return text
}
