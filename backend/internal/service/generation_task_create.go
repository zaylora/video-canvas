package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"strconv"

	"go.uber.org/zap"
	"gorm.io/datatypes"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/repository"
)

// Create 提交生成任务：冻结快照 → 校验输入与素材 → 按定价算出每个任务的积分 → 按节点顺序逐个创建任务。
// 选了生成数量 N（fanout 参数）时，一次请求拆成 N 个独立任务，第 i 个任务绑定 node_ids[i]；
// 每个任务各自一个事务、各自冻结，某个失败（积分不足、并发已满……）只让这个节点得到错误，不回滚已创建的任务。
// 请求级错误（参数不合法、模型不可用、输入校验失败）整体返回 error，一个任务也不创建。
// idempotencyKey 非空时，第 i 个任务用 key（i=0）或 key#i 去重：重复提交返回已创建的任务，失败的节点会重新尝试。
func (s *GenerationTaskService) Create(ctx context.Context, userID uint64, idempotencyKey string, req *model.CreateGenerationTaskReq) (*model.CreateGenerationTaskResp, error) {
	// 1. 节点列表与幂等键长度：超过列宽会在插入时才失败，提前按参数错误返回
	nodes := req.NodeIDs
	if len(nodes) == 0 {
		nodes = []string{req.NodeID}
	}
	if len(idempotencyKey) > maxIdempotencyKeyLen-len("#9") {
		return nil, errcode.ErrInvalidParams.WithMsg("Idempotency-Key 过长")
	}

	// 2. 幂等快速路径：每个节点都已经有任务时直接返回，不再做任何校验和冻结
	//    （即使模型此后被下线，重复请求也应得到第一次的结果）
	existing, err := s.findIdempotentAll(ctx, userID, idempotencyKey, len(nodes))
	if err != nil {
		return nil, err
	}
	if allFound(existing) {
		return taskItems(nodes, existing, nil), nil
	}

	// 3. 冻结模型快照（模型 revision + 渠道配置 + 插件版本）：任务此后只按快照执行，运营改配置不影响进行中的任务
	snap, err := s.snapshotFor(ctx, req)
	if err != nil {
		return nil, err
	}

	// 4. 按 capabilities 校验并规范化输入，再校验参考素材都属于当前用户
	input, err := s.prepareInput(ctx, userID, snap, req.Input)
	if err != nil {
		return nil, err
	}

	// 5. 生成数量必须与节点数一致；计价（每个任务的积分）在拆分前算，生成数量参数本身不交给插件
	caps := snap.Model.Capabilities
	n := modelcfg.FanoutCount(caps, input)
	if n != len(nodes) {
		return nil, errcode.ErrInvalidParams.WithMsg(fmt.Sprintf("node_ids 的数量（%d）必须等于生成数量（%d）", len(nodes), n))
	}
	one := max(modelcfg.Quote(snap.Model.Pricing, caps, modelcfg.SpecFromInput(input, caps.System)), 0)
	if name := modelcfg.FanoutParam(caps); name != "" {
		delete(input, name)
	}

	// 6. 按节点顺序逐个创建
	errs, err := s.createAll(ctx, userID, idempotencyKey, nodes, existing, func() *model.GenerationTask {
		return &model.GenerationTask{UserID: userID, Credits: one}
	}, req, snap, input, n)
	if err != nil {
		return nil, err
	}
	return taskItems(nodes, existing, errs), nil
}

// createAll 按节点顺序逐个创建任务，结果写回 existing：已存在的原样保留，其余各自冻结。
// 业务错误（积分不足、并发已满……）只落在这个节点上；基础设施故障（数据库等）不再继续创建：
// 一个任务都还没有时整体返回错误，否则剩下的节点按内部错误返回。
func (s *GenerationTaskService) createAll(ctx context.Context, userID uint64, key string, nodes []string, existing []*model.GenerationTask,
	newTask func() *model.GenerationTask, req *model.CreateGenerationTaskReq, snap *provider.Snapshot, input map[string]any, n int) ([]error, error) {
	errs := make([]error, len(nodes))
	for i, nodeID := range nodes {
		if existing[i] != nil {
			continue
		}
		task := newTask()
		task.NodeID, task.IdempotencyKey = nodeID, idemKeyAt(key, i)
		existing[i], errs[i] = s.createOne(ctx, task, req, snap, taskInput(input, snap.Model.Capabilities, n))
		var biz *errcode.Error
		if errs[i] == nil || errors.As(errs[i], &biz) {
			continue
		}
		if !anyFound(existing) {
			return nil, errs[i]
		}
		for j := i + 1; j < len(nodes); j++ {
			errs[j] = errs[i]
		}
		break
	}
	return errs, nil
}

