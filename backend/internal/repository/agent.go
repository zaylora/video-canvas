package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// ErrAgentStateConflict 状态迁移的 CAS 没有命中：记录不存在，或当前状态已不在期望的集合里（被别的流程抢先处理了）。
// 调用方应当把它当作「已被处理」，而不是失败。
var ErrAgentStateConflict = errors.New("agent state conflict")

// AgentRepository 是画布 Agent 的数据访问：会话、运行、事件、改动日志、审批。
type AgentRepository struct {
	db *gorm.DB
}

// NewAgentRepository 创建仓储。
func NewAgentRepository(db *gorm.DB) *AgentRepository { return &AgentRepository{db: db} }

// emptyJSON 把空 JSON 补成默认值：jsonb 列是 NOT NULL，nil 会写成 NULL 导致失败。
func emptyJSON(v datatypes.JSON, def string) datatypes.JSON {
	if len(v) == 0 {
		return datatypes.JSON(def)
	}
	return v
}

// CreateSession 创建会话。
func (r *AgentRepository) CreateSession(ctx context.Context, s *model.AgentSession) error {
	return r.db.WithContext(ctx).Create(s).Error
}

// GetSession 按 id + user_id 查会话（排除已软删除的），不存在或不属于该用户返回 ErrNotFound。
func (r *AgentRepository) GetSession(ctx context.Context, userID, id uint64) (*model.AgentSession, error) {
	var s model.AgentSession
	err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&s).Error
	if err != nil {
		return nil, translate(err)
	}
	return &s, nil
}

// ListSessions 列出某用户在某画布上的会话，最近更新的在前；不带 session_jsonl，那是大字段。
func (r *AgentRepository) ListSessions(ctx context.Context, userID, canvasID uint64) ([]model.AgentSession, error) {
	var out []model.AgentSession
	err := r.db.WithContext(ctx).Omit("session_jsonl").
		Where("user_id = ? AND canvas_id = ?", userID, canvasID).
		Order("updated_at DESC, id DESC").Find(&out).Error
	return out, err
}

// CountSessions 统计某用户在某画布上的会话数（排除已软删除的），用来限制每个画布的会话数量。
func (r *AgentRepository) CountSessions(ctx context.Context, userID, canvasID uint64) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.AgentSession{}).
		Where("user_id = ? AND canvas_id = ?", userID, canvasID).Count(&n).Error
	return n, err
}

