package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// marketingBarLimit 是列表卡状态条最多取每个项目最近几份脚本。
const marketingBarLimit = 8

// MarketingTx 是营销工作室业务事务里用到的数据库原语：项目、轮次、环节运行、脚本、出片任务和积分。
// 所有读写都带 user_id，别人的资源和不存在一样返回 ErrNotFound。
type MarketingTx interface {
	// InsertProject 插入项目。
	InsertProject(ctx context.Context, p *model.MarketingProject) error
	// Project 按用户读取存活项目，lock 为真时加行锁（串行化同一项目的写请求）。
	Project(ctx context.Context, userID, id uint64, lock bool) (*model.MarketingProject, error)
	// ProjectByID 按 id 读取存活项目，不限用户（任务回调、环节运行用），不存在返回 ErrNotFound。
	ProjectByID(ctx context.Context, id uint64) (*model.MarketingProject, error)
	// SaveProject 按 revision 乐观锁更新项目的可变列并让 revision+1，冲突返回 ErrRevisionConflict。
	SaveProject(ctx context.Context, userID, id uint64, revision int64, fields map[string]any) error
	// UpdateProject 更新项目的列，不动 revision（模特图、封面、事实卡的写回）。
	UpdateProject(ctx context.Context, id uint64, fields map[string]any) error
	// DeleteProject 软删项目。
	DeleteProject(ctx context.Context, userID, id uint64) error
	// InsertRound 插入轮次，轮次号取项目内最大号加一（调用方须已锁项目），并回填 No 和 ID。
	InsertRound(ctx context.Context, r *model.MarketingRound) error
	// Round 按用户读取轮次，lock 为真时加行锁。
	Round(ctx context.Context, userID, id uint64, lock bool) (*model.MarketingRound, error)
	// RoundByID 按 id 读取轮次，不限用户（队列 job 入口用）。
	RoundByID(ctx context.Context, id uint64, lock bool) (*model.MarketingRound, error)
	// ProjectRounds 返回项目的全部轮次，新的在前。
	ProjectRounds(ctx context.Context, userID, projectID uint64) ([]model.MarketingRound, error)
	// UpdateRound 更新轮次的列。
	UpdateRound(ctx context.Context, id uint64, fields map[string]any) error
	// ScriptingRounds 统计项目里写脚本中的轮次数。
	ScriptingRounds(ctx context.Context, projectID uint64) (int64, error)
	// InsertRun 插入环节运行。
	InsertRun(ctx context.Context, run *model.MarketingStepRun) error
	// Run 按用户读取环节运行，lock 为真时加行锁。
	Run(ctx context.Context, userID, id uint64, lock bool) (*model.MarketingStepRun, error)
	// RoundRuns 返回轮次的全部环节运行，按创建顺序。
	RoundRuns(ctx context.Context, roundIDs ...uint64) ([]model.MarketingStepRun, error)
	// UpdateRun 更新环节运行的列。
	UpdateRun(ctx context.Context, id uint64, fields map[string]any) error
	// UpdateRunIf 在运行状态属于 from 时更新，没命中返回 ErrStateConflict。
	UpdateRunIf(ctx context.Context, id uint64, from []string, fields map[string]any) error
	// InsertScripts 批量插入脚本。
	InsertScripts(ctx context.Context, rows []model.MarketingScript) error
	// Script 按用户读取脚本，lock 为真时加行锁。
	Script(ctx context.Context, userID, id uint64, lock bool) (*model.MarketingScript, error)
	// ScriptByID 按 id 读取脚本，不限用户（任务回调用）。
	ScriptByID(ctx context.Context, id uint64) (*model.MarketingScript, error)
	// RoundScripts 返回轮次的脚本，按 seq。
	RoundScripts(ctx context.Context, roundIDs ...uint64) ([]model.MarketingScript, error)
	// SaveScript 按 revision 乐观锁更新脚本的可变列并让 revision+1，冲突返回 ErrRevisionConflict。
	SaveScript(ctx context.Context, userID, id uint64, revision int64, fields map[string]any) error
	// UpdateScript 更新脚本的列，不动 revision（状态、任务列表的对账写回）。
	UpdateScript(ctx context.Context, id uint64, fields map[string]any) error
	// ScriptTasks 返回这些节点的正式生成任务，按创建时间、id 正序。
	ScriptTasks(ctx context.Context, userID uint64, nodeIDs []string) ([]model.GenerationTask, error)
	// EnqueueRound 在当前业务事务里为轮次入队 River job。
	EnqueueRound(ctx context.Context, roundID uint64) error
	// EnsureCredit 惰性建立积分账户。
	EnsureCredit(ctx context.Context, userID uint64, initial int) error
	// LockCredit 锁定积分账户。
	LockCredit(ctx context.Context, userID uint64) (*model.UserCredit, error)
	// AddCredit 增减余额和冻结额。
	AddCredit(ctx context.Context, userID uint64, balance, frozen int) error
	// InsertLedger 幂等插入积分流水。
	InsertLedger(ctx context.Context, e *model.CreditLedger) (bool, error)
}