func anyFound(tasks []*model.GenerationTask) bool {
	for _, t := range tasks {
		if t != nil {
			return true
		}
	}
	return false
}

// createOne 创建一个任务：组装 → 单个事务冻结积分并插入 → 唤醒 worker 并推送。命中幂等键时返回已存在的任务。
func (s *GenerationTaskService) createOne(ctx context.Context, task *model.GenerationTask, req *model.CreateGenerationTaskReq, snap *provider.Snapshot, input map[string]any) (*model.GenerationTask, error) {
	if req.CanvasID > 0 {
		canvasID := uint64(req.CanvasID)
		task.CanvasProjectID = &canvasID
	}
	if err := s.fillTask(task, snap, input); err != nil {
		return nil, err
	}
	existing, err := s.insertAndFreeze(ctx, task)
	if err != nil || existing != nil {
		return existing, err
	}
	// 事务提交之后才唤醒 worker 和推送，避免对方读到还没提交的数据
	s.Kick()
	s.publish(ctx, task)
	return task, nil
}

// taskInput 返回单个任务的输入。拆成多个任务时，每个任务带自己的随机种子（模型自己有 seed 参数时尊重用户的取值），
// 否则 N 个任务可能出一模一样的结果；插件决定怎么把 seed 交给上游。
func taskInput(input map[string]any, caps modelcfg.Capabilities, n int) map[string]any {
	out := make(map[string]any, len(input)+1)
	for k, v := range input {
		out[k] = v
	}
	if _, hasSeed := caps.Params.Get("seed"); n > 1 && !hasSeed {
		out["seed"] = float64(rand.Int32N(1<<31-1) + 1) //nolint:gosec // 生成结果的随机种子，不涉及安全
	}
	return out
}

// idemKeyAt 是第 i 个任务的幂等键：第一个沿用原 key，其余追加 #i；key 为空时都为空（不去重）。
func idemKeyAt(key string, i int) string {
	if key == "" || i == 0 {
		return key
	}
	return key + "#" + strconv.Itoa(i)
}

// findIdempotentAll 按每个节点的幂等键查已存在的任务；key 为空时全是 nil。
func (s *GenerationTaskService) findIdempotentAll(ctx context.Context, userID uint64, key string, n int) ([]*model.GenerationTask, error) {
	out := make([]*model.GenerationTask, n)
	for i := range out {
		t, err := s.findIdempotent(ctx, s.repo, userID, idemKeyAt(key, i))
		if err != nil {
			return nil, err
		}
		out[i] = t
	}
	return out, nil
}

func allFound(tasks []*model.GenerationTask) bool {
	for _, t := range tasks {
		if t == nil {
			return false
		}
	}
	return len(tasks) > 0
}

