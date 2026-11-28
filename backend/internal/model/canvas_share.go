package model

import "time"

// 画布的所有者、可编辑成员和仅查看成员角色。
const (
	CanvasRoleOwner  = "owner"
	CanvasRoleEditor = "editor"
	CanvasRoleViewer = "viewer"
)

// CanvasViewer 记录一位用户对一张原画布的成员授权。
type CanvasViewer struct {
	Canvas           CanvasProject `gorm:"foreignKey:CanvasID;references:ID;constraint:OnDelete:CASCADE" json:"-"`
	User             User          `gorm:"foreignKey:UserID;references:ID;constraint:OnDelete:CASCADE" json:"-"`
	CanvasID         uint64        `gorm:"primaryKey;autoIncrement:false;not null;index:idx_canvas_viewers_user,priority:2"`
	UserID           uint64        `gorm:"primaryKey;autoIncrement:false;not null;index:idx_canvas_viewers_user,priority:1"`
	Role             string        `gorm:"size:8;not null;default:viewer;check:chk_canvas_viewers_role,role IN ('viewer','editor')"`
	CanGenerate      bool          `gorm:"not null;default:false"`
	CanCopyToLibrary bool          `gorm:"not null;default:false"`
}

// TableName 返回授权或交付记录的表名。
func (CanvasViewer) TableName() string { return "canvas_viewers" }

// CanvasDelivery 绑定请求参数、当前尝试和已提交结果。
type CanvasDelivery struct {
	Attempt    string    `gorm:"size:64;not null"`
	UserID     uint64    `gorm:"primaryKey;autoIncrement:false"`
	RequestKey string    `gorm:"primaryKey;size:64;not null"`
	CanvasID   uint64    `gorm:"not null"`
	Recipient  uint64    `gorm:"not null"`
	Mode       string    `gorm:"size:12;not null"`
	Revision   uint64    `gorm:"not null"`
	ResultID   uint64    `gorm:"not null;default:0"`
	Status     string    `gorm:"size:16;not null;default:running"`
	UpdatedAt  time.Time `gorm:"not null;default:now();autoUpdateTime"`
}

// TableName 返回授权或交付记录的表名。
func (CanvasDelivery) TableName() string { return "canvas_deliveries" }

// CanvasShareReq 描述按邮箱授权或交付的请求。
type CanvasShareReq struct {
	Mode       string `json:"mode" binding:"required,oneof=view copy transfer"`
	Email      string `json:"email" binding:"required,email,max=128"`
	RequestKey string `json:"request_key" binding:"required_unless=Mode view,max=64"`
	Revision   uint64 `json:"revision" binding:"required_unless=Mode view"`
}

// CanvasShareItem 是共享给当前用户的列表项。
type CanvasShareItem struct {
	Role       string    `json:"role,omitempty"`
	CoverAsset string    `json:"cover_asset,omitempty"`
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Revision   uint64    `json:"revision"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// CanvasShareMember 是原画布的只读成员。
type CanvasShareMember struct {
	CanGenerate      bool   `json:"canGenerate,omitempty"`
	CanCopyToLibrary bool   `json:"canCopyToLibrary,omitempty"`
	Role             string `json:"role,omitempty"`
	ID               string `json:"id"`
	Email            string `json:"email"`
	Username         string `json:"username"`
}

// CanvasLibraryCopyReq 绑定个人收藏与当前协作会话。
type CanvasLibraryCopyReq struct {
	Epoch  uint64 `json:"epoch" binding:"required"`
	NodeID string `json:"nodeId" binding:"required,max=128"`
	AddAssetLibraryItemReq
}

// CanvasDeliveryStage 记录提交前创建的存储对象，供异常中断后回收。
type CanvasDeliveryStage struct {
	StorageKey string    `gorm:"primaryKey"`
	StorageID  uint64    `gorm:"not null"`
	Attempt    string    `gorm:"size:64;index:idx_canvas_delivery_stage_attempt;not null"`
	CreatedAt  time.Time `gorm:"index:idx_canvas_delivery_stage_expiry;default:now();not null"`
}