// MarketingEnqueuer 把写脚本 job 放进当前业务事务（真实实现是 provider/queue 的 Runtime）。
type MarketingEnqueuer interface {
	// EnqueueMarketingRoundTx 在 tx 所在事务里为轮次入队。
	EnqueueMarketingRoundTx(ctx context.Context, tx *sql.Tx, roundID uint64) error
}

// MarketingRepository 是营销工作室的数据访问层，只做 SQL，不含业务规则。
type MarketingRepository struct {
	db  *gorm.DB
	enq MarketingEnqueuer
}

var _ MarketingTx = (*MarketingRepository)(nil)

// NewMarketingRepository 创建营销仓储。
func NewMarketingRepository(db *gorm.DB) *MarketingRepository { return &MarketingRepository{db: db} }

// SetEnqueuer 在依赖装配时绑定事务队列（队列运行时晚于仓储创建）。
func (r *MarketingRepository) SetEnqueuer(e MarketingEnqueuer) { r.enq = e }

// WithTx 只通过此入口开启营销事务；事务内只用 tx 参数。
func (r *MarketingRepository) WithTx(ctx context.Context, fn func(MarketingTx) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return fn(&MarketingRepository{db: tx, enq: r.enq}) })
}

// MarketingTx 把生成任务事务适配成营销原语：出片任务入库时要在同一个事务里锁脚本行。
func (r *GenerationTaskRepository) MarketingTx() MarketingTx { return &MarketingRepository{db: r.db} }

