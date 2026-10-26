package repository

import (
	"context"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"video-canvas/internal/model"
)

// AgentRepository 是画布 Agent 的数据访问：画布、改动日志（撤销的依据）、模型调用的计费记录。会话、运行、审批、事件在 Agent 服务里。
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

// GetRun 按 id + user_id 查运行，不存在或不属于该用户返回 ErrNotFound。
func (r *AgentRepository) GetRun(ctx context.Context, userID, id uint64) (*model.AgentRun, error) {
	var run model.AgentRun
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&run).Error; err != nil {
		return nil, translate(err)
	}
	return &run, nil
}

// InterruptActiveRuns 把 Go 库里还停在活跃状态（排队、运行中、等审批、等回答）的旧运行标为 interrupted，返回处理的条数。
// 运行已经全部由 Agent 服务管理，这些切换之前留下的旧运行不会再有人推进，不该一直显示成进行中。服务启动时调用，幂等。
func (r *AgentRepository) InterruptActiveRuns(ctx context.Context) (int64, error) {
	res := r.db.WithContext(ctx).Model(&model.AgentRun{}).
		Where("status IN ?", []string{model.RunQueued, model.RunRunning, model.RunWaitingApproval, model.RunWaitingInput}).
		Update("status", model.RunInterrupted)
	return res.RowsAffected, res.Error
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

// ListEventsBefore 列出会话里序号小于 beforeSeq 的最新 limit 条事件，按序号升序（beforeSeq<=0 不设上界，limit<=0 取默认值）。
// GetCanvas 按 id + user_id 读画布（含 payload_json），不存在或不属于该用户返回 ErrNotFound。
// Agent 的工具只能碰运行所属用户自己的画布。
func (r *AgentRepository) GetCanvas(ctx context.Context, userID, id uint64) (*model.CanvasProject, error) {
	var c model.CanvasProject
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&c).Error; err != nil {
		return nil, translate(err)
	}
	return &c, nil
}

// CommitInput 是一次画布写入的全部内容。
type CommitInput struct {
	UserID       uint64                  // 画布所有者
	CanvasID     uint64                  // 画布 id
	BaseRevision uint64                  // 写入所基于的 revision，乐观锁
	Payload      []byte                  // 新的 payload_json
	Mutation     *model.AgentMutation    // 要记录的改动（RunID、Kind、ChangesJSON 等由调用方填好）
	UndoRunID    uint64                  // 非 0 表示这是对该运行的撤销，同一事务里把它的改动标记为已撤销
	CreatedReads []model.AgentCanvasRead // 自建节点同时计为已读，和画布写入一起提交，避免崩溃后丢失
}

// CommitCanvasMutation 在一个事务里完成「按乐观锁更新画布」「记录改动」「记录自建节点已读」「标记被撤销的改动」，要么都成要么都不成：
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
		if err := insertCanvasReads(tx, in.CreatedReads); err != nil {
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

// UpdateApprovalIf 是审批状态迁移的 CAS：只有当前状态在 from 里才更新，返回更新后的审批。
