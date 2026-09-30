package service

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"gorm.io/datatypes"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/repository"
)

// Create 提交生成任务：冻结快照 → 校验输入与素材 → 单个事务内冻结积分并插入 pending 任务。
// idempotencyKey 非空时同一 (用户, key) 只会创建一个任务，重复提交返回已存在的任务且不再冻结积分。
// 返回的任务处于 pending，由 worker 异步提交给上游，接口本身不依赖上游响应速度。kind 支持 video / image / audio / text。
func (s *GenerationTaskService) Create(ctx context.Context, userID uint64, idempotencyKey string, req *model.CreateGenerationTaskReq) (*model.GenerationTaskView, error) {
	// 1. 校验幂等键长度：超过列宽会在插入时才失败，提前按参数错误返回
	if len(idempotencyKey) > maxIdempotencyKeyLen {
		return nil, errcode.ErrInvalidParams.WithMsg("Idempotency-Key 过长")
	}

	// 2. 幂等快速路径：同一个 key 已经创建过任务，直接返回它，不再做任何校验和冻结
	//    （即使模型此后被下线，重复请求也应得到第一次的结果）
	existing, err := s.findIdempotent(ctx, s.repo, userID, idempotencyKey)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return taskView(existing), nil
	}

	// 3. 冻结模型快照（模型 revision + 渠道配置 + 插件版本）：任务此后只按快照执行，运营改配置不影响进行中的任务
	snap, err := s.snapshotFor(ctx, req)
	if err != nil {
		return nil, err
	}

	// 4. 按 input_schema 校验并规范化输入，再校验媒体字段引用的素材都属于当前用户
	input, err := s.prepareInput(ctx, userID, snap, req.Input)
	if err != nil {
		return nil, err
	}

	// 5. 组装任务：积分取模型配置（负数按 0 处理，防止配置错误变成“倒贴积分”）
	task := &model.GenerationTask{UserID: userID, NodeID: req.NodeID, IdempotencyKey: idempotencyKey, Credits: max(snap.Model.Credits, 0)}
	if req.CanvasID > 0 {
		canvasID := uint64(req.CanvasID)
		task.CanvasProjectID = &canvasID
	}
	if err := s.fillTask(task, snap, input); err != nil {
		return nil, err
	}

	// 6. 单个事务冻结积分并插入任务；并发的同 key 请求先提交时返回它的任务
	existing, err = s.insertAndFreeze(ctx, task)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return taskView(existing), nil
	}

	// 7. 事务提交之后才唤醒 worker 和推送，避免对方读到还没提交的数据
	s.Kick()
	s.publish(ctx, task)
	return taskView(task), nil
}

// SubmitTest 创建运营试跑任务（管理端 test-run 用）：is_test=true，不冻结也不扣积分，
// 不校验并发上限，状态变化也不推送给普通用户。快照由调用者传入，这样可以用未发布的草稿配置试跑。
func (s *GenerationTaskService) SubmitTest(ctx context.Context, userID uint64, snap *provider.Snapshot, input map[string]any) (*model.GenerationTaskView, error) {
	// 1. 快照必须有效
	if snap == nil {
		return nil, errcode.ErrInvalidParams.WithMsg("缺少试跑配置")
	}

	// 2. 与正式提交同样的输入校验与素材归属校验（素材必须属于发起试跑的管理员）
	normalized, err := s.prepareInput(ctx, userID, snap, input)
	if err != nil {
		return nil, err
	}

	// 3. 插入任务：credits 记 0（不参与积分与对账），没有幂等键
	task := &model.GenerationTask{UserID: userID, IsTest: true}
	if err := s.fillTask(task, snap, normalized); err != nil {
		return nil, err
	}
	if _, err := s.repo.InsertTask(ctx, task); err != nil {
		return nil, err
	}

	// 4. 唤醒 worker；不推送（is_test 任务对普通用户不可见，管理端自己轮询 GetTestTask）
	s.Kick()
	return taskView(task), nil
}

