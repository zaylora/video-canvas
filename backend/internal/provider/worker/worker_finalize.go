package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
)

// errInvalidOutput 表示产物本身不合规（类型未知、缺少地址、文本模型给了 URL 产物等），重试也不会变好，直接失败。
var errInvalidOutput = errors.New("产物不合规")

// finalize 处理 finalizing 任务：拿到产物列表，逐个转存（文本与宿主已落库的素材不下载），全部完成后结算。
func (w *Worker) finalize(ctx context.Context, t *model.GenerationTask, snap *provider.Snapshot) {
	log := w.taskLog(t)

	// 1. 取产物列表：同步接口的即时结果已在 provider_result 里，直接用，不再调用上游；
	//    异步任务重新查询一次拿产物地址（不把上游 URL 存进 DB，总是用最新的 URL）
	outs, ok := w.finalOutputs(ctx, t, snap)
	if !ok {
		return
	}
	if err := validateOutputs(outs, snap.Model.Kind); err != nil {
		log.Error("产物不合规，直接失败", zap.Error(err))
		w.fail(ctx, t, snap, failure{class: provider.ClassTerminal, raw: err.Error()})
		return
	}

	// 2. 逐个处理（已成功的跳过），失败按退避重试，耗尽后失败退款
	tctx, tcancel := context.WithTimeout(ctx, w.opts.TransferTimeout)
	defer tcancel()
	outputs, err := w.transferAll(tctx, t, snap, outs)
	if err != nil {
		w.retryTransfer(ctx, t, err)
		return
	}

	// 3. 全部完成：一个事务里写 output_json、置 succeeded、结算积分（在 store.Complete 内完成）
	applied, err := w.store.Complete(ctx, t, outputs, totalUsage(outs))
	if err != nil {
		// 事务失败：产物已转存好并记在内存里，租约过期后重试时不会重复转存
		log.Error("完成任务的事务失败，租约过期后重试", zap.Error(err))
		return
	}
	w.forgetTask(t.ID)
	if !applied {
		log.Warn("转存完成时任务已被取消或超时，已转存的素材成为孤儿素材")
		return
	}
	log.Info("任务完成", zap.Int("outputs", len(outputs)))
}

// finalOutputs 取待转存的产物；ok=false 表示已经按错误处理完（重试或失败），调用方直接返回。
func (w *Worker) finalOutputs(ctx context.Context, t *model.GenerationTask, snap *provider.Snapshot) ([]provider.Output, bool) {
	if len(t.ProviderResult) > 0 {
		var outs []provider.Output
		if err := json.Unmarshal(t.ProviderResult, &outs); err != nil {
			// 同步结果只有这一份，上游不能再查；损坏只能失败退款
			w.taskLog(t).Error("任务的 provider_result 损坏，直接失败", zap.Error(err))
			w.fail(ctx, t, snap, failure{class: provider.ClassTerminal, raw: "invalid provider_result"})
			return nil, false
		}
		return outs, true
	}
	return w.queryOutputs(ctx, t, snap)
}

// queryOutputs 重新查询一次上游拿产物地址。
func (w *Worker) queryOutputs(ctx context.Context, t *model.GenerationTask, snap *provider.Snapshot) ([]provider.Output, bool) {
	qctx, qcancel := context.WithTimeout(ctx, w.opts.OperationTimeout)
	res, err := w.exec.Query(qctx, snap, taskRef(t, w.providerState(t)))
	qcancel()
	if err != nil {
		switch {
		case w.handleRunnerError(ctx, t, snap, err):
		case provider.ClassOf(err) == provider.ClassRetryable:
			w.resetCrash(t.ID)
			w.retryTransfer(ctx, t, fmt.Errorf("查询产物地址失败：%w", err))
		default:
			w.resetCrash(t.ID)
			w.fail(ctx, t, snap, failureOf(err))
		}
		return nil, false
	}
	w.resetCrash(t.ID)
	switch {
	case res == nil:
		w.fail(ctx, t, snap, failure{class: provider.ClassTerminal, raw: "empty query result"})
	case res.Status == provider.StatusFailed:
		w.fail(ctx, t, snap, failureOfResult(res))
	case res.Status != provider.StatusSucceeded:
		w.retryTransfer(ctx, t, fmt.Errorf("上游状态回退为 %s", res.Status))
	default:
		return res.Outputs, true
	}
	return nil, false
}

// validateOutputs 检查产物能否处理：至少一个；类型已知；url 产物有地址且不是文本（文本模型没有素材可转存）。
// 宿主已按契约校验过，这里是 provider_result 从 DB 读回来之后的防御。
func validateOutputs(outs []provider.Output, modelKind string) error {
	if len(outs) == 0 {
		return fmt.Errorf("%w：没有产物", errInvalidOutput)
	}
	for i, o := range outs {
		switch o.Type {
		case provider.OutputText, provider.OutputAsset:
		case provider.OutputURL:
			if o.URL == "" {
				return fmt.Errorf("%w：第 %d 个 url 产物没有地址", errInvalidOutput, i+1)
			}
			if mediaOf(o, modelKind) == model.KindText {
				return fmt.Errorf("%w：第 %d 个产物是文本，却给了下载地址", errInvalidOutput, i+1)
			}
		default:
			return fmt.Errorf("%w：第 %d 个产物类型未知（%s）", errInvalidOutput, i+1, o.Type)
		}
	}
	return nil
}

// mediaOf 取产物的媒体类型：插件声明的优先，缺省取模型 kind。
func mediaOf(o provider.Output, modelKind string) string {
	if o.MediaType != "" {
		return o.MediaType
	}
	return modelKind
}

