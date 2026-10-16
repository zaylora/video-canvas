package model

import (
	"time"

	"video-canvas/internal/pkg/idcodec" //nolint:depguard // 对外 id 用十六进制串，与画布、对话（存量代码）一致
)

// AssetFolder 个人资产库的文件夹：每个用户一层平铺，没有嵌套。
// 同一用户下名称忽略大小写唯一（表达式唯一索引 lower(name)，迁移里同名）。
type AssetFolder struct {
	ID        uint64    `gorm:"primaryKey" json:"id"`                                                                                  // 文件夹 ID
	UserID    uint64    `gorm:"not null;uniqueIndex:uk_asset_folders_user_name,priority:1" json:"user_id"`                             // 所属用户
	Name      string    `gorm:"size:30;not null;uniqueIndex:uk_asset_folders_user_name,priority:2,expression:lower(name)" json:"name"` // 名称，1–30 字
	CreatedAt time.Time `json:"created_at"`                                                                                            // 创建时间
	UpdatedAt time.Time `json:"updated_at"`                                                                                            // 更新时间
}

// TableName 表名。
func (AssetFolder) TableName() string { return "asset_folders" }

// AssetLibraryItem 个人资产库的一条归属：同一条素材（assets）在该用户的库里最多一条，不复制文件。
// 不使用外键：删除文件夹时由服务在同一事务里先删归属行再删文件夹。
type AssetLibraryItem struct {
	ID        uint64    `gorm:"primaryKey;index:idx_asset_library_user_folder,priority:4,sort:desc" json:"id"`                                             // 归属 ID
	UserID    uint64    `gorm:"not null;uniqueIndex:uk_asset_library_user_asset,priority:1;index:idx_asset_library_user_folder,priority:1" json:"user_id"` // 所属用户
	AssetID   uint64    `gorm:"not null;uniqueIndex:uk_asset_library_user_asset,priority:2" json:"asset_id"`                                               // 素材 ID（assets.id）
	FolderID  *uint64   `gorm:"index:idx_asset_library_user_folder,priority:2" json:"folder_id"`                                                           // 所在文件夹；null 表示根目录
	CreatedAt time.Time `gorm:"index:idx_asset_library_user_folder,priority:3,sort:desc" json:"created_at"`                                                // 加入时间，库内按它倒序
}

// TableName 表名。
func (AssetLibraryItem) TableName() string { return "asset_library_items" }

// 个人资产库的上限与长度规则。
const (
	MaxAssetFolders       = 100 // 每个用户最多的文件夹数
	MaxAssetFolderNameLen = 30  // 文件夹名最多多少个字
)

// AssetFolderCount 是文件夹连同其中素材数，仓储联表统计的结果。
type AssetFolderCount struct {
	AssetFolder `gorm:"embedded"`
	ItemCount   int `json:"item_count"` // 文件夹里的素材数
}

// AssetFolderView 返回给前端的文件夹。
type AssetFolderView struct {
	ID        idcodec.ID `json:"id"`         // 文件夹 ID（十六进制串）
	Name      string     `json:"name"`       // 名称
	ItemCount int        `json:"item_count"` // 文件夹里的素材数
	CreatedAt time.Time  `json:"created_at"` // 创建时间
}

// AssetFolderListView 是文件夹列表（不分页，每用户最多 100 个）。
type AssetFolderListView struct {
	Items []AssetFolderView `json:"items"` // 全部文件夹
}

// AssetFolderRemovedView 是删除文件夹的结果。
type AssetFolderRemovedView struct {
	Removed int64 `json:"removed"` // 一并移出资产库的素材数
}

// AssetLibraryItemView 返回给前端的库条目。
type AssetLibraryItemView struct {
	ID        idcodec.ID `json:"id"`         // 归属 ID（十六进制串）
	Asset     *AssetView `json:"asset"`      // 素材
	FolderID  idcodec.ID `json:"folder_id"`  // 所在文件夹；根目录为 null
	CreatedAt time.Time  `json:"created_at"` // 加入时间
}

// AssetLibraryItemPage 是一页库条目；NextCursor 为空串表示没有更多。
type AssetLibraryItemPage struct {
	Items      []AssetLibraryItemView `json:"items"`       // 本页条目
	NextCursor string                 `json:"next_cursor"` // 下一页游标
}

// GeneratedCanvas 是生成历史条目的来源画布；画布已被删除时 Deleted 为 true 且标题为空。
type GeneratedCanvas struct {
	ID      idcodec.ID `json:"id"`                // 画布 ID（十六进制串）
	Title   string     `json:"title"`             // 画布标题
	Deleted bool       `json:"deleted,omitempty"` // 画布已被删除
}

