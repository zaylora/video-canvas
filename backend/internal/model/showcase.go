package model

import (
	"bytes"
	"encoding/json"
	"time"
)

// ShowcaseItem 登录页展示条目：后台挑选的一段视频素材，登录页（未登录）轮播播放。
// 视频与封面都引用素材表里的素材（assets.id），条目只存展示相关的文案与排序。
type ShowcaseItem struct {
	BaseModel
	AssetID       uint64  `gorm:"not null;index" json:"asset_id"`                 // 视频素材 ID
	PosterAssetID *uint64 `json:"poster_asset_id"`                                // 封面图素材 ID，没有封面时为 nil
	Prompt        string  `gorm:"size:255;not null" json:"prompt"`                // 生成它的那句话，显示在登录页画面上（≤80 字）
	ModelLabel    string  `gorm:"size:80;not null;default:''" json:"model_label"` // 模型名称标注（≤40 字，可空）
	StartSec      float64 `gorm:"not null;default:0" json:"start_sec"`            // 从视频第几秒开始播放
	Enabled       bool    `gorm:"not null" json:"enabled"`                        // 是否在登录页展示；不写 gorm default，避免 false 被零值规则吞成 true
	Sort          int     `gorm:"not null;default:0;index" json:"sort"`           // 排序，越小越靠前
	CreatedBy     uint64  `gorm:"not null;default:0" json:"-"`                    // 创建人（管理员用户 ID），不对外返回
}

// TableName 返回表名。
func (ShowcaseItem) TableName() string { return "showcase_items" }

// ShowcaseSettingsView 登录页展示的全局设置：公开接口与后台读写共用同一结构。
type ShowcaseSettingsView struct {
	ClipSeconds          int  `json:"clip_seconds"`             // 每段视频播放秒数（4–15）
	ShowOnLogin          bool `json:"show_on_login"`            // 登录页是否展示轮播
	PosterOnlyOnSaveData bool `json:"poster_only_on_save_data"` // 省流量模式下是否只显示封面
}

// UpdateShowcaseSettingsReq 是 PUT /admin/settings/showcase/settings 的请求体；三个字段都必填，避免漏传被当成 false 关掉开关。
type UpdateShowcaseSettingsReq struct {
	ClipSeconds          *int  `json:"clip_seconds" binding:"required" label:"片段秒数"`                 // 4–15，范围由 service 校验
	ShowOnLogin          *bool `json:"show_on_login" binding:"required" label:"登录页展示开关"`             // 登录页是否展示
	PosterOnlyOnSaveData *bool `json:"poster_only_on_save_data" binding:"required" label:"省流量只显示封面"` // 省流量模式是否只显示封面
}

// ShowcasePublicItem 是公开接口里的一个条目：只暴露登录页播放所需的字段，不含素材 ID、创建人等内部信息。
type ShowcasePublicItem struct {
	ID         uint64  `json:"id"`          // 条目 ID
	VideoURL   string  `json:"video_url"`   // 视频稳定地址
	PosterURL  string  `json:"poster_url"`  // 封面稳定地址，没有封面为空串
	Prompt     string  `json:"prompt"`      // 生成它的那句话
	ModelLabel string  `json:"model_label"` // 模型名称标注
	StartSec   float64 `json:"start_sec"`   // 起始秒
	Width      int     `json:"width"`       // 视频宽度（像素）
	Height     int     `json:"height"`      // 视频高度（像素）
	ByteSize   int64   `json:"byte_size"`   // 视频大小（字节）
}

// ShowcasePublicView 是 GET /showcase 的响应数据。
type ShowcasePublicView struct {
	Settings ShowcaseSettingsView `json:"settings"` // 展示设置
	Items    []ShowcasePublicItem `json:"items"`    // 已启用的条目，按排序升序；永不为 nil
}

// ShowcaseAdminItem 是后台看到的条目：比公开视图多出素材 ID、启用状态、文件名等管理信息。
type ShowcaseAdminItem struct {
	ID            uint64    `json:"id"`              // 条目 ID
	AssetID       uint64    `json:"asset_id"`        // 视频素材 ID
	PosterAssetID *uint64   `json:"poster_asset_id"` // 封面素材 ID，无则 null
	VideoURL      string    `json:"video_url"`       // 视频稳定地址；视频素材已被删除时为空串
	PosterURL     string    `json:"poster_url"`      // 封面稳定地址，没有封面（或封面素材已删除）为空串
	Prompt        string    `json:"prompt"`          // 生成它的那句话
	ModelLabel    string    `json:"model_label"`     // 模型名称标注
	StartSec      float64   `json:"start_sec"`       // 起始秒
	Enabled       bool      `json:"enabled"`         // 是否启用
	Width         int       `json:"width"`           // 视频宽度（像素）
	Height        int       `json:"height"`          // 视频高度（像素）
	ByteSize      int64     `json:"byte_size"`       // 视频大小（字节）
	DurationMs    int64     `json:"duration_ms"`     // 视频时长（毫秒）
	FileName      string    `json:"file_name"`       // 视频原始文件名
	CreatedAt     time.Time `json:"created_at"`      // 创建时间
}