// transferAll 按顺序处理全部产物。已完成的前 N 个从内存进度里复用，只处理剩下的；
// 任何一个失败就返回错误，已完成的进度保留给下次重试。
func (w *Worker) transferAll(ctx context.Context, t *model.GenerationTask, snap *provider.Snapshot, outs []provider.Output) ([]model.TaskOutput, error) {
	w.memMu.Lock()
	done := append([]model.TaskOutput(nil), w.transferred[t.ID]...)
	w.memMu.Unlock()
	if len(done) > len(outs) {
		done = done[:len(outs)] // 上游产物变少了（异常），以本次为准
	}

	for i := len(done); i < len(outs); i++ {
		o, err := w.transferOne(ctx, t, snap, indexedOutput{Output: outs[i], index: i})
		if err != nil {
			return nil, fmt.Errorf("转存第 %d 个产物失败：%w", i+1, err)
		}
		done = append(done, o)
		w.memMu.Lock()
		w.transferred[t.ID] = append([]model.TaskOutput(nil), done...)
		w.memMu.Unlock()
	}
	return done, nil
}

// indexedOutput 是带序号（从 0 开始）的产物，序号用来生成稳定的文件名。
type indexedOutput struct {
	provider.Output
	index int
}

// transferOne 把一个产物变成 TaskOutput：文本直接写正文；宿主已落库的素材直接引用；只有 url 产物需要下载转存。
// 类型已由 validateOutputs 检查过。
func (w *Worker) transferOne(ctx context.Context, t *model.GenerationTask, snap *provider.Snapshot, out indexedOutput) (model.TaskOutput, error) {
	switch out.Type {
	case provider.OutputText:
		return model.TaskOutput{MediaType: model.KindText, Text: out.Text}, nil
	case provider.OutputAsset:
		return model.TaskOutput{
			AssetID: out.AssetID, URL: out.AssetURL, MediaType: mediaOf(out.Output, snap.Model.Kind),
			DurationMs: out.DurationMs, Width: out.Width, Height: out.Height,
		}, nil
	}
	return w.downloadOne(ctx, t, snap, out)
}

// downloadOne 下载一个 url 产物（Download 内部校验域名白名单 / 内网 IP / 重定向）并写入自有存储。
func (w *Worker) downloadOne(ctx context.Context, t *model.GenerationTask, snap *provider.Snapshot, out indexedOutput) (model.TaskOutput, error) {
	dl, err := w.exec.Download(ctx, snap, out.URL)
	if err != nil {
		return model.TaskOutput{}, fmt.Errorf("下载：%w", err)
	}
	defer dl.Body.Close()

	kind := mediaOf(out.Output, snap.Model.Kind)
	// 下载响应头的类型比插件声明的更可信（插件只是照抄上游文档）
	mime := dl.ContentType
	if mime == "" {
		mime = out.Mime
	}
	fileName := dl.FileName
	if fileName == "" {
		fileName = fmt.Sprintf("task-%d-%d%s", t.ID, out.index+1, fileExt(out.Mime, dl.ContentType, out.URL))
	}
	asset, url, err := w.saver.SaveGenerated(ctx, provider.SaveGeneratedInput{
		UserID:   t.UserID,
		TaskID:   t.ID,
		Kind:     kind,
		MimeType: mime,
		FileName: fileName,
		Body:     dl.Body,
		MaxBytes: w.opts.MaxDownloadBytes,
	})
	if err != nil {
		return model.TaskOutput{}, fmt.Errorf("保存：%w", err)
	}
	return model.TaskOutput{
		AssetID:    asset.ID,
		URL:        url,
		MediaType:  kind,
		DurationMs: asset.DurationMs,
		Width:      asset.Width,
		Height:     asset.Height,
	}, nil
}

// retryTransfer 转存阶段的一次失败：次数没耗尽就退避重试（必须在上游 URL 有效期内完成，
// 默认最多 10 次、总窗口约 40 分钟），耗尽后置 failed（transfer_failed）并退款。
func (w *Worker) retryTransfer(ctx context.Context, t *model.GenerationTask, err error) {
	attempts := t.PollAttempts + 1
	if attempts >= w.opts.TransferMaxAttempts {
		w.taskLog(t).Error("转存重试次数耗尽，任务失败并退款", zap.Int("attempts", attempts), zap.Error(err))
		w.failTransfer(ctx, t)
		return
	}
	delay := w.jitter(transferBackoff(attempts), defaultJitter)
	w.taskLog(t).Warn("转存失败，稍后重试", zap.Int("attempts", attempts), zap.Duration("delay", delay), zap.Error(err))
	w.retry(ctx, t, attempts, w.opts.Now().Add(delay))
}

// failTransfer 转存彻底失败：置 failed（transfer_failed）并退款。
func (w *Worker) failTransfer(ctx context.Context, t *model.GenerationTask) {
	w.forgetTask(t.ID)
	code, msg := failureFor(failTransfer, "")
	if _, err := w.store.Fail(ctx, t, code, msg); err != nil {
		w.taskLog(t).Error("标记转存失败时出错，租约过期后重试", zap.Error(err))
	}
}

// totalUsage 汇总产物里的 Token 用量；没有任何产物带用量时返回 nil（由任务服务按冻结额结算）。
func totalUsage(outs []provider.Output) *modelcfg.Usage {
	var sum *modelcfg.Usage
	for _, o := range outs {
		if o.Usage == nil {
			continue
		}
		if sum == nil {
			sum = &modelcfg.Usage{}
		}
		sum.InputTokens += max(o.Usage.InputTokens, 0)
		sum.OutputTokens += max(o.Usage.OutputTokens, 0)
	}
	return sum
}