// taskItems 按节点顺序组装响应：有任务给任务，否则给这个节点的错误。
// 业务错误（errcode）原样带出 HTTP 状态与错误码，前端可以继续按状态码给文案；其它错误记日志后按内部错误返回。
func taskItems(nodes []string, tasks []*model.GenerationTask, errs []error) *model.CreateGenerationTaskResp {
	items := make([]model.CreateTaskItem, len(nodes))
	for i, nodeID := range nodes {
		items[i].NodeID = nodeID
		if tasks[i] != nil {
			items[i].Task = taskView(tasks[i])
			continue
		}
		var e *errcode.Error
		if errs == nil || !errors.As(errs[i], &e) {
			if errs != nil {
				logger.Error("创建生成任务失败", zap.String("node_id", nodeID), zap.Error(errs[i]))
			}
			e = errcode.ErrInternal
		}
		items[i].Error = &model.TaskItemError{Status: e.HTTPStatus(), Code: e.Code, Message: e.Msg}
	}
	return &model.CreateGenerationTaskResp{Items: items}
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
	inputJSON, err := json.Marshal(s.storedInput(input))
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
	// 事务外先读初始积分与并发上限：它们是设置读取，不属于积分事务，也避免在持有账户行锁时多做查询
	initial, err := s.initialCredits(ctx)
	if err != nil {
		return nil, err
	}
	maxActive, err := s.maxActiveTasks(ctx, userID)
	if err != nil {
		return nil, err
	}
	var existing *model.GenerationTask
	err = s.repo.WithTx(ctx, func(tx repository.GenerationTaskTx) error {
		// 1. 惰性创建积分账户（初始积分来自系统设置，缺省 config；真正建出新账户时仓储会在同一事务补写 initial 流水），
		//    再对账户行加锁：同一个用户的并发提交在这里被串行化，余额与并发数的检查因此不会被并发穿透
		if err := tx.EnsureCredit(ctx, userID, initial); err != nil {
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
		if active >= int64(maxActive) {
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
		_, err = tx.InsertLedger(ctx, &model.CreditLedger{UserID: userID, TaskID: model.TaskIDPtr(task.ID), Type: model.LedgerFreeze, Amount: credits})
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

// prepareInput 校验并规范化输入，补上宿主注入的文本参数，并确认参考素材都属于该用户、种类与大小合规。
// 字段级错误统一拼进 ErrTaskInput 的文案，返回 400。
func (s *GenerationTaskService) prepareInput(ctx context.Context, userID uint64, snap *provider.Snapshot, raw map[string]any) (map[string]any, error) {
	// 1. 按模型能力校验（提示词、生成方式、生成参数、素材数量）并补默认值
	caps := snap.Model.Capabilities
	input, fieldErrs := s.validateInput(snap.Model.Kind, caps, raw)
	if len(fieldErrs) > 0 {
		return nil, errcode.ErrTaskInput.WithMsg(taskUserVisibleErrPrefix + joinFieldErrors(fieldErrs))
	}

	// 2. 文本模型：固定系统提示与最大输出由配置决定，用户的输入改不了
	if snap.Model.Kind == modelcfg.KindText {
		if caps.System != "" {
			input["system"] = caps.System
		}
		if caps.Context != nil {
			input["max_tokens"] = caps.Context.Output
		}
	}

	// 3. 逐个校验参考素材：不存在 / 不属于自己统一按“素材不存在”处理，避免暴露别人的素材；
	//    素材种类必须与数组一致（images 里不能放视频），单个大小不能超过 refs.<种类>.max_mb
	var assetErrs []modelcfg.FieldError
	for _, ref := range modelcfg.MediaRefs(input) {
		fe, err := s.checkAsset(ctx, userID, caps, ref)
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

// storedInput 返回落库用的输入：参考素材数组里的素材 id 存成十进制字符串。
// 素材 id 存成 JSON 数字，worker 读出来会变成 float64，超过 2^53 就会丢精度；字符串没有这个问题。
func (s *GenerationTaskService) storedInput(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for k, v := range input {
		out[k] = v
	}
	for _, m := range modelcfg.MediaKinds {
		if _, ok := input[m.Key]; !ok {
			continue
		}
		var ids []string
		for _, r := range modelcfg.MediaRefs(input) {
			if r.Key == m.Key {
				ids = append(ids, strconv.FormatUint(r.ID, 10))
			}
		}
		out[m.Key] = ids
	}
	return out
}

// checkAsset 校验一个参考素材：不存在 / 种类不符 / 超过大小上限返回字段级错误；素材存储故障返回 error。
func (s *GenerationTaskService) checkAsset(ctx context.Context, userID uint64, caps modelcfg.Capabilities, ref modelcfg.MediaRef) (*modelcfg.FieldError, error) {
	asset, err := s.assets.Get(ctx, userID, ref.ID)
	if errors.Is(err, provider.ErrAssetNotFound) {
		return &modelcfg.FieldError{Field: ref.Key, Message: "素材不存在"}, nil
	}
	if err != nil {
		return nil, err
	}
	if asset.Kind != ref.Kind {
		return &modelcfg.FieldError{Field: ref.Key, Message: "素材类型不匹配"}, nil
	}
	if mb := caps.Refs.Of(ref.Kind).MaxMB; mb > 0 && asset.ByteSize > int64(mb)<<20 {
		return &modelcfg.FieldError{Field: ref.Key, Message: fmt.Sprintf("单个素材不能超过 %d MB", mb)}, nil
	}
	return nil, nil
}