// forUpdate 在需要时给查询加行锁。
func forUpdate(q *gorm.DB, lock bool) *gorm.DB {
	if lock {
		return q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	return q
}

// InsertProject 见 MarketingTx。
func (r *MarketingRepository) InsertProject(ctx context.Context, p *model.MarketingProject) error {
	return r.db.WithContext(ctx).Create(p).Error
}

// Project 见 MarketingTx。
func (r *MarketingRepository) Project(ctx context.Context, userID, id uint64, lock bool) (*model.MarketingProject, error) {
	var p model.MarketingProject
	if err := forUpdate(r.db.WithContext(ctx), lock).Where("id = ? AND user_id = ?", id, userID).Take(&p).Error; err != nil {
		return nil, translate(err)
	}
	return &p, nil
}

// ProjectByID 见 MarketingTx。
func (r *MarketingRepository) ProjectByID(ctx context.Context, id uint64) (*model.MarketingProject, error) {
	var p model.MarketingProject
	if err := r.db.WithContext(ctx).Where("id = ?", id).Take(&p).Error; err != nil {
		return nil, translate(err)
	}
	return &p, nil
}

// ListProjects 按更新时间倒序分页返回用户的项目；keyword 非空时按标题模糊匹配。
func (r *MarketingRepository) ListProjects(ctx context.Context, userID uint64, keyword string, page, size int) ([]model.MarketingProject, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.MarketingProject{}).Where("user_id = ?", userID)
	if keyword != "" {
		q = q.Where("title ILIKE ?", "%"+escapeLike(keyword)+"%")
	}
	var n int64
	if err := q.Count(&n).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.MarketingProject
	err := q.Order("updated_at DESC, id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error
	return rows, n, err
}

// SaveProject 见 MarketingTx。
func (r *MarketingRepository) SaveProject(ctx context.Context, userID, id uint64, revision int64, fields map[string]any) error {
	fields["revision"] = gorm.Expr("revision + 1")
	fields["updated_at"] = time.Now()
	res := r.db.WithContext(ctx).Model(&model.MarketingProject{}).
		Where("id = ? AND user_id = ? AND revision = ?", id, userID, revision).Updates(jsonFields(fields))
	return casResult(ctx, res, func() (bool, error) {
		return r.exists(ctx, &model.MarketingProject{}, "id = ? AND user_id = ?", id, userID)
	})
}

// UpdateProject 见 MarketingTx。
func (r *MarketingRepository) UpdateProject(ctx context.Context, id uint64, fields map[string]any) error {
	fields["updated_at"] = time.Now()
	res := r.db.WithContext(ctx).Model(&model.MarketingProject{}).Where("id = ?", id).Updates(jsonFields(fields))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteProject 见 MarketingTx。
func (r *MarketingRepository) DeleteProject(ctx context.Context, userID, id uint64) error {
	res := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&model.MarketingProject{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// exists 判断满足条件的行是否存在（乐观锁没命中时区分「不存在」和「版本冲突」）。
func (r *MarketingRepository) exists(ctx context.Context, m any, query string, args ...any) (bool, error) {
	var n int64
	if err := r.db.WithContext(ctx).Model(m).Where(query, args...).Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// casResult 解释乐观锁更新的结果：命中返回 nil；没命中时行存在是版本冲突，不存在是 ErrNotFound。
func casResult(_ context.Context, res *gorm.DB, exists func() (bool, error)) error {
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		return nil
	}
	ok, err := exists()
	if err != nil {
		return err
	}
	if ok {
		return ErrRevisionConflict
	}
	return ErrNotFound
}

// InsertRound 见 MarketingTx。
func (r *MarketingRepository) InsertRound(ctx context.Context, row *model.MarketingRound) error {
	var next int
	if err := r.db.WithContext(ctx).Model(&model.MarketingRound{}).Where("project_id = ?", row.ProjectID).
		Select("COALESCE(MAX(no), 0) + 1").Scan(&next).Error; err != nil {
		return err
	}
	row.No = next
	return r.db.WithContext(ctx).Create(row).Error
}

// Round 见 MarketingTx。
func (r *MarketingRepository) Round(ctx context.Context, userID, id uint64, lock bool) (*model.MarketingRound, error) {
	var row model.MarketingRound
	if err := forUpdate(r.db.WithContext(ctx), lock).Where("id = ? AND user_id = ?", id, userID).Take(&row).Error; err != nil {
		return nil, translate(err)
	}
	return &row, nil
}

// RoundByID 见 MarketingTx。
func (r *MarketingRepository) RoundByID(ctx context.Context, id uint64, lock bool) (*model.MarketingRound, error) {
	var row model.MarketingRound
	if err := forUpdate(r.db.WithContext(ctx), lock).Where("id = ?", id).Take(&row).Error; err != nil {
		return nil, translate(err)
	}
	return &row, nil
}

// ProjectRounds 见 MarketingTx。
func (r *MarketingRepository) ProjectRounds(ctx context.Context, userID, projectID uint64) ([]model.MarketingRound, error) {
	var rows []model.MarketingRound
	err := r.db.WithContext(ctx).Where("user_id = ? AND project_id = ?", userID, projectID).Order("no DESC").Find(&rows).Error
	return rows, err
}

// UpdateRound 见 MarketingTx。
func (r *MarketingRepository) UpdateRound(ctx context.Context, id uint64, fields map[string]any) error {
	fields["updated_at"] = time.Now()
	res := r.db.WithContext(ctx).Model(&model.MarketingRound{}).Where("id = ?", id).Updates(jsonFields(fields))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// ScriptingRounds 见 MarketingTx。
func (r *MarketingRepository) ScriptingRounds(ctx context.Context, projectID uint64) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.MarketingRound{}).
		Where("project_id = ? AND status = ?", projectID, model.MarketingRoundScripting).Count(&n).Error
	return n, err
}

// InsertRun 见 MarketingTx。
func (r *MarketingRepository) InsertRun(ctx context.Context, run *model.MarketingStepRun) error {
	return r.db.WithContext(ctx).Create(run).Error
}

// Run 见 MarketingTx。
func (r *MarketingRepository) Run(ctx context.Context, userID, id uint64, lock bool) (*model.MarketingStepRun, error) {
	var run model.MarketingStepRun
	if err := forUpdate(r.db.WithContext(ctx), lock).Where("id = ? AND user_id = ?", id, userID).Take(&run).Error; err != nil {
		return nil, translate(err)
	}
	return &run, nil
}

// RoundRuns 见 MarketingTx。
func (r *MarketingRepository) RoundRuns(ctx context.Context, roundIDs ...uint64) ([]model.MarketingStepRun, error) {
	if len(roundIDs) == 0 {
		return nil, nil
	}
	var rows []model.MarketingStepRun
	err := r.db.WithContext(ctx).Where("round_id IN ?", roundIDs).Order("created_at ASC, id ASC").Find(&rows).Error
	return rows, err
}

// UpdateRun 见 MarketingTx。
func (r *MarketingRepository) UpdateRun(ctx context.Context, id uint64, fields map[string]any) error {
	res := r.db.WithContext(ctx).Model(&model.MarketingStepRun{}).Where("id = ?", id).Updates(jsonFields(fields))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateRunIf 见 MarketingTx。
func (r *MarketingRepository) UpdateRunIf(ctx context.Context, id uint64, from []string, fields map[string]any) error {
	res := r.db.WithContext(ctx).Model(&model.MarketingStepRun{}).Where("id = ? AND status IN ?", id, from).Updates(jsonFields(fields))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrStateConflict
	}
	return nil
}

// FailRate7d 返回环节近 7 天的失败率：失败数 / 已结束（成功或失败）数；没有记录返回 0。
// 跳过、取消、沿用不算运行，避免上游失败把下游的失败率拉高。
func (r *MarketingRepository) FailRate7d(ctx context.Context, stepKey string, since time.Time) (float64, error) {
	var row struct{ Failed, Total int64 }
	err := r.db.WithContext(ctx).Model(&model.MarketingStepRun{}).
		Select("COUNT(*) FILTER (WHERE status = ?) AS failed, COUNT(*) AS total", model.MarketingRunFailed).
		Where("step_key = ? AND created_at >= ? AND status IN ?", stepKey, since,
			[]string{model.MarketingRunSucceeded, model.MarketingRunFailed}).Scan(&row).Error
	if err != nil || row.Total == 0 {
		return 0, err
	}
	return float64(row.Failed) / float64(row.Total), nil
}

// InsertScripts 见 MarketingTx。
func (r *MarketingRepository) InsertScripts(ctx context.Context, rows []model.MarketingScript) error {
	if len(rows) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Create(&rows).Error
}

// Script 见 MarketingTx。
func (r *MarketingRepository) Script(ctx context.Context, userID, id uint64, lock bool) (*model.MarketingScript, error) {
	var s model.MarketingScript
	if err := forUpdate(r.db.WithContext(ctx), lock).Where("id = ? AND user_id = ?", id, userID).Take(&s).Error; err != nil {
		return nil, translate(err)
	}
	return &s, nil
}

// ScriptByID 见 MarketingTx。
func (r *MarketingRepository) ScriptByID(ctx context.Context, id uint64) (*model.MarketingScript, error) {
	var s model.MarketingScript
	if err := r.db.WithContext(ctx).Where("id = ?", id).Take(&s).Error; err != nil {
		return nil, translate(err)
	}
	return &s, nil
}

// RoundScripts 见 MarketingTx。
func (r *MarketingRepository) RoundScripts(ctx context.Context, roundIDs ...uint64) ([]model.MarketingScript, error) {
	if len(roundIDs) == 0 {
		return nil, nil
	}
	var rows []model.MarketingScript
	err := r.db.WithContext(ctx).Where("round_id IN ?", roundIDs).Order("seq ASC, id ASC").Find(&rows).Error
	return rows, err
}

// SaveScript 见 MarketingTx。
func (r *MarketingRepository) SaveScript(ctx context.Context, userID, id uint64, revision int64, fields map[string]any) error {
	fields["revision"] = gorm.Expr("revision + 1")
	fields["updated_at"] = time.Now()
	res := r.db.WithContext(ctx).Model(&model.MarketingScript{}).
		Where("id = ? AND user_id = ? AND revision = ?", id, userID, revision).Updates(jsonFields(fields))
	return casResult(ctx, res, func() (bool, error) {
		return r.exists(ctx, &model.MarketingScript{}, "id = ? AND user_id = ?", id, userID)
	})
}

// UpdateScript 见 MarketingTx。
func (r *MarketingRepository) UpdateScript(ctx context.Context, id uint64, fields map[string]any) error {
	fields["updated_at"] = time.Now()
	res := r.db.WithContext(ctx).Model(&model.MarketingScript{}).Where("id = ?", id).Updates(jsonFields(fields))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// ScriptTasks 见 MarketingTx：只取本人的正式任务（排除试跑），没有画布归属（营销出片不挂画布）。
func (r *MarketingRepository) ScriptTasks(ctx context.Context, userID uint64, nodeIDs []string) ([]model.GenerationTask, error) {
	if len(nodeIDs) == 0 {
		return nil, nil
	}
	var rows []model.GenerationTask
	err := r.db.WithContext(ctx).Where("user_id = ? AND node_id IN ? AND is_test = false", userID, nodeIDs).
		Order("created_at ASC, id ASC").Find(&rows).Error
	return rows, err
}

// EnqueueRound 见 MarketingTx。必须在业务事务里调用：入队用同一个 sql.Tx，才能和轮次行一起提交或回滚。
func (r *MarketingRepository) EnqueueRound(ctx context.Context, roundID uint64) error {
	if r.enq == nil {
		return errors.New("营销写脚本队列未配置")
	}
	tx, ok := r.db.Statement.ConnPool.(*sql.Tx)
	if !ok {
		return errors.New("营销写脚本入队必须处于业务事务")
	}
	return r.enq.EnqueueMarketingRoundTx(ctx, tx, roundID)
}

// EnsureCredit 复用已有账户初始化及流水逻辑。
func (r *MarketingRepository) EnsureCredit(ctx context.Context, userID uint64, initial int) error {
	return insertInitialCredit(ctx, r.db, userID, initial)
}

// LockCredit 读取并加行锁保护账户。
func (r *MarketingRepository) LockCredit(ctx context.Context, userID uint64) (*model.UserCredit, error) {
	return (&GenerationTaskRepository{db: r.db}).LockCredit(ctx, userID)
}

// AddCredit 通过已有积分原语原子更新。
func (r *MarketingRepository) AddCredit(ctx context.Context, userID uint64, balance, frozen int) error {
	return (&GenerationTaskRepository{db: r.db}).AddCredit(ctx, userID, balance, frozen)
}

// InsertLedger 复用幂等积分流水写入。
func (r *MarketingRepository) InsertLedger(ctx context.Context, e *model.CreditLedger) (bool, error) {
	return (&GenerationTaskRepository{db: r.db}).InsertLedger(ctx, e)
}

// MarketingStatusCount 是一个项目某状态的脚本数。
type MarketingStatusCount struct {
	ProjectID uint64
	Status    string
	N         int
}

// MarketingBar 是一份脚本在列表卡状态条里的一格。
type MarketingBar struct {
	ProjectID uint64
	Status    string
}

// MarketingRoundBrief 是列表需要的轮次概况。
type MarketingRoundBrief struct {
	ProjectID uint64
	Status    string
	PlanCount int
}

// ScriptCounts 统计每个项目各状态的脚本数。
func (r *MarketingRepository) ScriptCounts(ctx context.Context, projectIDs []uint64) ([]MarketingStatusCount, error) {
	if len(projectIDs) == 0 {
		return nil, nil
	}
	var rows []MarketingStatusCount
	err := r.db.WithContext(ctx).Model(&model.MarketingScript{}).
		Select("project_id, status, COUNT(*) AS n").Where("project_id IN ?", projectIDs).
		Group("project_id, status").Scan(&rows).Error
	return rows, err
}

// ScriptBars 返回每个项目最近 8 份脚本的状态，新的在前。
func (r *MarketingRepository) ScriptBars(ctx context.Context, projectIDs []uint64) ([]MarketingBar, error) {
	if len(projectIDs) == 0 {
		return nil, nil
	}
	var rows []MarketingBar
	err := r.db.WithContext(ctx).Raw(`
SELECT project_id, status FROM (
  SELECT project_id, status, created_at, id,
         ROW_NUMBER() OVER (PARTITION BY project_id ORDER BY created_at DESC, id DESC) AS rn
  FROM marketing_scripts WHERE project_id IN ?) t
WHERE rn <= ? ORDER BY project_id, created_at DESC, id DESC`, projectIDs, marketingBarLimit).Scan(&rows).Error
	return rows, err
}

// RoundBriefs 返回每个项目的轮次概况：写脚本中的轮次及计划份数，加每个项目最新一轮的状态。
func (r *MarketingRepository) RoundBriefs(ctx context.Context, projectIDs []uint64) (scripting []MarketingRoundBrief, latest []MarketingRoundBrief, err error) {
	if len(projectIDs) == 0 {
		return nil, nil, nil
	}
	err = r.db.WithContext(ctx).Model(&model.MarketingRound{}).
		Select("project_id, status, plan_count").
		Where("project_id IN ? AND status = ?", projectIDs, model.MarketingRoundScripting).Scan(&scripting).Error
	if err != nil {
		return nil, nil, err
	}
	err = r.db.WithContext(ctx).Raw(`
SELECT DISTINCT ON (project_id) project_id, status, plan_count FROM marketing_rounds
WHERE project_id IN ? ORDER BY project_id, no DESC`, projectIDs).Scan(&latest).Error
	return scripting, latest, err
}

// ActiveProjectTasks 统计项目下仍在进行的出片任务数：按脚本节点查，不含试跑。
func (r *MarketingRepository) ActiveProjectTasks(ctx context.Context, userID uint64, nodeIDs []string) (int64, error) {
	if len(nodeIDs) == 0 {
		return 0, nil
	}
	var n int64
	err := r.db.WithContext(ctx).Model(&model.GenerationTask{}).
		Where("user_id = ? AND node_id IN ? AND is_test = false AND status IN ?", userID, nodeIDs, model.ActiveTaskStatuses).Count(&n).Error
	return n, err
}

// ProjectScriptIDs 返回项目全部脚本的 id（删除项目前检查进行中的任务用）。
func (r *MarketingRepository) ProjectScriptIDs(ctx context.Context, userID, projectID uint64) ([]uint64, error) {
	var ids []uint64
	err := r.db.WithContext(ctx).Model(&model.MarketingScript{}).
		Where("user_id = ? AND project_id = ?", userID, projectID).Pluck("id", &ids).Error
	return ids, err
}

// CoverKeys 按素材 id 批量取存储 key（列表封面用），不存在的素材不在结果里；只取本人的素材。
func (r *MarketingRepository) CoverKeys(ctx context.Context, userID uint64, assetIDs []uint64) (map[uint64]string, error) {
	out := make(map[uint64]string, len(assetIDs))
	if len(assetIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		ID  uint64
		Key string
	}
	err := r.db.WithContext(ctx).Model(&model.Asset{}).Select("id, storage_key AS key").
		Where("user_id = ? AND id IN ?", userID, assetIDs).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.ID] = row.Key
	}
	return out, nil
}

// jsonFields 把更新列里的 []byte 值包成 datatypes.JSON：jsonb 列要按 JSON 写入，不能当 bytea。
func jsonFields(fields map[string]any) map[string]any {
	for k, v := range fields {
		if b, ok := v.([]byte); ok {
			fields[k] = datatypes.JSON(b)
		}
	}
	return fields
}

// Add 返回「列 + n」的更新表达式，给更新列用（计数类字段原子累加，不读后写）。col 只能是代码里的常量列名。
func Add(col string, n int) any { return gorm.Expr(col+" + ?", n) }
