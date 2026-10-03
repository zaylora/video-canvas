package model

import (
	"strconv"
	"time"
)

// StorageConfig 素材存储配置：一行一套存储，素材记录自己写在哪一套（assets.storage_id）。
// 密钥不在这张表里：加密后存 ai_secrets，名字固定为 storage:<id>（见 StorageSecretName）。
type StorageConfig struct {
	ID             uint64     `gorm:"primaryKey" json:"id"`                                                                       // 存储 ID
	Name           string     `gorm:"size:64;not null;uniqueIndex" json:"name"`                                                   // 显示名
	Provider       string     `gorm:"size:16;not null" json:"provider"`                                                           // local / aliyun_oss / tencent_cos / s3 / r2
	Builtin        bool       `gorm:"not null;default:false" json:"builtin"`                                                      // 内置本地磁盘，配置来自 YAML，不可编辑 / 删除
	AccountID      string     `gorm:"size:64;not null;default:''" json:"account_id"`                                              // 仅 R2：Cloudflare Account ID
	Endpoint       string     `gorm:"size:255;not null;default:''" json:"endpoint"`                                               // 服务地址（OSS / COS / R2 由预设推导）
	Region         string     `gorm:"size:64;not null;default:''" json:"region"`                                                  // 地域
	Bucket         string     `gorm:"size:128;not null;default:''" json:"bucket"`                                                 // 桶名
	PathPrefix     string     `gorm:"size:255;not null;default:''" json:"path_prefix"`                                            // 桶内路径前缀
	Addressing     string     `gorm:"size:8;not null;default:'auto'" json:"addressing"`                                           // auto / virtual / path
	UseSSL         bool       `gorm:"not null;default:true" json:"use_ssl"`                                                       // 是否 HTTPS
	AccessKeyID    string     `gorm:"size:128;not null;default:''" json:"-"`                                                      // AccessKey ID（非机密，对外脱敏后展示）
	PublicBaseURL  string     `gorm:"size:512;not null;default:''" json:"public_base_url"`                                        // 公开桶 / CDN 前缀，空表示私有桶走签名
	SignedTTLSec   int        `gorm:"not null;default:3600" json:"signed_ttl_sec"`                                                // 私有桶签名地址有效期（秒）
	DirectUpload   bool       `gorm:"not null;default:false" json:"direct_upload"`                                                // 允许浏览器直传
	IsDefault      bool       `gorm:"not null;default:false;uniqueIndex:uidx_storage_default,where:is_default" json:"is_default"` // 默认存储，全表最多一条
	LastCheckAt    *time.Time `json:"last_check_at"`                                                                              // 最近一次测试连接的时间
	LastCheckOK    *bool      `json:"last_check_ok"`                                                                              // 最近一次测试是否通过，nil 表示没测过
	LastCheckError string     `gorm:"type:text;not null;default:''" json:"last_check_error"`                                      // 最近一次失败原因
	Version        int        `gorm:"not null;default:1" json:"version"`                                                          // 乐观锁 + 客户端缓存失效；改配置才 +1，记录测试结果不变
	CreatedBy      uint64     `gorm:"not null;default:0" json:"created_by"`                                                       // 创建人
	UpdatedBy      uint64     `gorm:"not null;default:0" json:"updated_by"`                                                       // 最后修改人
	CreatedAt      time.Time  `json:"created_at"`                                                                                 // 创建时间
	UpdatedAt      time.Time  `json:"updated_at"`                                                                                 // 更新时间
}

// TableName 返回表名。
func (StorageConfig) TableName() string { return "storage_configs" }

// StorageSecretName 返回存储密钥在 ai_secrets 里的名字。
func StorageSecretName(id uint64) string { return "storage:" + strconv.FormatUint(id, 10) }

// AssetUploadIntent 浏览器直传的上传意图：申请时记下写到哪套存储，之后 complete 一律按它找存储，
// 不看当前默认存储，所以上传中途切换默认也不会出现“文件在 A、记录指向 B”。
type AssetUploadIntent struct {
	ID           uint64     `gorm:"primaryKey" json:"id"`                          // 意图 ID
	UserID       uint64     `gorm:"not null;index" json:"user_id"`                 // 申请人
	StorageID    uint64     `gorm:"not null;index" json:"storage_id"`              // 写入的存储
	StorageKey   string     `gorm:"size:512;not null;uniqueIndex" json:"-"`        // 预分配的对象 key
	FileName     string     `gorm:"size:255;not null;default:''" json:"file_name"` // 原始文件名
	MimeType     string     `gorm:"size:128;not null;default:''" json:"mime_type"` // 声明的类型
	Kind         string     `gorm:"size:16;not null;default:''" json:"kind"`       // image / video / audio
	DeclaredSize int64      `gorm:"not null;default:0" json:"declared_size"`       // 声明的大小
	ExpiresAt    time.Time  `gorm:"not null;index" json:"expires_at"`              // 过期时间，过期未完成的由清理任务删除对象
	CompletedAt  *time.Time `json:"completed_at"`                                  // 完成登记的时间
	CreatedAt    time.Time  `json:"created_at"`                                    // 创建时间
}

// TableName 返回表名。
func (AssetUploadIntent) TableName() string { return "asset_upload_intents" }
