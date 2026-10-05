package service

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/repository"
)

// AdminCreditTx 提供积分账户的事务能力，真实实现是 repository.GenerationTaskRepository：
// 积分调整与任务冻结 / 结算共用同一套行锁原语（LockCredit），才能互相串行、不丢更新。
type AdminCreditTx interface {
	// WithTx 在一个事务里执行 fn，fn 返回错误则整体回滚。
	WithTx(ctx context.Context, fn func(tx repository.GenerationTaskTx) error) error
}

// UserInvalidator 清除用户状态缓存，由 UserService 实现。
type UserInvalidator interface {
	// InvalidateUser 删除用户的状态缓存，让封禁、启用、改并发上限立即对鉴权和提交任务生效。
	InvalidateUser(ctx context.Context, id uint64)
}

// ActiveTaskCanceler 取消用户全部进行中的任务并退还冻结，由 GenerationTaskService 实现（复用它的取消与结算流程）。
type ActiveTaskCanceler interface {
	// CancelActiveByUser 逐个取消用户进行中的正式任务；单个失败不影响其他任务，失败的 id 在结果里返回。
	// 只有查询进行中任务本身失败这类整体性错误才返回 error。
	CancelActiveByUser(ctx context.Context, userID uint64) (*CancelActiveResult, error)
}

// CancelActiveResult 是批量取消的结果。
type CancelActiveResult struct {
	Canceled int      // 成功取消并退还冻结的任务数
	Failed   []uint64 // 取消失败的任务 id
}

// UserDisconnector 断开用户的全部 WebSocket 连接，由 ws.Hub 实现。
type UserDisconnector interface {
	// DisconnectUser 断开用户的全部连接，返回断开的数量。
	DisconnectUser(userID uint64) int
}

// 编译期确认：用户服务、任务服务分别满足缓存失效与取消任务的依赖接口。
var (
	_ UserInvalidator    = (*UserService)(nil)
	_ ActiveTaskCanceler = (*GenerationTaskService)(nil)
)

// AdminControls 是后台用户管控需要的依赖。Users / Tasks / Conns 为 nil 时跳过对应动作（只用于单测裁剪），Credits 调整积分时必须有。
type AdminControls struct {
	Credits AdminCreditTx
	Users   UserInvalidator
	Tasks   ActiveTaskCanceler
	Conns   UserDisconnector
}

// AdminUserOption 是 AdminUserService 的可选配置。
type AdminUserOption func(*AdminUserService)

// WithAdminControls 注入管控依赖。
func WithAdminControls(c AdminControls) AdminUserOption {
	return func(s *AdminUserService) { s.ctl = c }
}

// 业务上限与文案。
const (
	maxBatchIDs        = 200 // 批量接口一次最多处理的用户数（去重后）
	maxCreditNoteRunes = 100 // 积分调整备注的最大字数
	maxLimitUpper      = 64  // 并发上限的最大值（与系统设置一致）

	msgForbidAdminTarget = "admin 不能操作管理员账号"
	msgForbidSelfBan     = "不能封禁自己"
	msgForbidSelfAdjust  = "admin 不能给自己调整积分"
	msgItemInternal      = "操作失败，请稍后重试"
)

// adminAction 是需要权限判断的后台动作。
type adminAction int

const (
	actCredit adminAction = iota // 调整积分
	actLimit                     // 改并发上限
	actBan                       // 封禁
	actUnban                     // 启用
)

// isStaff 判断角色是否属于管理员（admin / super_admin）。
func isStaff(role string) bool {
	return role == model.RoleAdmin || role == model.RoleSuperAdmin
}

// loadActor 读操作人的最新账号（直接读库，不走缓存，权限判断要用最新角色）。
func (s *AdminUserService) loadActor(ctx context.Context, actorID uint64) (*model.User, error) {
	u, err := s.repo.GetByID(ctx, actorID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrForbidden.WithMsg("操作人账号不存在")
	}
	return u, err
}

// loadTarget 读被操作的用户，不存在返回 ErrUserNotFound。
func (s *AdminUserService) loadTarget(ctx context.Context, id uint64) (*model.User, error) {
	u, err := s.repo.GetByID(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrUserNotFound
	}
	return u, err
}

