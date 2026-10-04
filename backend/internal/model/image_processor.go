package model

import "time"

// 图片处理服务状态。
const (
	ProcessorDraft     = "draft"     // 草稿：从未发布
	ProcessorPublished = "published" // 已发布：正在为所绑定存储里的素材生成缩略图 / 封面
	ProcessorDisabled  = "disabled"  // 已停用：该存储回退原图 / 占位
)

// ImageProcessor 图片处理服务：一行绑定一套存储，决定那套存储里的素材用哪家云厂商生成缩略图和视频封面。
// 凭证不在这里：腾讯云、阿里云私有读桶的签名直接用存储配置里的 AccessKey，Cloudflare 公开读不需要密钥。
type ImageProcessor struct {
	ID               uint64    `gorm:"primaryKey" json:"id"`                                                                                   // 处理服务 ID
	Name             string    `gorm:"size:64;not null;uniqueIndex" json:"name"`                                                               // 显示名
	Vendor           string    `gorm:"size:24;not null" json:"vendor"`                                                                         // cloudflare / tencent_ci / aliyun_oss_img
	StorageID        uint64    `gorm:"not null;index;uniqueIndex:uidx_image_processor_published,where:status = 'published'" json:"storage_id"` // 绑定的存储；同一存储最多一个已发布的
	Status           string    `gorm:"size:16;not null;default:'draft'" json:"status"`                                                         // draft / published / disabled
	Config           JSONText  `gorm:"type:json;not null" json:"-"`                                                                            // 工作配置（草稿）
	PublishedConfig  JSONText  `gorm:"type:json" json:"-"`                                                                                     // 线上正在用的配置快照，从未发布为空
	PublishedVersion int       `gorm:"not null;default:0" json:"published_version"`                                                            // 线上版本号，从未发布为 0
	Version          int       `gorm:"not null;default:1" json:"version"`                                                                      // 乐观锁；每次保存草稿 +1
	CheckResult      JSONText  `gorm:"type:json" json:"-"`                                                                                     // 最近一次校验与试跑的结果
	CreatedBy        uint64    `gorm:"not null;default:0" json:"created_by"`                                                                   // 创建人
	UpdatedBy        uint64    `gorm:"not null;default:0" json:"updated_by"`                                                                   // 最后修改人
	CreatedAt        time.Time `json:"created_at"`                                                                                             // 创建时间
	UpdatedAt        time.Time `json:"updated_at"`                                                                                             // 更新时间
}

// TableName 返回表名。
func (ImageProcessor) TableName() string { return "image_processors" }

// ImageProcessorVersion 处理服务的发布历史：每次发布写入一行配置快照，回滚时按它恢复。
type ImageProcessorVersion struct {
	ID          uint64    `gorm:"primaryKey" json:"id"`                                                  // 主键
	ProcessorID uint64    `gorm:"not null;uniqueIndex:uidx_image_processor_version" json:"processor_id"` // 所属处理服务
	Version     int       `gorm:"not null;uniqueIndex:uidx_image_processor_version" json:"version"`      // 发布版本号，同一处理服务内单调递增
	Config      JSONText  `gorm:"type:json;not null" json:"-"`                                           // 发布时的配置快照
	TrialResult JSONText  `gorm:"type:json" json:"-"`                                                    // 发布前的校验与试跑结果
	PublishedBy uint64    `gorm:"not null;default:0" json:"published_by"`                                // 发布人
	PublishedAt time.Time `json:"published_at"`                                                          // 发布时间
}

// TableName 返回表名。
func (ImageProcessorVersion) TableName() string { return "image_processor_versions" }