// ShowcaseAdminView 是 GET /admin/settings/showcase 的响应数据。
type ShowcaseAdminView struct {
	Settings ShowcaseSettingsView `json:"settings"` // 展示设置
	Items    []ShowcaseAdminItem  `json:"items"`    // 全部条目（含禁用），按排序升序；永不为 nil
}

// CreateShowcaseItemReq 是 POST /admin/settings/showcase/items 的请求体。
type CreateShowcaseItemReq struct {
	AssetID       uint64  `json:"asset_id" binding:"required,min=1" label:"视频素材"` // 视频素材：当前管理员自己的视频，或平台生成的视频（任何用户的）
	PosterAssetID *uint64 `json:"poster_asset_id"`                                // 可空；给了必须是当前管理员的图片素材
	Prompt        string  `json:"prompt"`                                         // 去首尾空白后 1–80 字，由 service 校验
	ModelLabel    string  `json:"model_label"`                                    // ≤40 字，可空
	StartSec      float64 `json:"start_sec"`                                      // 0–3600，默认 0
	Enabled       *bool   `json:"enabled"`                                        // 缺省 true
}

// OptionalID 区分 JSON 里“没传”“传 null”“传了数字”三种情况：
// 没传时 UnmarshalJSON 根本不会被调用（Set 保持 false）；传 null 时 Set=true、ID=nil；传数字时 Set=true、ID 指向该数字。
type OptionalID struct {
	Set bool    // 请求里是否出现了这个字段
	ID  *uint64 // 字段的值，null 为 nil
}

// UnmarshalJSON 实现 json.Unmarshaler。null 也会走到这里（目标是非指针的可寻址值），非数字或负数报错，由绑定层转成 400。
func (o *OptionalID) UnmarshalJSON(b []byte) error {
	o.Set = true
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		o.ID = nil
		return nil
	}
	var v uint64
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	o.ID = &v
	return nil
}

// UpdateShowcaseItemReq 是 PUT /admin/settings/showcase/items/:id 的请求体：全部可选，没传的字段不改。
type UpdateShowcaseItemReq struct {
	AssetID       *uint64    `json:"asset_id" binding:"omitempty,min=1" label:"视频素材"` // 替换视频：没传 = 不改；规则同新增（自己的视频或平台生成的视频），不会自动改封面
	Prompt        *string    `json:"prompt"`                                          // 新文案
	ModelLabel    *string    `json:"model_label"`                                     // 新模型标注，传空串表示清空
	StartSec      *float64   `json:"start_sec"`                                       // 新起始秒
	Enabled       *bool      `json:"enabled"`                                         // 启用 / 禁用
	PosterAssetID OptionalID `json:"poster_asset_id"`                                 // 没传 = 不改；传 null = 清空封面；传数字 = 换封面
}

// ReorderShowcaseReq 是 PUT /admin/settings/showcase/order 的请求体：必须恰好包含全部现有条目 ID 各一次。
type ReorderShowcaseReq struct {
	IDs []uint64 `json:"ids" binding:"required" label:"排序列表"` // 按期望顺序排列的条目 ID
}

// ListShowcaseLibraryReq 是 GET /admin/settings/showcase/library 的查询参数；非法值由 service 回落默认。
type ListShowcaseLibraryReq struct {
	Page     int `form:"page" label:"页码"`        // 页码，默认 1
	PageSize int `form:"page_size" label:"每页条数"` // 每页条数，默认 48，最大 100
}

// ShowcaseLibraryItem 是素材库里的一个平台生成视频，供管理员挑选后添加为展示条目。
type ShowcaseLibraryItem struct {
	AssetID    uint64    `json:"asset_id"`    // 视频素材 ID
	VideoURL   string    `json:"video_url"`   // 视频稳定地址 /files/<key>
	Prompt     string    `json:"prompt"`      // 生成它的提示词，取不到为空串，最多 80 个字符
	ModelLabel string    `json:"model_label"` // 模型展示名，取不到退回模型 key，再取不到为空串
	Owner      string    `json:"owner"`       // 素材所属用户的用户名，查不到为 "用户#<id>"
	CreatedAt  time.Time `json:"created_at"`  // 素材生成时间
	Width      int       `json:"width"`       // 视频宽度（像素）
	Height     int       `json:"height"`      // 视频高度（像素）
	ByteSize   int64     `json:"byte_size"`   // 视频大小（字节）
	DurationMs int64     `json:"duration_ms"` // 视频时长（毫秒）
	Added      bool      `json:"added"`       // 是否已被某个未删除的展示条目引用
}

// ShowcaseLibraryView 是 GET /admin/settings/showcase/library 的响应数据。
type ShowcaseLibraryView struct {
	Items    []ShowcaseLibraryItem `json:"items"`     // 当前页素材，按创建时间倒序；永不为 nil
	Total    int64                 `json:"total"`     // 平台生成视频总数
	Page     int                   `json:"page"`      // 修正后的页码
	PageSize int                   `json:"page_size"` // 修正后的每页条数
}