// authorize 按契约 §3 的权限规则判断 actor 能否对 target 执行动作，违反返回 ErrForbidden（Msg 是能直接展示给用户的原因）：
//   - 任何人不能封禁自己；
//   - admin 不能给自己调整积分（super_admin 可以）；
//   - admin 不能操作 admin / super_admin 账号；自己改自己的并发上限是例外（运营调自己的限额无风险）。
//
// super_admin 对其他账号不受角色限制。
func authorize(actor, target *model.User, act adminAction) error {
	self := actor.ID == target.ID
	if !isStaff(actor.Role) {
		return errcode.ErrForbidden
	}
	if act == actBan && self {
		return errcode.ErrForbidden.WithMsg(msgForbidSelfBan)
	}
	if act == actCredit && self && actor.Role == model.RoleAdmin {
		return errcode.ErrForbidden.WithMsg(msgForbidSelfAdjust)
	}
	selfLimit := self && act == actLimit // 自己改自己的并发上限不受“不能操作管理员”限制
	if actor.Role == model.RoleAdmin && isStaff(target.Role) && !selfLimit {
		return errcode.ErrForbidden.WithMsg(msgForbidAdminTarget)
	}
	return nil
}

// loadAndAuthorize 读目标与操作人并做权限判断：先判断目标是否存在（不存在比无权限更该先告诉调用方）。
func (s *AdminUserService) loadAndAuthorize(ctx context.Context, actorID, targetID uint64, act adminAction) (actor, target *model.User, err error) {
	target, err = s.loadTarget(ctx, targetID)
	if err != nil {
		return nil, nil, err
	}
	actor, err = s.loadActor(ctx, actorID)
	if err != nil {
		return nil, nil, err
	}
	if err := authorize(actor, target, act); err != nil {
		return nil, nil, err
	}
	return actor, target, nil
}

// validCreditNote 校验并整理备注：去首尾空白后必须是 1..100 个字。
func validCreditNote(note string) (string, error) {
	note = strings.TrimSpace(note)
	if n := utf8.RuneCountInString(note); n < 1 || n > maxCreditNoteRunes {
		return "", errcode.ErrCreditAdjustInvalid.WithMsg("备注必填，1 到 100 个字")
	}
	return note, nil
}

// creditDelta 按调整方式算出对余额的增量（delta）。三种方式都以“可用积分”（余额 - 冻结）为准：
// add / sub 在可用上加减；set 把可用设为 amount，即余额 = amount + 冻结，所以永远不会出现“低于冻结额”的报错。
func creditDelta(mode string, amount int, acc *model.UserCredit) (int, error) {
	available := acc.Balance - acc.Frozen
	switch mode {
	case model.CreditModeAdd:
		if amount <= 0 {
			return 0, errcode.ErrCreditAdjustInvalid.WithMsg("增加的积分必须大于 0")
		}
		return amount, nil
	case model.CreditModeSub:
		if amount <= 0 {
			return 0, errcode.ErrCreditAdjustInvalid.WithMsg("扣减的积分必须大于 0")
		}
		if amount > available {
			return 0, errcode.ErrCreditAdjustInvalid.WithMsg("扣减超过可用积分，最多可扣 " + itoa(max(available, 0)))
		}
		return -amount, nil
	case model.CreditModeSet:
		return amount - available, nil
	}
	return 0, errcode.ErrCreditAdjustInvalid.WithMsg("不支持的调整方式")
}