// UpdateSession 按 id + user_id 更新会话的指定字段，没有命中返回 ErrNotFound。
func (r *AgentRepository) UpdateSession(ctx context.Context, userID, id uint64, fields map[string]any) error {
	res := r.db.WithContext(ctx).Model(&model.AgentSession{}).
		Where("id = ? AND user_id = ?", id, userID).Updates(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteSession 软删除会话，没有命中返回 ErrNotFound。
func (r *AgentRepository) DeleteSession(ctx context.Context, userID, id uint64) error {
	res := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&model.AgentSession{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// CreateRun 创建运行。同一画布上已有活跃运行时返回 ErrDuplicate（由部分唯一索引保证，不会因并发漏判）。
func (r *AgentRepository) CreateRun(ctx context.Context, run *model.AgentRun) error {
	if err := r.db.WithContext(ctx).Create(run).Error; err != nil {
		if isUniqueViolation(err) {
			return ErrDuplicate
		}
		return err
	}
	return nil
}

// GetRun 按 id + user_id 查运行，不存在或不属于该用户返回 ErrNotFound。
func (r *AgentRepository) GetRun(ctx context.Context, userID, id uint64) (*model.AgentRun, error) {
	var run model.AgentRun
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&run).Error; err != nil {
		return nil, translate(err)
	}
	return &run, nil
}

// ActiveRun 查某画布上占位的活跃运行，没有返回 ErrNotFound。
func (r *AgentRepository) ActiveRun(ctx context.Context, canvasID uint64) (*model.AgentRun, error) {
	var run model.AgentRun
	err := r.db.WithContext(ctx).Where("canvas_id = ? AND status IN ?", canvasID, model.ActiveRunStatuses).First(&run).Error
	if err != nil {
		return nil, translate(err)
	}
	return &run, nil
}

// UpdateRunIf 是运行状态迁移的 CAS：UPDATE … WHERE id=? AND status IN (from) … RETURNING *。
// 没命中（运行不存在，或状态已不在 from 里）返回 ErrAgentStateConflict。
// 迁移到终态（不再占用画布）时由调用方在 fields 里写 ended_at。
func (r *AgentRepository) UpdateRunIf(ctx context.Context, id uint64, from []string, fields map[string]any) (*model.AgentRun, error) {
	var run model.AgentRun
	res := r.db.WithContext(ctx).Model(&run).Clauses(clause.Returning{}).
		Where("id = ? AND status IN ?", id, from).Updates(fields)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrAgentStateConflict
	}
	return &run, nil
}

// AddRunUsage 原子地累加运行的步数和已花积分，运行不存在返回 ErrNotFound。
func (r *AgentRepository) AddRunUsage(ctx context.Context, id uint64, steps, credits int) error {
	res := r.db.WithContext(ctx).Model(&model.AgentRun{}).Where("id = ?", id).Updates(map[string]any{
		"steps":         gorm.Expr("steps + ?", steps),
		"spent_credits": gorm.Expr("spent_credits + ?", credits),
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// AppendEvent 给会话追加一条事件。序号在同一事务里用 UPDATE … RETURNING 原子地分配，
// 并发追加不会拿到相同的 seq；会话不存在返回 ErrNotFound。
func (r *AgentRepository) AppendEvent(ctx context.Context, sessionID, runID uint64, typ string, payload datatypes.JSON) (*model.AgentEvent, error) {
	var ev *model.AgentEvent
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var s model.AgentSession
		res := tx.Model(&s).Clauses(clause.Returning{Columns: []clause.Column{{Name: "last_seq"}}}).
			Where("id = ?", sessionID).UpdateColumn("last_seq", gorm.Expr("last_seq + 1"))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		ev = &model.AgentEvent{SessionID: sessionID, RunID: runID, Seq: s.LastSeq, Type: typ, PayloadJSON: emptyJSON(payload, "{}")}
		return tx.Create(ev).Error
	})
	if err != nil {
		return nil, err
	}
	return ev, nil
}

// ListEvents 列出会话里序号大于 afterSeq 的事件，按序号升序，最多 limit 条（<=0 取默认值）。
func (r *AgentRepository) ListEvents(ctx context.Context, sessionID uint64, afterSeq int64, limit int) ([]model.AgentEvent, error) {
	if limit <= 0 {
		limit = defaultListLimit
	}
	var out []model.AgentEvent
	err := r.db.WithContext(ctx).Where("session_id = ? AND seq > ?", sessionID, afterSeq).
		Order("seq ASC").Limit(limit).Find(&out).Error
	return out, err
}

// CommitInput 是一次画布写入的全部内容。
type CommitInput struct {
	UserID       uint64               // 画布所有者
	CanvasID     uint64               // 画布 id
	BaseRevision uint64               // 写入所基于的 revision，乐观锁
	Payload      []byte               // 新的 payload_json
	Mutation     *model.AgentMutation // 要记录的改动（RunID、Kind、ChangesJSON 等由调用方填好）
	UndoRunID    uint64               // 非 0 表示这是对该运行的撤销，同一事务里把它的改动标记为已撤销
}

// CommitCanvasMutation 在一个事务里完成「按乐观锁更新画布」「记录改动」「标记被撤销的改动」，三者要么都成要么都不成：
// 画布写进去了但日志没记，撤销就找不到依据；日志记了画布没写，撤销会改坏用户的画布。
// 画布不存在或不属于该用户返回 ErrNotFound，revision 与库里不一致返回 ErrRevisionConflict。
// 成功时 Mutation 的 seq、revision_before、revision_after 被回填。
func (r *AgentRepository) CommitCanvasMutation(ctx context.Context, in CommitInput) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&model.CanvasProject{}).
			Where("id = ? AND user_id = ? AND revision = ?", in.CanvasID, in.UserID, in.BaseRevision).
			Updates(map[string]any{
				"payload_json": datatypes.JSON(in.Payload),
				"revision":     gorm.Expr("revision + 1"),
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			var n int64
			if err := tx.Model(&model.CanvasProject{}).Where("id = ? AND user_id = ?", in.CanvasID, in.UserID).Count(&n).Error; err != nil {
				return err
			}
			if n == 0 {
				return ErrNotFound
			}
			return ErrRevisionConflict
		}
		m := in.Mutation
		var maxSeq int
		if err := tx.Model(&model.AgentMutation{}).Where("run_id = ?", m.RunID).
			Select("COALESCE(MAX(seq), 0)").Scan(&maxSeq).Error; err != nil {
			return err
		}
		m.Seq, m.CanvasID, m.UserID = maxSeq+1, in.CanvasID, in.UserID
		m.RevisionBefore, m.RevisionAfter = in.BaseRevision, in.BaseRevision+1
		m.ChangesJSON = emptyJSON(m.ChangesJSON, "[]")
		if err := tx.Create(m).Error; err != nil {
			return err
		}
		if in.UndoRunID == 0 {
			return nil
		}
		return tx.Model(&model.AgentMutation{}).
			Where("run_id = ? AND undone_at IS NULL AND kind <> ?", in.UndoRunID, model.MutationUndo).
			Update("undone_at", time.Now()).Error
	})
}

