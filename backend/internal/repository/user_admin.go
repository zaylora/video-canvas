package repository

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// UserTx 是注册事务里要用到的 SQL 原语；UserRepository 自身实现它，WithTx 把它绑定到事务上传给回调。
type UserTx interface {
	// LockRegistration 取事务级咨询锁，串行化所有注册事务：
	// “users 表是否为空”的判断与随后的插入必须在锁内完成，否则并发注册会产生多个 super_admin。锁在事务结束时自动释放。
	LockRegistration(ctx context.Context) error
	// Count 统计用户数（不含软删除）。
	Count(ctx context.Context) (int64, error)
	// UsernameExists 用户名是否已存在。
	UsernameExists(ctx context.Context, username string) (bool, error)
	// EmailExists 邮箱是否已存在（按小写比较）。
	EmailExists(ctx context.Context, email string) (bool, error)
	// Create 新建用户；用户名或邮箱的唯一约束冲突返回 ErrDuplicate。
	Create(ctx context.Context, u *model.User) error
	// CreateCredit 建积分账户（余额 initial）并写一条 initial 流水；账户已存在时什么都不做。
	CreateCredit(ctx context.Context, userID uint64, initial int) error
	// InsertLoginLog 写一条登录记录。
	InsertLoginLog(ctx context.Context, l *model.UserLoginLog) error
	// LockRoleChange 取事务级咨询锁，串行化所有“调整角色”事务：
	// “统计 super_admin 数量”与随后的降级必须在锁内完成，否则两个超管同时互相降级会把最后一个超管也降没。
	LockRoleChange(ctx context.Context) error
	// CountByRole 统计某角色的用户数（不含软删除）。
	CountByRole(ctx context.Context, role string) (int64, error)
	// GetByID 按 id 查询用户（事务内读最新值），不存在返回 ErrNotFound。
	GetByID(ctx context.Context, id uint64) (*model.User, error)
	// Update 按 id 更新指定字段，用户不存在返回 ErrNotFound。
	Update(ctx context.Context, id uint64, fields map[string]any) error
}

var _ UserTx = (*UserRepository)(nil)

// registrationLockKey 是注册咨询锁的键（任意固定常量，只要不与别处冲突）。
const registrationLockKey int64 = 0x76635f726567 // "vc_reg"

// roleChangeLockKey 是调整角色咨询锁的键（与注册锁不同，互不阻塞）。
const roleChangeLockKey int64 = 0x76635f726f6c // "vc_rol"

// WithTx 在一个事务里执行 fn：fn 返回错误整体回滚，否则提交。回调里只能用传入的 tx。
func (r *UserRepository) WithTx(ctx context.Context, fn func(tx UserTx) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(&UserRepository{db: tx})
	})
}

// LockRegistration 见 UserTx.LockRegistration；必须在事务里调用。
func (r *UserRepository) LockRegistration(ctx context.Context) error {
	return r.db.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(?)", registrationLockKey).Error
}

// LockRoleChange 见 UserTx.LockRoleChange；必须在事务里调用。
func (r *UserRepository) LockRoleChange(ctx context.Context) error {
	return r.db.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(?)", roleChangeLockKey).Error
}

// CountByRole 统计某角色的用户数（软删除的不算）。
func (r *UserRepository) CountByRole(ctx context.Context, role string) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.User{}).Where("role = ?", role).Count(&n).Error
	return n, err
}

// ResetPassword 原子地改密码哈希并把 token_version +1（单条 UPDATE，不会读到旧版本再写回），
// 让该用户已签发的 token 失效；用户不存在返回 ErrNotFound。hash 必须是 bcrypt 哈希，调用方保证。
func (r *UserRepository) ResetPassword(ctx context.Context, id uint64, hash string) error {
	res := r.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", id).
		Updates(map[string]any{"password": hash, "token_version": gorm.Expr("token_version + 1")})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Count 统计用户数（软删除的不算）。
func (r *UserRepository) Count(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.User{}).Count(&n).Error
	return n, err
}

// UsernameExists 用户名是否已存在（软删除的用户仍占着用户名，因为唯一索引不区分软删除，所以这里也 Unscoped）。
func (r *UserRepository) UsernameExists(ctx context.Context, username string) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Unscoped().Model(&model.User{}).Where("username = ?", username).Count(&n).Error
	return n > 0, err
}

// EmailExists 邮箱是否已存在：库里统一存小写，这里把入参也转小写再比较（含软删除的用户，理由同 UsernameExists）。
func (r *UserRepository) EmailExists(ctx context.Context, email string) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Unscoped().Model(&model.User{}).Where("email = ?", strings.ToLower(email)).Count(&n).Error
	return n > 0, err
}