// AdjustCredits 调整用户的积分，返回调整后的 {balance, frozen, available}。
// 与任务冻结 / 结算并发安全：整个“读余额 → 算 delta → 改余额 → 写流水”在一个事务里，并先对积分账户行加锁。
func (s *AdminUserService) AdjustCredits(ctx context.Context, actorID, targetID uint64, req *model.AdjustCreditsReq) (*model.AdminCreditView, error) {
	// 1. 校验入参：amount 不能缺（缺字段与 0 要区分，否则漏传的 set 会被当成“设为 0”）、不能为负；备注必填 1..100 字
	if req.Amount == nil || *req.Amount < 0 {
		return nil, errcode.ErrCreditAdjustInvalid.WithMsg("数值必须是不小于 0 的整数")
	}
	amount := *req.Amount
	note, err := validCreditNote(req.Note)
	if err != nil {
		return nil, err
	}
	// 2. 目标必须存在，并按权限规则判断操作人能否调整
	actor, _, err := s.loadAndAuthorize(ctx, actorID, targetID, actCredit)
	if err != nil {
		return nil, err
	}
	// 3. 读初始积分（事务外读：事务里不做别的读取；只在老用户还没有积分账户时才会用到）
	initial, err := s.defaults.InitialCredits(ctx)
	if err != nil {
		return nil, err
	}

	// 4. 事务：确保账户存在（无账户则建并写 initial 流水）→ 行锁 → 算 delta → 改余额 → 写 admin_adjust 流水。
	//    LockCredit 与任务冻结、结算用的是同一把行锁，所以并发的冻结 / 结算会等调整提交后再基于新余额计算，不会丢更新。
	//    校验失败（如扣减超过可用）直接返回错误，整个事务回滚，连同刚建的账户一起撤销
	var view *model.AdminCreditView
	var delta int
	err = s.ctl.Credits.WithTx(ctx, func(tx repository.GenerationTaskTx) error {
		if err := tx.EnsureCredit(ctx, targetID, initial); err != nil {
			return err
		}
		acc, err := tx.LockCredit(ctx, targetID)
		if err != nil {
			return err
		}
		if delta, err = creditDelta(req.Mode, amount, acc); err != nil {
			return err
		}
		if err := tx.AddCredit(ctx, targetID, delta, 0); err != nil {
			return err
		}
		// task_id 为空：管理员调整不属于任何任务，不受 (task_id, type) 唯一索引约束，不会被当成重复流水吞掉
		if _, err := tx.InsertLedger(ctx, &model.CreditLedger{
			UserID: targetID, Type: model.LedgerAdminAdjust, Amount: delta, OperatorID: &actor.ID, Note: note,
		}); err != nil {
			return err
		}
		balance := acc.Balance + delta // 账户行已加锁，这个值就是提交后的真实余额
		view = &model.AdminCreditView{Balance: balance, Frozen: acc.Frozen, Available: balance - acc.Frozen}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// 5. 提交后写审计：detail 记 {mode, amount, delta, note}（前端用 note 展示）
	adminAudit(ctx, s.audit, actorID, model.AdminAuditCreditAdjust, model.AdminAuditTargetUser, targetID,
		map[string]any{"mode": req.Mode, "amount": amount, "delta": delta, "note": note})
	return view, nil
}

// SetMaxActiveTasks 设置单用户并发上限；nil 表示改回全局默认。允许 1..64。
func (s *AdminUserService) SetMaxActiveTasks(ctx context.Context, actorID, targetID uint64, limit *int) error {
	// 1. 范围校验（handler 也会校验，这里是 service 自己的底线，批量 / 内部调用同样受约束）
	if limit != nil && (*limit < 1 || *limit > maxLimitUpper) {
		return errcode.ErrInvalidParams.WithMsg("并发上限必须在 1 到 64 之间")
	}
	// 2. 目标存在 + 权限（自己改自己的上限是允许的）
	_, target, err := s.loadAndAuthorize(ctx, actorID, targetID, actLimit)
	if err != nil {
		return err
	}
	// 3. 写库。nil 要以无类型 nil 传给 GORM 才会写成 NULL，带类型的 (*int)(nil) 不保证
	var value any
	if limit != nil {
		value = *limit
	}
	if err := s.repo.Update(ctx, targetID, map[string]any{"max_active_tasks": value}); err != nil {
		return err
	}
	// 4. 清缓存：提交任务读的是库，但 State 缓存里也带着该字段，统一清掉免得读到旧值
	s.invalidate(ctx, targetID)
	// 5. 审计记前后值（null 表示用全局默认）
	adminAudit(ctx, s.audit, actorID, model.AdminAuditUserLimit, model.AdminAuditTargetUser, targetID,
		map[string]any{"from": target.MaxActiveTasks, "to": limit})
	return nil
}

// SetStatus 封禁 / 启用用户。封禁时可同时取消其进行中的任务并退还冻结；启用时 cancelActive 被忽略。
// 取消任务是逐个处理的：部分失败体现在返回值里，已取消的不回滚，封禁本身也不回滚。
func (s *AdminUserService) SetStatus(ctx context.Context, actorID, targetID uint64, status string, cancelActive bool) (*model.AdminStatusResult, error) {
	// 1. 状态只能是 active / disabled
	if status != model.UserStatusActive && status != model.UserStatusDisabled {
		return nil, errcode.ErrInvalidParams.WithMsg("状态只能是 active 或 disabled")
	}
	ban := status == model.UserStatusDisabled
	act := actUnban
	if ban {
		act = actBan
	}
	// 2. 目标存在 + 权限（不能封禁自己；admin 不能操作管理员）
	_, target, err := s.loadAndAuthorize(ctx, actorID, targetID, act)
	if err != nil {
		return nil, err
	}
	// 3. 写状态，随后清缓存：鉴权中间件读缓存，不清的话封禁最多 30 分钟后才生效
	if err := s.repo.Update(ctx, targetID, map[string]any{"status": status}); err != nil {
		return nil, err
	}
	s.invalidate(ctx, targetID)

	res := &model.AdminStatusResult{Status: status, FailedTaskIDs: []uint64{}}
	if ban {
		// 4. 断开该用户全部 WebSocket：已建立的连接不会再鉴权，不踢掉就还能继续收推送
		if s.ctl.Conns != nil {
			s.ctl.Conns.DisconnectUser(targetID)
		}
		// 5. 要求取消时：复用任务服务的取消流程（状态迁移 + 结算流水 + 退还冻结在同一事务里）。
		//    封禁已生效，取消出错不回滚封禁，只在返回值里体现，让运营知道还有任务需要手动处理
		if cancelActive && s.ctl.Tasks != nil {
			r, err := s.ctl.Tasks.CancelActiveByUser(ctx, targetID)
			if err != nil {
				logger.Error("封禁后取消进行中任务失败", zap.Error(err), zap.Uint64("user_id", targetID))
				res.CancelError = "取消进行中任务失败，请稍后在任务列表中重试"
			} else {
				res.Canceled = r.Canceled
				res.CancelFailed = len(r.Failed)
				res.FailedTaskIDs = append(res.FailedTaskIDs, r.Failed...)
			}
		}
	}

	// 6. 审计：封禁记 cancel_active 与取消数量；启用只记前后状态
	detail := map[string]any{"from": target.Status, "to": status}
	action := model.AdminAuditUserUnban
	if ban {
		action = model.AdminAuditUserBan
		detail["cancel_active"] = cancelActive
		detail["canceled"] = res.Canceled
		detail["cancel_failed"] = res.CancelFailed
	}
	adminAudit(ctx, s.audit, actorID, action, model.AdminAuditTargetUser, targetID, detail)
	return res, nil
}

// invalidate 清用户状态缓存；没有注入 UserInvalidator 时什么都不做。
func (s *AdminUserService) invalidate(ctx context.Context, id uint64) {
	if s.ctl.Users != nil {
		s.ctl.Users.InvalidateUser(ctx, id)
	}
}

// dedupeIDs 去重并保持首次出现的顺序；去重后为空或超过 200 个返回参数错误。
func dedupeIDs(ids []uint64) ([]uint64, error) {
	seen := make(map[uint64]struct{}, len(ids))
	out := make([]uint64, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil, errcode.ErrInvalidParams.WithMsg("用户列表不能为空")
	}
	if len(out) > maxBatchIDs {
		return nil, errcode.ErrInvalidParams.WithMsg("一次最多操作 200 个用户")
	}
	return out, nil
}

// batchItem 把单个用户的操作结果转成批量响应项：业务错误的文案可以直接展示；
// 其他（内部）错误不透传细节，只记日志，避免把数据库错误暴露给前端。
func batchItem(id uint64, err error) model.BatchItem {
	if err == nil {
		return model.BatchItem{ID: id, OK: true}
	}
	var ec *errcode.Error
	if errors.As(err, &ec) {
		return model.BatchItem{ID: id, Error: ec.Msg}
	}
	logger.Error("批量操作单个用户失败", zap.Error(err), zap.Uint64("user_id", id))
	return model.BatchItem{ID: id, Error: msgItemInternal}
}

// BatchAddCredits 给多个用户各增加 amount 积分（只增不减）。每个用户独立判断权限、独立事务，
// 不存在 / 无权限 / 执行失败只写进对应项，不整体失败；整体参数不合法（空列表、超过 200 个、amount<=0、备注不合法）才返回错误。
func (s *AdminUserService) BatchAddCredits(ctx context.Context, actorID uint64, ids []uint64, amount int, note string) (*model.BatchResult, error) {
	// 1. 整体参数校验与去重
	ids, err := dedupeIDs(ids)
	if err != nil {
		return nil, err
	}
	if amount <= 0 {
		return nil, errcode.ErrCreditAdjustInvalid.WithMsg("增加的积分必须大于 0")
	}
	if _, err := validCreditNote(note); err != nil {
		return nil, err
	}
	// 2. 逐个用户调用单用户流程：权限、事务、审计与单个调整完全一致
	res := &model.BatchResult{Results: make([]model.BatchItem, 0, len(ids))}
	for _, id := range ids {
		_, err := s.AdjustCredits(ctx, actorID, id, &model.AdjustCreditsReq{Mode: model.CreditModeAdd, Amount: &amount, Note: note})
		res.Results = append(res.Results, batchItem(id, err))
	}
	return res, nil
}

// BatchSetStatus 批量封禁 / 启用。不支持取消任务（要取消请逐个处理）；每个用户独立判断权限，失败只写进对应项。
func (s *AdminUserService) BatchSetStatus(ctx context.Context, actorID uint64, ids []uint64, status string) (*model.BatchResult, error) {
	// 1. 整体参数校验与去重
	ids, err := dedupeIDs(ids)
	if err != nil {
		return nil, err
	}
	if status != model.UserStatusActive && status != model.UserStatusDisabled {
		return nil, errcode.ErrInvalidParams.WithMsg("状态只能是 active 或 disabled")
	}
	// 2. 逐个用户走单用户流程（cancelActive 固定为 false）
	res := &model.BatchResult{Results: make([]model.BatchItem, 0, len(ids))}
	for _, id := range ids {
		_, err := s.SetStatus(ctx, actorID, id, status, false)
		res.Results = append(res.Results, batchItem(id, err))
	}
	return res, nil
}