// findIdempotent 按幂等键查已存在的任务；key 为空或没找到返回 (nil, nil)。q 可以是仓储本身，也可以是事务。
func (s *GenerationTaskService) findIdempotent(ctx context.Context, q repository.GenerationTaskTx, userID uint64, key string) (*model.GenerationTask, error) {
	if key == "" {
		return nil, nil
	}
	t, err := q.FindByIdempotencyKey(ctx, userID, key)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}

// snapshotFor 取模型的当前快照并核对种类：模型不存在 / 未发布 / 已下架 / 渠道或插件不可用统一返回“模型不可用”；
// 模型种类必须与请求一致，避免拿视频模型提交图片任务。
func (s *GenerationTaskService) snapshotFor(ctx context.Context, req *model.CreateGenerationTaskReq) (*provider.Snapshot, error) {
	snap, err := s.registry.Snapshot(ctx, req.ModelID)
	if errors.Is(err, provider.ErrModelUnavailable) {
		return nil, errcode.ErrModelUnavailable
	}
	if err != nil {
		return nil, err
	}
	if snap.Model.Kind != req.Kind {
		return nil, errcode.ErrTaskInput.WithMsg(taskUserVisibleErrPrefix + "模型的生成种类与请求不一致")
	}
	return snap, nil
}

// fillTask 把快照与规范化输入填进任务行：种类 / 模型 / 渠道取快照；状态 pending、version 1；
// next_poll_at = 现在，让 worker 立即领取；deadline 取模型配置，缺省 30 分钟。
func (s *GenerationTaskService) fillTask(task *model.GenerationTask, snap *provider.Snapshot, input map[string]any) error {
	inputJSON, err := json.Marshal(s.storedInput(snap.Model.InputSchema, input))
	if err != nil {
		return err
	}
	snapJSON, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	now := s.now()
	task.Kind = snap.Model.Kind
	task.ModelKey = snap.Model.Key
	task.Provider = providerKeyOf(snap)
	task.Status = model.TaskPending
	task.InputJSON = datatypes.JSON(inputJSON)
	task.ConfigSnapshot = datatypes.JSON(snapJSON)
	task.Version = 1
	task.NextPollAt = now
	task.DeadlineAt = now.Add(taskDeadline(snap))
	return nil
}

// insertAndFreeze 在单个事务里：锁账户 → 幂等复查 → 校验余额与并发上限 → 插入任务 → 冻结积分 → 写 freeze 流水。
// 任何一步失败整体回滚，不会出现“积分冻结了但任务没创建”。命中幂等键（事务内复查或唯一索引冲突）时返回已存在的任务。
func (s *GenerationTaskService) insertAndFreeze(ctx context.Context, task *model.GenerationTask) (*model.GenerationTask, error) {
	userID, key, credits := task.UserID, task.IdempotencyKey, task.Credits
	var existing *model.GenerationTask
	err := s.repo.WithTx(ctx, func(tx repository.GenerationTaskTx) error {
		// 1. 惰性创建积分账户（新用户初始积分来自配置），再对账户行加锁：
		//    同一个用户的并发提交在这里被串行化，余额与并发数的检查因此不会被并发穿透
		if err := tx.EnsureCredit(ctx, userID, s.cfg.InitialCredits); err != nil {
			return err
		}
		acc, err := tx.LockCredit(ctx, userID)
		if err != nil {
			return err
		}
		// 2. 加锁后再查一次幂等键：快速路径到这里之间可能已有同 key 的请求提交成功，命中就直接返回它，不冻结积分
		if existing, err = s.findIdempotent(ctx, tx, userID, key); err != nil || existing != nil {
			return err
		}
		// 3. 可用余额 = 余额 - 冻结，不足返回 402；进行中的任务数不能超过上限，超过返回 429
		if acc.Balance-acc.Frozen < credits {
			return errcode.ErrInsufficientCredits
		}
		active, err := tx.CountActive(ctx, userID)
		if err != nil {
			return err
		}
		if active >= int64(s.maxActiveTasks()) {
			return errcode.ErrTooManyTasks
		}
		// 4. 插入任务。即使前面的复查漏掉了（理论上不会），(user_id, idempotency_key) 唯一索引也会拦住：
		//    inserted=false 时回滚整个事务（此时还没动积分），由外层去取已存在的任务
		inserted, err := tx.InsertTask(ctx, task)
		if err != nil {
			return err
		}
		if !inserted {
			return errIdempotentConflict
		}
		// 5. 冻结积分并写 freeze 流水（流水需要任务 id，所以在插入任务之后）
		if err := tx.AddCredit(ctx, userID, 0, credits); err != nil {
			return err
		}
		_, err = tx.InsertLedger(ctx, &model.CreditLedger{UserID: userID, TaskID: task.ID, Type: model.LedgerFreeze, Amount: credits})
		return err
	})
	// 6. 幂等冲突：事务已回滚，取赢家的任务返回；其它错误原样返回（errcode 直接透传给前端）
	if errors.Is(err, errIdempotentConflict) {
		return s.repo.FindByIdempotencyKey(ctx, userID, key)
	}
	if err != nil {
		return nil, err
	}
	return existing, nil
}

