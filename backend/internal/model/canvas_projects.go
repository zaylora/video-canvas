package model

import (
	"encoding/json"
	"time"

	"gorm.io/datatypes"
)

// CanvasProject 画布主表，完整画布内容整体存放在 PayloadJSON 中。
type CanvasProject struct {
	BaseModel
	UserID      uint64         `gorm:"not null;index;" json:"user_id"`            //  所属用户
	Title       string         `gorm:"size:255;not null;default:''" json:"title"` //  画布标题
	PayloadJSON datatypes.JSON `gorm:"type:jsonb;not null;" json:"payload_json"`  //  完整画布内容
	Revision    uint64         `gorm:"not null;default:1;" json:"revision"`       //  乐观锁版本号
}

func (CanvasProject) TableName() string { return "canvas_projects" }

// CanvasPayload 描述 payload_json 的结构，各子项内容由前端自行定义。
type CanvasPayload struct {
	ID             string          `json:"id"`             // 画布 ID
	Title          string          `json:"title"`          // 画布标题
	Nodes          json.RawMessage `json:"nodes"`          // 节点列表
	Connections    json.RawMessage `json:"connections"`    // 节点连线
	ChatSessions   json.RawMessage `json:"chatSessions"`   // 对话会话
	DirectorScenes json.RawMessage `json:"directorScenes"` // 导演分镜场景
	Timeline       json.RawMessage `json:"timeline"`       // 时间线
	Appearance     json.RawMessage `json:"appearance"`     // 外观设置
	Viewport       json.RawMessage `json:"viewport"`       // 视口（缩放、平移）
}

// 创建画布请求参数
type CreateCanvasProjectReq struct {
	Title       string          `json:"title" binding:"required,max=255" label:"画布标题"` // 画布标题
	PayloadJSON json.RawMessage `json:"payload_json" label:"画布内容"`                     // 不传时默认为 {}
}

// 更新画布请求参数，title 和 payload_json 至少传一个
type UpdateCanvasProjectReq struct {
	Title       *string         `json:"title" binding:"omitempty,min=1,max=255" label:"画布标题"` // 新标题，不传表示不修改
	PayloadJSON json.RawMessage `json:"payload_json" label:"画布内容"`                            // 新的画布内容，不传表示不修改
	Revision    uint64          `json:"revision" binding:"required" label:"版本号"`              // 客户端当前持有的版本号
}

// 画布列表请求参数
type ListCanvasProjectReq struct {
	Page     int    `form:"page" label:"页码"`                       // 页码，从 1 开始
	PageSize int    `form:"page_size" label:"每页条数"`                // 每页条数
	Keyword  string `form:"keyword" binding:"max=255" label:"关键词"` // 按标题模糊搜索
}

// 画布列表项，不含 payload_json，避免列表接口返回大字段
type CanvasProjectItem struct {
	ID        uint64    `json:"id"`         // 画布 ID
	Title     string    `json:"title"`      // 画布标题
	Revision  uint64    `json:"revision"`   // 乐观锁版本号
	CreatedAt time.Time `json:"created_at"` // 创建时间
	UpdatedAt time.Time `json:"updated_at"` // 更新时间
}
