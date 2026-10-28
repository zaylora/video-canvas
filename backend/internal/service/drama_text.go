package service

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"video-canvas/internal/drama"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
)

// callDrama JSON校验失败只重试一次，失败请求不计入扣费。
func (s *DramaService) callDrama(ctx context.Context, j *model.DramaJob, snap *provider.Snapshot, step string, vars map[string]any, validate func(any) error) (any, int, error) {
	var invalid error
	for range 2 {
		if e := s.ensureDramaActive(ctx, j); e != nil {
			return nil, 0, e
		}
		if invalid != nil {
			vars["validation_error"] = invalid.Error()
		}
		prompt, e := drama.Prompt(step, vars)
		if e != nil {
			return nil, 0, e
		}
		input, issues := modelcfg.ValidateInput(modelcfg.KindText, snap.Model.Capabilities, map[string]any{"prompt": prompt})
		if len(issues) > 0 {
			return nil, 0, errcode.ErrDramaStepNotReady.WithMsg("模型上下文或参数不足以执行当前步骤")
		}
		result, e := s.submitDramaText(ctx, j, snap, input)
		if e != nil {
			return nil, 0, e
		}
		text, usage := dramaTextOutput(result)
		decoded, e := drama.DecodeOutput(step, []byte(text))
		if e == nil && validate != nil {
			e = validate(decoded)
		}
		if e != nil {
			invalid = e
			continue
		}
		quoted := modelcfg.Quote(snap.Model.Pricing, snap.Model.Capabilities, modelcfg.SpecFromInput(input, snap.Model.Capabilities.System))
		return decoded, modelcfg.Settle(snap.Model.Pricing, quoted, usage), nil
	}
	return nil, 0, errcode.ErrDramaOutputInvalid
}

// submitDramaText 复用插件Submit/Query文本路径，异步文本结果也遵守取消。
func (s *DramaService) submitDramaText(ctx context.Context, j *model.DramaJob, snap *provider.Snapshot, input map[string]any) (*provider.QueryResult, error) {
	if s.executor == nil {
		return nil, errcode.ErrInternal
	}
	result, e := s.executor.Submit(ctx, snap, provider.SubmitInput{Task: provider.TaskRef{ID: j.ID, UserID: j.UserID}, Input: input})
	if e != nil {
		return nil, e
	}
	if result == nil {
		return nil, errcode.ErrInternal
	}
	query := result.Immediate
	task := provider.TaskRef{ID: j.ID, UserID: j.UserID, ProviderTaskID: result.ProviderTaskID, State: result.State}
	for query == nil || query.Status == provider.StatusRunning || query.Status == provider.StatusQueued {
		if e := s.ensureDramaActive(ctx, j); e != nil {
			_ = s.executor.Cancel(ctx, snap, task)
			return nil, e
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
		query, e = s.executor.Query(ctx, snap, task)
		if e != nil {
			return nil, e
		}
		if query != nil && len(query.State) > 0 {
			task.State = query.State
		}
	}
	if query.Status != provider.StatusSucceeded {
		return nil, errcode.ErrInternal.WithMsg("文本模型调用失败")
	}
	return query, nil
}

// dramaTextOutput 合并文本与实际Token用量，忽略非文本产物。
func dramaTextOutput(result *provider.QueryResult) (string, *modelcfg.Usage) {
	var text strings.Builder
	var usage *modelcfg.Usage
	for _, out := range result.Outputs {
		if out.Type != provider.OutputText {
			continue
		}
		text.WriteString(out.Text)
		if out.Usage == nil {
			continue
		}
		if usage == nil {
			usage = &modelcfg.Usage{}
		}
		usage.InputTokens += max(0, out.Usage.InputTokens)
		usage.OutputTokens += max(0, out.Usage.OutputTokens)
	}
	return text.String(), usage
}

// ensureDramaActive 取消后停止后续网络调用与写入。
func (s *DramaService) ensureDramaActive(ctx context.Context, j *model.DramaJob) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	current, e := s.repo.Job(ctx, j.UserID, j.DramaID, j.ID, false)
	if e != nil {
		return e
	}
	if !dramaActive(current.Status) {
		return errcode.ErrTaskNotCancelable
	}
	return nil
}

// dramaRetryAssets 从最近同范围任务恢复本次新建节点，不扩展到旧资产。
func dramaRetryAssets(jobs []model.DramaJob, scope []int) ([]DramaCanvasAsset, error) {
	slices.SortFunc(jobs, func(a, b model.DramaJob) int { return cmp.Compare(b.ID, a.ID) })
	for _, j := range jobs {
		if !dramaSameScope(j.Episodes, scope) {
			continue
		}
		var assets []DramaCanvasAsset
		if j.Step == "asset_prompts" {
			if jsonErr := decodeDramaAssets(j.InputJSON, &assets); jsonErr == nil {
				return assets, nil
			}
		}
		if j.Step == "breakdown" {
			var checkpoint dramaCheckpoint
			if json.Unmarshal(j.Output, &checkpoint) == nil && checkpoint.CanvasCommitted && len(checkpoint.NewAssets) > 0 {
				return checkpoint.NewAssets, nil
			}
		}
	}
	return nil, errcode.ErrDramaStepNotReady.WithMsg("没有待生成提示词的新资产")
}

// dramaSameScope 比较范围集合，顺序差异不影响重试。
func dramaSameScope(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[int]bool{}
	for _, n := range a {
		seen[n] = true
	}
	for _, n := range b {
		if !seen[n] {
			return false
		}
	}
	return true
}

// decodeDramaAssets 拒绝空批次，防止重试请求变成无作用的扣费。
func decodeDramaAssets(raw []byte, out *[]DramaCanvasAsset) error {
	if e := json.Unmarshal(raw, out); e != nil {
		return e
	}
	if len(*out) == 0 {
		return errors.New("新资产列表为空")
	}
	return nil
}