// prepareInput 校验并规范化输入，并确认媒体字段引用的素材都属于该用户。
// 字段级错误统一拼进 ErrTaskInput 的文案，返回 400。
func (s *GenerationTaskService) prepareInput(ctx context.Context, userID uint64, snap *provider.Snapshot, raw map[string]any) (map[string]any, error) {
	// 1. 按 input_schema 校验（必填、枚举、长度、范围）并补默认值
	input, fieldErrs := s.validateInput(snap.Model.InputSchema, raw)
	if len(fieldErrs) > 0 {
		return nil, errcode.ErrTaskInput.WithMsg(taskUserVisibleErrPrefix + joinFieldErrors(fieldErrs))
	}

	// 2. 逐个校验媒体字段的素材：不存在 / 不属于自己统一按“素材不存在”处理，避免暴露别人的素材；
	//    素材种类还必须与字段类型一致（image 字段不能填视频素材）
	var assetErrs []modelcfg.FieldError
	for _, name := range s.mediaFields(snap.Model.InputSchema) {
		v, ok := input[name]
		if !ok || v == nil {
			continue // 可选的媒体字段没传
		}
		fe, err := s.checkAsset(ctx, userID, snap.Model.InputSchema, name, v)
		if err != nil {
			return nil, err
		}
		if fe != nil {
			assetErrs = append(assetErrs, *fe)
		}
	}
	if len(assetErrs) > 0 {
		return nil, errcode.ErrTaskInput.WithMsg(taskUserVisibleErrPrefix + joinFieldErrors(assetErrs))
	}
	return input, nil
}

// storedInput 返回落库用的输入：媒体字段的素材 id 存成十进制字符串。
// 素材 id 存成 JSON 数字，worker 读出来会变成 float64，超过 2^53 就会丢精度；字符串没有这个问题。
func (s *GenerationTaskService) storedInput(schema modelcfg.InputSchema, input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for k, v := range input {
		out[k] = v
	}
	for _, name := range s.mediaFields(schema) {
		if id, ok := toUint64(out[name]); ok {
			out[name] = strconv.FormatUint(id, 10)
		}
	}
	return out
}

// checkAsset 校验一个媒体字段引用的素材：格式错误 / 不存在 / 种类不符返回字段级错误；素材存储故障返回 error。
func (s *GenerationTaskService) checkAsset(ctx context.Context, userID uint64, schema modelcfg.InputSchema, name string, v any) (*modelcfg.FieldError, error) {
	assetID, ok := toUint64(v)
	if !ok {
		return &modelcfg.FieldError{Field: name, Message: "素材 id 格式错误"}, nil
	}
	asset, err := s.assets.Get(ctx, userID, assetID)
	if errors.Is(err, provider.ErrAssetNotFound) {
		return &modelcfg.FieldError{Field: name, Message: "素材不存在"}, nil
	}
	if err != nil {
		return nil, err
	}
	if field, ok := schema.Get(name); ok && asset.Kind != field.Type {
		return &modelcfg.FieldError{Field: name, Message: "素材类型与字段不匹配"}, nil
	}
	return nil, nil
}