// GeneratedItemView 是生成历史的一张卡：一个生成产物素材连同提示词、模型名与来源。
type GeneratedItemView struct {
	Asset         *AssetView       `json:"asset"`           // 素材
	Prompt        string           `json:"prompt"`          // 完整提示词
	ModelLabel    string           `json:"model_label"`     // 模型展示名
	Canvas        *GeneratedCanvas `json:"canvas"`          // 来源画布；首页对话为 null
	CreatedAt     time.Time        `json:"created_at"`      // 生成时间
	LibraryItemID idcodec.ID       `json:"library_item_id"` // 已在资产库时的归属 ID，否则为 null
}

// GeneratedItemPage 是一页生成历史；NextCursor 为空串表示没有更多。
type GeneratedItemPage struct {
	Items      []GeneratedItemView `json:"items"`       // 本页条目
	NextCursor string              `json:"next_cursor"` // 下一页游标
}

// GeneratedAssetRow 是生成历史查询的一行：素材 + 它所属任务里和展示有关的字段（任务不存在时这些字段为空）。
type GeneratedAssetRow struct {
	Asset           `gorm:"embedded"`
	CanvasProjectID *uint64 `gorm:"column:task_canvas_project_id"` // 任务所属画布；null 表示首页对话
	InputJSON       []byte  `gorm:"column:task_input_json"`        // 任务输入（取提示词）
	ConfigSnapshot  []byte  `gorm:"column:task_config_snapshot"`   // 任务创建时的配置快照（取模型展示名）
	TaskModelKey    string  `gorm:"column:task_model_key"`         // 任务的模型 key（展示名缺失时的兜底）
}

// CanvasBrief 是画布的简要信息，Deleted 表示已被软删除。
type CanvasBrief struct {
	ID      uint64 // 画布主键
	Title   string // 画布标题
	Deleted bool   // 是否已删除
}

// CreateAssetFolderReq 新建文件夹。名称的长度与空白校验在服务里做（去首尾空白后 1–30 字）。
type CreateAssetFolderReq struct {
	Name string `json:"name" label:"文件夹名称"` // 名称
}

// RenameAssetFolderReq 重命名文件夹。
type RenameAssetFolderReq struct {
	Name string `json:"name" label:"文件夹名称"` // 新名称
}

// AddAssetLibraryItemReq 把素材加入资产库；FolderID 不传或为 null 表示根目录。
type AddAssetLibraryItemReq struct {
	AssetID  uint64     `json:"asset_id" binding:"required,min=1" label:"素材"` // 素材 ID
	FolderID idcodec.ID `json:"folder_id" label:"文件夹"`                        // 目标文件夹（十六进制串）
}

// MoveAssetLibraryItemReq 移动库条目；FolderID 为 null 表示移到根目录。
type MoveAssetLibraryItemReq struct {
	FolderID idcodec.ID `json:"folder_id" label:"文件夹"` // 目标文件夹（十六进制串）
}

// ListAssetLibraryItemsReq 是库条目列表的查询参数。
type ListAssetLibraryItemsReq struct {
	FolderID string `form:"folder_id" binding:"max=64" label:"文件夹"`                        // root（默认）/ 十六进制 id / all
	Kind     string `form:"kind" binding:"omitempty,oneof=image video audio" label:"素材类型"` // 不传不过滤
	Q        string `form:"q" binding:"max=100" label:"搜索词"`                               // 文件名包含匹配
	Cursor   string `form:"cursor" binding:"max=128" label:"游标"`                           // 上一页返回的 next_cursor
	Limit    int    `form:"limit" binding:"omitempty,min=1,max=100" label:"每页条数"`          // 默认 40，最大 100
}

// ListGeneratedAssetsReq 是生成历史的查询参数。
type ListGeneratedAssetsReq struct {
	Kind        string `form:"kind" binding:"required,oneof=image video audio" label:"素材类型"`        // 必填
	Source      string `form:"source" binding:"omitempty,oneof=all conversation canvas" label:"来源"` // all（默认）/ conversation / canvas
	CanvasID    string `form:"canvas_id" binding:"max=64" label:"画布"`                               // 十六进制串，指定后只看这张画布
	CreatedFrom string `form:"created_from" binding:"max=64" label:"起始时间"`                          // RFC3339
	Order       string `form:"order" binding:"omitempty,oneof=desc asc" label:"排序"`                 // desc（默认）/ asc
	Q           string `form:"q" binding:"max=100" label:"搜索词"`                                     // 提示词包含匹配
	Cursor      string `form:"cursor" binding:"max=128" label:"游标"`                                 // 上一页返回的 next_cursor
	Limit       int    `form:"limit" binding:"omitempty,min=1,max=100" label:"每页条数"`                // 默认 40，最大 100
}