// CreateCredit 建积分账户并写 initial 流水（同一个事务；已在事务里时退化为保存点）。账户已存在时什么都不做。
func (r *UserRepository) CreateCredit(ctx context.Context, userID uint64, initial int) error {
	return insertInitialCredit(ctx, r.db, userID, initial)
}

// insertInitialCredit 在 db（事务或普通连接）上插入积分账户，真正插入了新行才写 initial 流水：
// 保证“有账户就有且仅有一条 initial 流水”，老用户惰性建账户与注册共用这一处。
func insertInitialCredit(ctx context.Context, db *gorm.DB, userID uint64, initial int) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		acc := &model.UserCredit{UserID: userID, Balance: initial, UpdatedAt: time.Now()}
		res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(acc)
		if res.Error != nil || res.RowsAffected == 0 {
			return res.Error
		}
		return tx.Create(&model.CreditLedger{UserID: userID, Type: model.LedgerInitial, Amount: initial}).Error
	})
}

// InsertLoginLog 写一条登录记录。
func (r *UserRepository) InsertLoginLog(ctx context.Context, l *model.UserLoginLog) error {
	return r.db.WithContext(ctx).Create(l).Error
}

// UserListFilter 是后台用户列表的筛选与分页条件。
type UserListFilter struct {
	ID     uint64 // 非 0 时只查这个用户（详情用）
	Q      string // 模糊匹配用户名 / 邮箱（ILIKE，% _ \ 已转义）
	Status string // 为空不筛
	Role   string // 为空不筛
	Offset int
	Limit  int
}

// likeEscaper 把 LIKE 通配符转义成普通字符，避免关键字里的 % _ 匹配到不相关的用户。
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// ListAdmin 分页查询后台用户列表（id 倒序）：联积分账户与进行中任务数（不含试跑），同时返回满足筛选的总数。
// 软删除的用户不出现。
func (r *UserRepository) ListAdmin(ctx context.Context, f UserListFilter) ([]model.AdminUserRow, int64, error) {
	base := r.db.WithContext(ctx).Table("users u").Where("u.deleted_at IS NULL")
	if q := strings.TrimSpace(f.Q); q != "" {
		like := "%" + likeEscaper.Replace(q) + "%"
		base = base.Where(`(u.username ILIKE ? ESCAPE '\' OR u.email ILIKE ? ESCAPE '\')`, like, like)
	}
	if f.ID != 0 {
		base = base.Where("u.id = ?", f.ID)
	}
	if f.Status != "" {
		base = base.Where("u.status = ?", f.Status)
	}
	if f.Role != "" {
		base = base.Where("u.role = ?", f.Role)
	}
	var total int64
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.AdminUserRow
	err := base.Session(&gorm.Session{}).
		Select(`u.id, u.username, u.email, u.role, u.status, COALESCE(c.balance, 0) AS balance, COALESCE(c.frozen, 0) AS frozen,
			(c.user_id IS NOT NULL) AS has_credit_account, u.max_active_tasks, u.last_login_at, u.created_at,
			(SELECT COUNT(*) FROM generation_tasks t WHERE t.user_id = u.id AND t.is_test = FALSE AND t.status IN ?) AS active_tasks`, activeStatuses()).
		Joins("LEFT JOIN user_credits c ON c.user_id = u.id").
		Order("u.id DESC").Offset(f.Offset).Limit(f.Limit).Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// TaskStats 汇总用户的正式任务统计（不含试跑）与已结算积分。
func (r *UserRepository) TaskStats(ctx context.Context, userID uint64) (*model.UserTaskStats, error) {
	var st model.UserTaskStats
	err := r.db.WithContext(ctx).Raw(`
		SELECT COUNT(*) AS total,
		  COUNT(*) FILTER (WHERE status = 'succeeded') AS success,
		  COUNT(*) FILTER (WHERE status IN ('failed', 'expired')) AS failed,
		  COUNT(*) FILTER (WHERE created_at >= now() - interval '7 days') AS last7d
		FROM generation_tasks WHERE user_id = ? AND is_test = FALSE`, userID).Row().Scan(&st.Total, &st.Success, &st.Failed, &st.Last7d)
	if err != nil {
		return nil, err
	}
	err = r.db.WithContext(ctx).Raw(`SELECT COALESCE(SUM(amount), 0) FROM credit_ledger WHERE user_id = ? AND type = 'settle'`, userID).
		Row().Scan(&st.SpentCredits)
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// AdminAuditRepository 是后台运营审计日志的数据访问层。
type AdminAuditRepository struct {
	db *gorm.DB
}

// NewAdminAuditRepository 创建审计仓储。
func NewAdminAuditRepository(db *gorm.DB) *AdminAuditRepository {
	return &AdminAuditRepository{db: db}
}

// Insert 写一条审计日志（只增不改不删）。
func (r *AdminAuditRepository) Insert(ctx context.Context, l *model.AdminAuditLog) error {
	return r.db.WithContext(ctx).Create(l).Error
}

// RecentForUser 返回针对某用户的最近 limit 条审计（target_type=user），按时间倒序，带操作人用户名。
func (r *AdminAuditRepository) RecentForUser(ctx context.Context, userID uint64, limit int) ([]model.AdminAuditView, error) {
	var out []model.AdminAuditView
	err := r.db.WithContext(ctx).Table("admin_audit_logs a").
		Select("a.actor_id, COALESCE(u.username, '') AS actor_name, a.action, a.detail_json, a.created_at").
		Joins("LEFT JOIN users u ON u.id = a.actor_id").
		Where("a.target_type = ? AND a.target_id = ?", model.AdminAuditTargetUser, userID).
		Order("a.id DESC").Limit(limit).Scan(&out).Error
	return out, err
}

// SystemSettingRepository 是 system_settings 键值表的数据访问层。
type SystemSettingRepository struct {
	db *gorm.DB
}

// NewSystemSettingRepository 创建系统设置仓储。
func NewSystemSettingRepository(db *gorm.DB) *SystemSettingRepository {
	return &SystemSettingRepository{db: db}
}

// GetAll 返回全部设置（key -> value）；表为空返回空 map。
func (r *SystemSettingRepository) GetAll(ctx context.Context) (map[string]string, error) {
	var rows []model.SystemSetting
	if err := r.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[row.Key] = row.Value
	}
	return out, nil
}

// SetMany 在一个事务里 upsert 多个设置。
func (r *SystemSettingRepository) SetMany(ctx context.Context, kv map[string]string, updatedBy uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		for k, v := range kv {
			row := &model.SystemSetting{Key: k, Value: v, UpdatedBy: updatedBy, UpdatedAt: now}
			err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "key"}},
				DoUpdates: clause.AssignmentColumns([]string{"value", "updated_by", "updated_at"}),
			}).Create(row).Error
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// SMTPSettingRepository 是 smtp_settings 单行表的数据访问层。
type SMTPSettingRepository struct {
	db *gorm.DB
}

