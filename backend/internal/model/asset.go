package model

import "time"

// 素材来源。
const (
	AssetSourceUpload    = "upload"    // 用户上传
	AssetSourceGenerated = "generated" // 生成任务产物
)

// Asset 素材：用户上传或生成任务转存后的产物。只存 StorageKey，URL 由存储实现按策略生成。
type Asset struct {
	ID         uint64    `gorm:"primaryKey;index:idx_assets_user_source_kind_created,priority:5,sort:desc" json:"id"` // 素材 ID
	UserID     uint64    `gorm:"not null;index;index:idx_assets_user_source_kind_created,priority:1" json:"user_id"`  // 所属用户；后一个索引给生成历史按用户翻页
	Kind       string    `gorm:"size:16;not null;index:idx_assets_user_source_kind_created,priority:3" json:"kind"`   // image / video / audio
	StorageID  uint64    `gorm:"not null;default:0;index" json:"-"`                                                   // 所在存储（storage_configs.id）；读取、签名、删除都按它找存储
	StorageKey string    `gorm:"size:512;not null;uniqueIndex" json:"-"`                                              // 存储内的对象 key，全局唯一（/files/<key> 靠它反查素材）
	MimeType   string    `gorm:"size:128;not null" json:"mime_type"`                                                  // MIME 类型
	ByteSize   int64     `gorm:"not null;default:0" json:"byte_size"`                                                 // 文件大小（字节）
	Width      int       `json:"width"`                                                                               // 宽度（像素），图片/视频有效
	Height     int       `json:"height"`                                                                              // 高度（像素），图片/视频有效
	DurationMs int64     `json:"duration_ms"`                                                                         // 时长（毫秒），视频/音频有效
	Source     string    `gorm:"size:16;not null;index:idx_assets_user_source_kind_created,priority:2" json:"source"` // upload / generated
	TaskID     *uint64   `gorm:"index" json:"task_id"`                                                                // 生成产物对应的任务
	FileName   string    `gorm:"size:255;not null;default:''" json:"file_name"`                                       // 原始文件名
	Sha256     string    `gorm:"size:64;not null;default:'';index:idx_assets_sha256" json:"-"`                        // 文件内容的 SHA-256（小写十六进制）；空串表示还没算，导入导出按它复用已有文件
	CreatedAt  time.Time `gorm:"index:idx_assets_user_source_kind_created,priority:4,sort:desc" json:"created_at"`    // 创建时间
}

func (Asset) TableName() string { return "assets" }

// AssetView 返回给前端的素材信息，URL 由存储实现生成（可能是带过期时间的签名地址）。
type AssetView struct {
	ID         uint64 `json:"id"`          // 素材 ID
	Kind       string `json:"kind"`        // image / video / audio
	URL        string `json:"url"`         // 访问地址
	MimeType   string `json:"mime_type"`   // MIME 类型
	ByteSize   int64  `json:"byte_size"`   // 文件大小（字节）
	Width      int    `json:"width"`       // 宽度（像素）
	Height     int    `json:"height"`      // 高度（像素）
	DurationMs int64  `json:"duration_ms"` // 时长（毫秒）
	FileName   string `json:"file_name"`   // 原始文件名
}
