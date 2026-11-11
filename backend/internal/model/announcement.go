package model

import "time"

// 公告在后台列表里的派生状态（不入库，按启用开关、生效时间和排名实时算出）。
const (
	AnnouncementStatusDisabled  = "disabled"  // 已停用：enabled=false
	AnnouncementStatusScheduled = "scheduled" // 待上线：启用，但开始时间还没到
	AnnouncementStatusExpired   = "expired"   // 已过期：启用，但结束时间已过
	AnnouncementStatusOverflow  = "overflow"  // 超出上限：启用且在生效期内，但排名在公开展示条数之后
	AnnouncementStatusLive      = "live"      // 展示中：用户端能看到
)

// Announcement 首页顶部公告：后台发布，首页外壳最顶部的通栏轮播展示（设计：docs/design/公告/公告.md）。
// 全站共享，不属于某个用户；本期只有「通知」一种类型，不存类型字段。
type Announcement struct {
	BaseModel
	Title     string     `gorm:"size:255;not null" json:"title"`       // 标题，去首尾空白后 1–60 个字符（service 校验）
	Enabled   bool       `gorm:"not null" json:"enabled"`              // 是否启用；不写 gorm default，避免 false 被零值规则吞成 true
	StartsAt  *time.Time `json:"starts_at"`                            // 开始展示时间，nil = 立即
	EndsAt    *time.Time `json:"ends_at"`                              // 结束展示时间，nil = 不过期
	Sort      int        `gorm:"not null;default:0;index" json:"sort"` // 排序，越小越靠前
	CreatedBy uint64     `gorm:"not null;default:0" json:"-"`          // 创建人（管理员用户 ID），不对外返回
	UpdatedBy uint64     `gorm:"not null;default:0" json:"-"`          // 最后修改人（管理员用户 ID），不对外返回
}

// TableName 返回表名。
func (Announcement) TableName() string { return "announcements" }

// AnnouncementPublicItem 是 GET /announcements 里的一条公告：只暴露用户端展示所需的字段。
type AnnouncementPublicItem struct {
	ID      uint64     `json:"id"`      // 公告 ID
	Title   string     `json:"title"`   // 标题
	EndsAt  *time.Time `json:"ends_at"` // 结束时间，前端到点自己隐藏；nil = 不过期
	Version int64      `json:"version"` // 内容版本（updated_at 的 Unix 毫秒），前端按 id+version 记关闭，改了内容会重新出现
}

// AnnouncementPublicView 是 GET /announcements 的响应数据。
type AnnouncementPublicView struct {
	Items []AnnouncementPublicItem `json:"items"` // 生效中的公告，按后台排序；最多 5 条；永不为 nil
}

// AnnouncementAdminItem 是后台看到的一条公告：比公开视图多出启用状态、时间窗口、排序与派生状态。
type AnnouncementAdminItem struct {
	ID        uint64     `json:"id"`         // 公告 ID
	Title     string     `json:"title"`      // 标题
	Enabled   bool       `json:"enabled"`    // 是否启用
	StartsAt  *time.Time `json:"starts_at"`  // 开始时间，null = 立即
	EndsAt    *time.Time `json:"ends_at"`    // 结束时间，null = 不过期
	Sort      int        `json:"sort"`       // 排序
	Status    string     `json:"status"`     // 派生状态，见 AnnouncementStatus* 常量
	CreatedAt time.Time  `json:"created_at"` // 创建时间
	UpdatedAt time.Time  `json:"updated_at"` // 最后修改时间
}

// AnnouncementAdminView 是 GET /admin/settings/announcements 的响应数据。
type AnnouncementAdminView struct {
	Items []AnnouncementAdminItem `json:"items"` // 全部未删除公告（含停用），按排序升序；永不为 nil
}

// SaveAnnouncementReq 是新建（POST）与编辑（PUT /:id）公告的请求体：编辑是整体替换，前端提交全部字段。
type SaveAnnouncementReq struct {
	Title    string     `json:"title"`                                 // 去首尾空白后 1–60 个字符，由 service 校验
	Enabled  *bool      `json:"enabled" binding:"required" label:"启用"` // 必填，避免漏传被当成 false 悄悄停用
	StartsAt *time.Time `json:"starts_at"`                             // 可空；RFC 3339
	EndsAt   *time.Time `json:"ends_at"`                               // 可空；RFC 3339，都给时必须晚于开始时间
}

// SetAnnouncementEnabledReq 是 PUT /admin/settings/announcements/:id/enabled 的请求体。
type SetAnnouncementEnabledReq struct {
	Enabled *bool `json:"enabled" binding:"required" label:"启用"` // 启用 / 停用
}

// ReorderAnnouncementsReq 是 PUT /admin/settings/announcements/order 的请求体：必须恰好包含全部未删除公告的 ID 各一次。
type ReorderAnnouncementsReq struct {
	IDs []uint64 `json:"ids" binding:"required" label:"排序列表"` // 按期望顺序排列的公告 ID
}