// NewSMTPSettingRepository 创建 SMTP 设置仓储。
func NewSMTPSettingRepository(db *gorm.DB) *SMTPSettingRepository {
	return &SMTPSettingRepository{db: db}
}

// Get 读取 SMTP 配置；还没有保存过返回 ErrNotFound。
func (r *SMTPSettingRepository) Get(ctx context.Context) (*model.SMTPSetting, error) {
	var s model.SMTPSetting
	if err := r.db.WithContext(ctx).First(&s, model.SMTPSettingID).Error; err != nil {
		return nil, translate(err)
	}
	return &s, nil
}

// SaveConfig upsert 除密码与检查结果外的配置字段：保存配置不能清掉已设置的密码。
func (r *SMTPSettingRepository) SaveConfig(ctx context.Context, s *model.SMTPSetting) error {
	s.ID = model.SMTPSettingID
	s.UpdatedAt = time.Now()
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"host", "port", "encryption", "username", "from_address", "from_name", "enabled", "updated_by", "updated_at",
		}),
	}).Create(s).Error
}

// SetPassword upsert 密码密文与随机数；此时还没有配置行也能先设密码。
func (r *SMTPSettingRepository) SetPassword(ctx context.Context, enc, nonce []byte, updatedBy uint64) error {
	row := &model.SMTPSetting{ID: model.SMTPSettingID, Encryption: model.SMTPEncStartTLS, PasswordEnc: enc, PasswordNonce: nonce, UpdatedBy: updatedBy, UpdatedAt: time.Now()}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"password_enc", "password_nonce", "updated_by", "updated_at"}),
	}).Create(row).Error
}

// RecordCheck 记录最近一次测试发信的结果；还没有配置行时忽略（没有配置也就没有可记录的检查）。
func (r *SMTPSettingRepository) RecordCheck(ctx context.Context, ok bool, errMsg string, at time.Time) error {
	return r.db.WithContext(ctx).Model(&model.SMTPSetting{}).Where("id = ?", model.SMTPSettingID).
		Updates(map[string]any{"last_check_ok": ok, "last_check_error": errMsg, "last_check_at": at}).Error
}
