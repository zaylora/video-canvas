package model

import "time"

// 用户角色。
const (
	RoleUser       = "user"        // 普通用户
	RoleAdmin      = "admin"       // 管理员（运营）：只管模型
	RoleSuperAdmin = "super_admin" // 超级管理员（运维）：装插件、管渠道与凭证；users 表为空时的首个注册者自动成为 super_admin
)

// 用户状态。
const (
	UserStatusActive   = "active"   // 正常
	UserStatusDisabled = "disabled" // 已停用：不能登录，已签发的 token 也立即失效
)

// User 是用户表。该结构同时被序列化进 Redis 缓存（user:<id>），所以鉴权要用到的字段都要带 json 标签。
type User struct {
	BaseModel
	Username        string     `gorm:"size:64;uniqueIndex;not null" json:"username"`                       // 用户名，唯一
	Password        string     `gorm:"size:128;not null" json:"-"`                                         // bcrypt 哈希后的密码
	Nickname        string     `gorm:"size:64" json:"nickname"`                                            // 昵称
	Email           string     `gorm:"size:128;uniqueIndex:uk_users_email,where:email <> ''" json:"email"` // 邮箱，统一存小写；空串不参与唯一约束
	Role            string     `gorm:"size:16;not null;default:user" json:"role"`                          // user / admin / super_admin
	Status          string     `gorm:"size:16;not null;default:active" json:"status"`                      // active / disabled
	MaxActiveTasks  *int       `json:"max_active_tasks"`                                                   // 单用户并发上限覆盖，空表示用全局默认
	EmailVerifiedAt *time.Time `json:"email_verified_at"`                                                  // 邮箱通过验证码验证的时间
	LastLoginAt     *time.Time `json:"last_login_at"`                                                      // 最近一次登录时间
	TokenVersion    int        `gorm:"not null;default:0" json:"token_version"`                            // token 版本：重置密码等操作 +1，使旧 token 失效
}

func (User) TableName() string { return "users" }

// 登录请求参数
type LoginUserReq struct {
	Username string `json:"username" binding:"required,min=3,max=64" label:"用户名"` // 用户名
	Password string `json:"password" binding:"required,min=6,max=128" label:"密码"` // 明文密码
}

// RegisterUserReq 是 POST /auth/register 的请求参数。
type RegisterUserReq struct {
	Username string `json:"username" binding:"required,min=3,max=64" label:"用户名"` // 用户名
	Email    string `json:"email" binding:"required,email,max=128" label:"邮箱"`    // 邮箱（必填，大小写不敏感）
	Password string `json:"password" binding:"required,min=6,max=128" label:"密码"` // 明文密码
	Code     string `json:"code" binding:"omitempty,max=16" label:"验证码"`          // 邮箱验证码；需要验证时必填
}

// SendRegisterCodeReq 是 POST /auth/register/code 的请求参数。
type SendRegisterCodeReq struct {
	Email string `json:"email" binding:"required,email,max=128" label:"邮箱"` // 接收验证码的邮箱
}

// AuthConfigView 是 GET /auth/config 的响应。
type AuthConfigView struct {
	RegisterEnabled     bool `json:"register_enabled"`      // 当前是否开放注册
	EmailVerifyRequired bool `json:"email_verify_required"` // 注册是否需要邮箱验证码
}

// LoginView 是登录 / 注册即登录的响应。
type LoginView struct {
	Token    string `json:"token"`     // JWT
	ExpireAt int64  `json:"expire_at"` // 过期时间（Unix 秒）
	Role     string `json:"role"`      // 用户角色
}

// AdminUserRow 是后台用户列表一行的查询结果（用户 + 积分账户 + 进行中任务数），由 repository 联表查出。
// 有效并发上限依赖全局设置，由 service 算出后再组装成 AdminUserListItem。
type AdminUserRow struct {
	ID               uint64     `json:"id"`
	Username         string     `json:"username"`
	Email            string     `json:"email"`
	Role             string     `json:"role"`
	Status           string     `json:"status"`
	Balance          int        `json:"balance"`
	Frozen           int        `json:"frozen"`
	HasCreditAccount bool       `json:"has_credit_account"`
	MaxActiveTasks   *int       `json:"max_active_tasks"`
	ActiveTasks      int        `json:"active_tasks"`
	LastLoginAt      *time.Time `json:"last_login_at"`
	CreatedAt        time.Time  `json:"created_at"`
}

// AdminUserListItem 是 GET /admin/users 列表项（契约 §3 UserListItem）。
type AdminUserListItem struct {
	AdminUserRow
	Available               int `json:"available"`                  // 可用积分 = 余额 - 冻结
	EffectiveMaxActiveTasks int `json:"effective_max_active_tasks"` // 实际生效的并发上限
}

// UserTaskStats 是用户详情里的任务统计。
type UserTaskStats struct {
	Total        int   `json:"task_total"`    // 正式任务总数（不含试跑）
	Success      int   `json:"task_success"`  // 成功数
	Failed       int   `json:"task_failed"`   // 失败数（failed + expired）
	Last7d       int   `json:"tasks_last_7d"` // 近 7 天创建的任务数
	SpentCredits int64 `json:"spent_credits"` // 已结算（真正扣掉）的积分合计
}

// AdminAuditView 是用户详情里展示的审计记录（带操作人用户名）。
type AdminAuditView struct {
	ActorID    uint64    `json:"actor_id"`
	ActorName  string    `json:"actor_name"`
	Action     string    `json:"action"`
	DetailJSON JSONText  `json:"detail_json"`
	CreatedAt  time.Time `json:"created_at"`
}

// AdminUserDetail 是 GET /admin/users/:id 的响应（契约 §3 UserDetail）。
type AdminUserDetail struct {
	AdminUserListItem
	EmailVerifiedAt *time.Time `json:"email_verified_at"`
	UserTaskStats
	RecentAudits []AdminAuditView `json:"recent_audits"` // 最多 3 条
}

// ListAdminUserReq 是 GET /admin/users 的查询参数。
type ListAdminUserReq struct {
	Q        string `form:"q" binding:"max=64" label:"关键字"`                                   // 模糊匹配用户名 / 邮箱
	Status   string `form:"status" binding:"omitempty,oneof=active disabled" label:"状态"`      // active / disabled
	Role     string `form:"role" binding:"omitempty,oneof=user admin super_admin" label:"角色"` // 角色
	Page     int    `form:"page" label:"页码"`                                                  // 页码，从 1 开始
	PageSize int    `form:"page_size" label:"每页条数"`                                           // 每页条数，最大 100
}
