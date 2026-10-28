package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// DramaTx 短剧业务事务使用的数据库原语。
type DramaTx interface {
	// Project 按用户和ID读取项目；lock用于串行化写请求。
	Project(context.Context, uint64, uint64, bool) (*model.DramaProject, error)
	// InsertProject 插入项目并建立专属画布。
	InsertProject(context.Context, *model.DramaProject) error
	// UpdateProject 带revision更新项目，冲突返回ErrStateConflict。
	UpdateProject(context.Context, uint64, uint64, int64, map[string]any) error
	// DeleteProject 软删项目、专属画布与角色卡。
	DeleteProject(context.Context, uint64, uint64) error
	// Episodes 读取用户项目的集列表。
	Episodes(context.Context, uint64, uint64) ([]model.DramaEpisode, error)
	// SaveEpisode 插入新集或按revision更新已有集。
	SaveEpisode(context.Context, *model.DramaEpisode, int64) error
	// Segments 读取用户存活项目下某集片段，按seq和ID排序。
	Segments(context.Context, uint64, uint64) ([]model.DramaSegment, error)
	// ReplaceSegments 替换指定集全部片段，不删除任务和素材。
	ReplaceSegments(context.Context, uint64, uint64, []model.DramaSegment) error
	// HasSegmentTakes 判断该集现存片段是否已有任何视频任务。
	HasSegmentTakes(context.Context, uint64, uint64) (bool, error)
	// SegmentEpisode 读取片段所属集，同时校验项目存活。
	SegmentEpisode(context.Context, uint64, uint64) (*model.DramaEpisode, error)
	// Segment 读取用户片段，可在项目锁之后锁定片段。
	Segment(context.Context, uint64, uint64, bool) (*model.DramaSegment, error)
	// SaveSegment 插入片段或按revision更新，冲突返回ErrStateConflict。
	SaveSegment(context.Context, *model.DramaSegment, int64) error
	// DeleteSegment 只软删用户片段，缺失返回ErrNotFound。
	DeleteSegment(context.Context, uint64, uint64) error
	// SegmentTasks 读取同用户、画布和节点的正式任务，按创建时间和ID排序。
	SegmentTasks(context.Context, uint64, uint64, string) ([]model.GenerationTask, error)
	// Jobs 读取项目任务，可仅返回活动任务。
	Jobs(context.Context, uint64, uint64, bool) ([]model.DramaJob, error)
	// Job 读取用户所属项目的任务。
	Job(context.Context, uint64, uint64, uint64, bool) (*model.DramaJob, error)
	// InsertJob 插入任务并在同一事务入队。
	InsertJob(context.Context, *model.DramaJob) error
	// UpdateJob 更新任务可变列。
	UpdateJob(context.Context, uint64, uint64, map[string]any) error
	// EnsureCredit 惰性建立账户。
	EnsureCredit(context.Context, uint64, int) error
	// LockCredit 锁定账户，避免并发超扣。
	LockCredit(context.Context, uint64) (*model.UserCredit, error)
	// AddCredit 增减余额和冻结额。
	AddCredit(context.Context, uint64, int, int) error
	// InsertLedger 幂等插入积分流水。
	InsertLedger(context.Context, *model.CreditLedger) (bool, error)
}

// DramaEnqueuer 把短剧步骤放进当前业务事务。
type DramaEnqueuer interface {
	EnqueueDramaTx(context.Context, *sql.Tx, uint64, uint64, uint64) error
}

// DramaRepository 短剧数据访问，不执行模型网络请求。
type DramaRepository struct {
	db  *gorm.DB
	enq DramaEnqueuer
}

// NewDramaRepository 创建短剧仓储。
func NewDramaRepository(db *gorm.DB) *DramaRepository { return &DramaRepository{db: db} }

// SetEnqueuer 在依赖装配时绑定事务队列。
func (r *DramaRepository) SetEnqueuer(e DramaEnqueuer) { r.enq = e }

// WithTx 只通过此入口创建短剧事务。
func (r *DramaRepository) WithTx(ctx context.Context, fn func(DramaTx) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return fn(&DramaRepository{db: tx, enq: r.enq}) })
}

// List 按用户、标题分页读取，更新时间倒序。
func (r *DramaRepository) List(ctx context.Context, userID uint64, keyword string, page, size int) ([]model.DramaProject, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.DramaProject{}).Where("user_id = ?", userID)
	if keyword != "" {
		q = q.Where("title ILIKE ?", "%"+escapeLike(keyword)+"%")
	}
	var n int64
	if err := q.Count(&n).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.DramaProject
	err := q.Order("updated_at DESC, id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error
	return rows, n, err
}

// Project 按用户和ID读取，其他用户的数据返回ErrNotFound。
func (r *DramaRepository) Project(ctx context.Context, userID, id uint64, lock bool) (*model.DramaProject, error) {
	q := r.db.WithContext(ctx)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var p model.DramaProject
	err := q.Where("user_id = ? AND id = ?", userID, id).First(&p).Error
	return &p, translate(err)
}

// InsertProject 在当前事务插入项目及画布。
func (r *DramaRepository) InsertProject(ctx context.Context, p *model.DramaProject) error {
	canvas := model.CanvasProject{UserID: p.UserID, Title: p.Title, PayloadJSON: []byte(`{"nodes":[],"connections":[]}`), Revision: 1}
	if err := r.db.WithContext(ctx).Create(&canvas).Error; err != nil {
		return err
	}
	p.CanvasID = canvas.ID
	if err := r.db.WithContext(ctx).Create(p).Error; err != nil {
		return err
	}
	return r.db.WithContext(ctx).Model(&canvas).Update("drama_id", p.ID).Error
}

