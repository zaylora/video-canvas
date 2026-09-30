package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/repository"
)

// aiRedactMask 是凭证明文在输出里的替换文本。
const aiRedactMask = "***"

// DryRun 用模型草稿（没有草稿则用已发布版本）+ 渠道当前配置 + 渠道固定的插件版本组装快照，按 input_schema 校验示例输入，
// 交给宿主渲染请求但不发送，返回脱敏后的渲染结果（插件返回的请求描述与宿主注入鉴权后的最终请求）。
func (s *AIConfigService) DryRun(ctx context.Context, modelKey string, input map[string]any) (any, error) {
	// 1. 依赖检查：宿主适配器在接线时注入
	if s.dryRunner == nil {
		return nil, errcode.ErrInternal.WithMsg("dry-run 功能未启用")
	}
	// 2. 组装试跑快照
	snap, err := s.buildTrialSnapshot(ctx, modelKey)
	if err != nil {
		return nil, err
	}
	// 3. 校验并规范化示例输入：与真实提交走同一套规则，dry-run 才能反映线上行为
	normalized, err := aiNormalizeTrialInput(snap, input)
	if err != nil {
		return nil, err
	}
	// 4. 渲染。runner 连不上是临时故障，返回 503 让运营稍后重试；
	//    其余失败几乎都是插件或配置问题（钩子抛异常、请求描述非法），把脱敏后的原因带给运营
	secrets := s.trialSecrets(ctx, snap)
	res, err := s.dryRunner.DryRun(ctx, snap, normalized)
	if provider.CodeOf(err) == provider.CodeRunnerUnavailable {
		return nil, errcode.ErrRunnerUnavailable
	}
	if err != nil {
		return nil, errcode.ErrConfigInvalid.WithMsg("dry-run 失败：" + aiRedactString(err.Error(), secrets))
	}
	// 5. 宿主已经脱敏，这里再做一次防御性脱敏（凭证明文绝不能出现在任何管理接口响应里）
	return aiRedactResult(res, secrets), nil
}

// TestRun 用试跑快照真实跑一次：创建 is_test 任务（不扣用户积分），返回任务视图。
// 前端随后轮询 GetTestRun 查看进度和结果，用 GetTestTrace 查看每次钩子与 HTTP 的追踪。
func (s *AIConfigService) TestRun(ctx context.Context, adminID uint64, modelKey string, input map[string]any) (*model.GenerationTaskView, error) {
	// 1. 依赖检查
	if s.testTasks == nil {
		return nil, errcode.ErrInternal.WithMsg("试跑功能未启用")
	}
	// 2. 组装试跑快照
	snap, err := s.buildTrialSnapshot(ctx, modelKey)
	if err != nil {
		return nil, err
	}
	// 3. 插件需要鉴权时渠道 Key 必须已设置：试跑会真实调用上游，提前拦下比创建一个必然失败的任务更清楚
	if err := s.checkSnapshotSecret(ctx, snap); err != nil {
		return nil, err
	}
	// 4. 校验并规范化输入
	normalized, err := aiNormalizeTrialInput(snap, input)
	if err != nil {
		return nil, err
	}
	// 5. 创建试跑任务；任务服务负责 is_test 标记、不扣积分、不推送
	view, err := s.testTasks.SubmitTest(ctx, adminID, snap, normalized)
	if err != nil {
		return nil, err
	}
	logger.Info("创建试跑任务", zap.String("model", modelKey), zap.String("channel", snap.Channel.Key),
		zap.Uint64("admin_id", adminID), zap.Uint64("task_id", view.ID))
	return view, nil
}

// GetTestRun 查询试跑任务的当前视图。只能查自己创建的试跑任务，查不到统一返回“任务不存在”。
func (s *AIConfigService) GetTestRun(ctx context.Context, adminID, taskID uint64) (*model.GenerationTaskView, error) {
	// 1. 依赖检查
	if s.testTasks == nil {
		return nil, errcode.ErrInternal.WithMsg("试跑功能未启用")
	}
	// 2. 查询；仓储的“不存在”翻译成任务不存在（404）
	view, err := s.testTasks.GetTestTask(ctx, adminID, taskID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrTaskNotFound
	}
	if err != nil {
		return nil, err
	}
	return view, nil
}

// GetTestTrace 查询试跑任务的执行追踪：每次钩子的输入 / 输出 / utils.log、每次 HTTP 的请求与响应（宿主已脱敏）。
// 别人的任务、非试跑任务、不存在的任务统一返回 ErrTaskNotFound；任务还没有追踪时 steps 为空数组。
func (s *AIConfigService) GetTestTrace(ctx context.Context, adminID, taskID uint64) (*TestTraceView, error) {
	// 1. 依赖检查
	if s.testTasks == nil {
		return nil, errcode.ErrInternal.WithMsg("试跑功能未启用")
	}
	// 2. 查询；仓储的“不存在”翻译成任务不存在（404）
	steps, err := s.testTasks.GetTestTrace(ctx, adminID, taskID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrTaskNotFound
	}
	if err != nil {
		return nil, err
	}
	// 3. steps 永远是数组，前端不用处理 null
	if steps == nil {
		steps = []provider.TraceStep{}
	}
	return &TestTraceView{Steps: steps}, nil
}

