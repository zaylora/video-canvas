package model

import "time"

// 配置目标类型与 revision 状态。
const (
	ConfigTargetProvider = "provider" // 平台协议配置
	ConfigTargetModel    = "model"    // 模型 / 工作流配置

	RevisionDraft     = "draft"     // 草稿，可编辑
	RevisionPublished = "published" // 已发布，不可变
	RevisionArchived  = "archived"  // 已归档
)

// AIProvider 平台协议配置的指针表：正文存在 ai_config_revisions 里。
type AIProvider struct {
	Key                 string    `gorm:"primaryKey;size:64" json:"key"` // 平台 key
	Name                string    `gorm:"size:128;not null" json:"name"` // 平台名称
	PublishedRevisionID *uint64   `json:"published_revision_id"`         // 为空表示还没发布过
	UpdatedAt           time.Time `json:"updated_at"`                    // 更新时间
}

func (AIProvider) TableName() string { return "ai_providers" }

// AIModel 模型 / 工作流配置的指针表。
type AIModel struct {
	Key                 string    `gorm:"primaryKey;size:128" json:"key"`             // 模型 key
	Kind                string    `gorm:"size:16;not null;index" json:"kind"`         // video / image / audio
	ProviderKey         string    `gorm:"size:64;not null;index" json:"provider_key"` // 所属平台 key
	Enabled             bool      `gorm:"not null;default:false" json:"enabled"`      // 是否启用（对用户可见）
	Sort                int       `gorm:"not null;default:100" json:"sort"`           // 排序，越小越靠前
	PublishedRevisionID *uint64   `json:"published_revision_id"`                      // 当前发布的配置版本，为空表示还没发布过
	UpdatedAt           time.Time `json:"updated_at"`                                 // 更新时间
}

func (AIModel) TableName() string { return "ai_models" }

// AIConfigRevision 配置版本（正文用 json 而不是 jsonb：jsonb 会重排对象键，input_schema 的书写顺序就是前端的渲染顺序）：编辑写 draft，发布后不可变，回滚只是把指针改回旧 revision。
type AIConfigRevision struct {
	ID         uint64    `gorm:"primaryKey" json:"id"`                                                        // 版本 ID
	Target     string    `gorm:"size:16;not null;uniqueIndex:uk_rev_target_no,priority:1" json:"target"`      // provider / model
	TargetKey  string    `gorm:"size:128;not null;uniqueIndex:uk_rev_target_no,priority:2" json:"target_key"` // 目标 key（平台 key 或模型 key）
	RevisionNo int       `gorm:"not null;uniqueIndex:uk_rev_target_no,priority:3" json:"revision_no"`         // 同一目标下的版本序号
	BodyJSON   JSONText  `gorm:"type:json;not null" json:"body_json"`                                         // 配置正文
	Status     string    `gorm:"size:16;not null" json:"status"`                                              // draft / published / archived
	CreatedBy  uint64    `gorm:"not null;default:0" json:"created_by"`                                        // 创建人
	Note       string    `gorm:"size:255;not null;default:''" json:"note"`                                    // 版本备注
	CreatedAt  time.Time `json:"created_at"`                                                                  // 创建时间
}

func (AIConfigRevision) TableName() string { return "ai_config_revisions" }

// AISecret 平台凭证：AES-256-GCM 密文，只写不读，永远不下发。
type AISecret struct {
	Name       string    `gorm:"primaryKey;size:128" json:"name"`       // 凭证名称
	Ciphertext []byte    `gorm:"not null" json:"-"`                     // AES-256-GCM 密文
	Nonce      []byte    `gorm:"not null" json:"-"`                     // GCM 随机数
	KeyVersion int       `gorm:"not null;default:1" json:"key_version"` // 加密主密钥版本
	UpdatedBy  uint64    `gorm:"not null;default:0" json:"updated_by"`  // 最后修改人
	UpdatedAt  time.Time `json:"updated_at"`                            // 更新时间
}

func (AISecret) TableName() string { return "ai_secrets" }