// UpdateProject 按用户及revision执行CAS，未命中返回ErrStateConflict。
func (r *DramaRepository) UpdateProject(ctx context.Context, userID, id uint64, revision int64, fields map[string]any) error {
	fields["revision"] = gorm.Expr("revision + 1")
	fields["updated_at"] = time.Now()
	res := r.db.WithContext(ctx).Model(&model.DramaProject{}).Where("user_id = ? AND id = ? AND revision = ?", userID, id, revision).Updates(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrStateConflict
	}
	return nil
}

// DeleteProject 软删同一用户的项目、画布与短剧角色。
func (r *DramaRepository) DeleteProject(ctx context.Context, userID, id uint64) error {
	for _, m := range []any{&model.CanvasProject{}, &model.CharacterCard{}} {
		if err := r.db.WithContext(ctx).Where("user_id = ? AND drama_id = ?", userID, id).Delete(m).Error; err != nil {
			return err
		}
	}
	if err := r.db.WithContext(ctx).Where("user_id = ? AND drama_id = ?", userID, id).Delete(&model.DramaEpisode{}).Error; err != nil {
		return err
	}
	return r.db.WithContext(ctx).Where("user_id = ? AND id = ?", userID, id).Delete(&model.DramaProject{}).Error
}

// Episodes 读取同用户同项目的集，集号升序。
func (r *DramaRepository) Episodes(ctx context.Context, userID, id uint64) ([]model.DramaEpisode, error) {
	var rows []model.DramaEpisode
	err := r.db.WithContext(ctx).Where("user_id = ? AND drama_id = ?", userID, id).Order("no ASC").Find(&rows).Error
	return rows, err
}

// SaveEpisode 新建集或按用户和revision更新，冲突返回ErrStateConflict。
func (r *DramaRepository) SaveEpisode(ctx context.Context, e *model.DramaEpisode, revision int64) error {
	if e.ID == 0 {
		return r.db.WithContext(ctx).Create(e).Error
	}
	res := r.db.WithContext(ctx).Model(&model.DramaEpisode{}).Where("user_id = ? AND drama_id = ? AND id = ? AND revision = ?", e.UserID, e.DramaID, e.ID, revision).Updates(map[string]any{"title": e.Title, "synopsis": e.Synopsis, "script_md": e.ScriptMD, "script_status": e.ScriptStatus, "storyboard_status": e.StoryboardStatus, "locked_at": e.LockedAt, "revision": gorm.Expr("revision + 1"), "updated_at": time.Now()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrStateConflict
	}
	e.Revision = revision + 1
	e.UpdatedAt = time.Now()
	return nil
}

// Jobs 读取用户项目任务，active限定queued/running。
func (r *DramaRepository) Jobs(ctx context.Context, userID, id uint64, active bool) ([]model.DramaJob, error) {
	q := r.db.WithContext(ctx).Where("user_id = ? AND drama_id = ?", userID, id)
	if active {
		q = q.Where("status IN ?", []string{"queued", "running"})
	}
	var rows []model.DramaJob
	err := q.Order("id DESC").Find(&rows).Error
	return rows, err
}

// Job 按用户项目读取任务；不存在返回ErrNotFound。
func (r *DramaRepository) Job(ctx context.Context, userID, dramaID, id uint64, lock bool) (*model.DramaJob, error) {
	q := r.db.WithContext(ctx)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var row model.DramaJob
	err := q.Where("user_id = ? AND drama_id = ? AND id = ?", userID, dramaID, id).First(&row).Error
	return &row, translate(err)
}

// InsertJob 插入任务并在当前sql.Tx入队；活动任务唯一冲突返回ErrDuplicate。
func (r *DramaRepository) InsertJob(ctx context.Context, j *model.DramaJob) error {
	if err := r.db.WithContext(ctx).Create(j).Error; err != nil {
		if isUniqueViolation(err) {
			return ErrDuplicate
		}
		return err
	}
	if r.enq == nil {
		return errors.New("短剧队列未配置")
	}
	tx, ok := r.db.Statement.ConnPool.(*sql.Tx)
	if !ok {
		return errors.New("短剧入队必须处于业务事务")
	}
	return r.enq.EnqueueDramaTx(ctx, tx, j.UserID, j.DramaID, j.ID)
}

// UpdateJob 按用户和任务ID更新，不存在返回ErrNotFound。
func (r *DramaRepository) UpdateJob(ctx context.Context, userID, id uint64, fields map[string]any) error {
	res := r.db.WithContext(ctx).Model(&model.DramaJob{}).Where("user_id = ? AND id = ?", userID, id).Updates(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// EnsureCredit 复用已有账户初始化及流水逻辑。
func (r *DramaRepository) EnsureCredit(ctx context.Context, userID uint64, initial int) error {
	return insertInitialCredit(ctx, r.db, userID, initial)
}

// LockCredit 读取并行锁保护账户。
func (r *DramaRepository) LockCredit(ctx context.Context, userID uint64) (*model.UserCredit, error) {
	return (&GenerationTaskRepository{db: r.db}).LockCredit(ctx, userID)
}

// AddCredit 通过已有积分原语原子更新。
func (r *DramaRepository) AddCredit(ctx context.Context, userID uint64, balance, frozen int) error {
	return (&GenerationTaskRepository{db: r.db}).AddCredit(ctx, userID, balance, frozen)
}

// InsertLedger 复用幂等积分流水写入。
func (r *DramaRepository) InsertLedger(ctx context.Context, e *model.CreditLedger) (bool, error) {
	return (&GenerationTaskRepository{db: r.db}).InsertLedger(ctx, e)
}