// buildTrialSnapshot 用模型草稿（没有草稿用已发布版本）+ 渠道当前配置 + 渠道固定的插件版本组装试跑快照。
// 配置本身校验不通过、渠道 / 插件不可用时直接报错（与发布检查同一套标准），不拿有问题的配置去试跑。
func (s *AIConfigService) buildTrialSnapshot(ctx context.Context, modelKey string) (*provider.Snapshot, error) {
	// 1. 模型 revision：优先最新草稿
	rev, err := s.repo.GetDraft(ctx, model.ConfigTargetModel, modelKey)
	if errors.Is(err, repository.ErrNotFound) {
		rev, err = s.repo.GetPublishedRevision(ctx, model.ConfigTargetModel, modelKey)
	}
	if err != nil {
		return nil, aiNotFound(err)
	}
	// 2. 模型配置校验
	cfg, issues := modelcfg.ParseModel(rev.BodyJSON)
	if len(issues) > 0 || cfg == nil {
		return nil, errcode.ErrConfigInvalid.WithMsg("模型配置校验未通过：" + aiFormatIssues(issues))
	}
	// 3. 渠道与插件版本
	rc, err := s.resolveChannel(ctx, cfg)
	if err != nil {
		return nil, err
	}
	rt, err := provider.RuntimeOf(rc.channel, rc.version)
	if err != nil {
		return nil, errcode.ErrChannelInvalid.WithMsg("渠道配置无法解析：" + err.Error())
	}
	return provider.NewSnapshot(cfg, rt, rev.ID), nil
}

// checkSnapshotSecret 快照里的插件需要宿主注入鉴权时，要求渠道 Key 已设置（ErrChannelSecretUnset）。
func (s *AIConfigService) checkSnapshotSecret(ctx context.Context, snap *provider.Snapshot) error {
	meta := snap.Plugin.Meta
	ch := &model.AIChannel{Key: snap.Channel.Key}
	return s.checkChannelSecret(ctx, &aiResolvedChannel{channel: ch, meta: &meta})
}

// aiNormalizeTrialInput 按 input_schema 校验示例输入，字段级错误合并成一条提示。input 为 nil 视为空对象。
func aiNormalizeTrialInput(snap *provider.Snapshot, input map[string]any) (map[string]any, error) {
	if input == nil {
		input = map[string]any{}
	}
	normalized, ferrs := modelcfg.ValidateInput(snap.Model.InputSchema, input)
	if len(ferrs) > 0 {
		return nil, errcode.ErrTaskInput.WithMsg("示例输入不合法：" + joinFieldErrors(ferrs))
	}
	return normalized, nil
}

// trialSecrets 取出快照里渠道 Key 的明文，仅用于对输出做脱敏；取不到（未设置、没有主密钥）就返回空，
// 这时本来也不会有明文可泄露，所以忽略错误。
func (s *AIConfigService) trialSecrets(ctx context.Context, snap *provider.Snapshot) []string {
	v, err := s.Get(ctx, model.ChannelSecretName(snap.Channel.Key))
	if err != nil || v == "" {
		return nil
	}
	return []string{v}
}

// aiRedactResult 对宿主结果做防御性脱敏：先转成 JSON 做一遍文本级替换（转义后的明文也能替换掉），
// 再转回通用结构交给 modelcfg.Redact 兜底（含 URL 转义形式）。任何一步失败都不返回原结果，宁可丢结果也不泄露凭证。
func aiRedactResult(res any, secrets []string) any {
	b, err := json.Marshal(res)
	if err != nil {
		return nil
	}
	for _, sec := range secrets {
		if sec == "" {
			continue
		}
		esc, _ := json.Marshal(sec) // 字符串一定能编码，忽略错误
		b = bytes.ReplaceAll(b, esc[1:len(esc)-1], []byte(aiRedactMask))
	}
	var generic any
	if err := json.Unmarshal(b, &generic); err != nil {
		return nil
	}
	return modelcfg.Redact(generic, secrets...)
}

// aiRedactString 把文本里出现的凭证明文替换成 ***。
func aiRedactString(text string, secrets []string) string {
	for _, sec := range secrets {
		if sec != "" {
			text = strings.ReplaceAll(text, sec, aiRedactMask)
		}
	}
	return text
}