// ListMutations 列出某运行的全部改动，按序号升序。
func (r *AgentRepository) ListMutations(ctx context.Context, runID uint64) ([]model.AgentMutation, error) {
	var out []model.AgentMutation
	err := r.db.WithContext(ctx).Where("run_id = ?", runID).Order("seq ASC").Find(&out).Error
	return out, err
}

// CreateApproval 创建审批。
func (r *AgentRepository) CreateApproval(ctx context.Context, a *model.AgentApproval) error {
	a.PayloadJSON = emptyJSON(a.PayloadJSON, "{}")
	a.DecisionJSON = emptyJSON(a.DecisionJSON, "{}")
	a.ResultJSON = emptyJSON(a.ResultJSON, "{}")
	return r.db.WithContext(ctx).Create(a).Error
}

// GetApproval 按 id + user_id 查审批，不存在或不属于该用户返回 ErrNotFound。
func (r *AgentRepository) GetApproval(ctx context.Context, userID, id uint64) (*model.AgentApproval, error) {
	var a model.AgentApproval
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&a).Error; err != nil {
		return nil, translate(err)
	}
	return &a, nil
}

// ListApprovals 列出某运行的审批，按创建先后。
func (r *AgentRepository) ListApprovals(ctx context.Context, runID uint64) ([]model.AgentApproval, error) {
	var out []model.AgentApproval
	err := r.db.WithContext(ctx).Where("run_id = ?", runID).Order("id ASC").Find(&out).Error
	return out, err
}

// UpdateApprovalIf 是审批状态迁移的 CAS：只有当前状态在 from 里才更新，返回更新后的审批。
// 没命中返回 ErrAgentStateConflict，意味着已经被处理过（重复点击、已过期、本轮已停止）。
func (r *AgentRepository) UpdateApprovalIf(ctx context.Context, id uint64, from []string, fields map[string]any) (*model.AgentApproval, error) {
	var a model.AgentApproval
	res := r.db.WithContext(ctx).Model(&a).Clauses(clause.Returning{}).
		Where("id = ? AND status IN ?", id, from).Updates(fields)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrAgentStateConflict
	}
	return &a, nil
}

// ExpirePendingApprovals 把某运行所有待处理的审批置为已失效，返回受影响的条数；运行被停止或结束时调用。
func (r *AgentRepository) ExpirePendingApprovals(ctx context.Context, runID uint64) (int64, error) {
	res := r.db.WithContext(ctx).Model(&model.AgentApproval{}).
		Where("run_id = ? AND status = ?", runID, model.ApprovalPending).
		Update("status", model.ApprovalExpired)
	return res.RowsAffected, res.Error
}
