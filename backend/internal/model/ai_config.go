package model

import "time"

// 配置目标类型与配置行状态。协议插件设计之后只有模型配置走 ai_config_revisions（插件版本不可变、渠道直接改库）。
const (
	ConfigTargetModel = "model" // 模型 / 工作流配置

	// RevisionPublished 是配置行唯一的状态。模型没有版本管理：每个模型只有一行配置，保存就是原地覆盖，
	// 对用户是否可见只看 AIModel.Enabled。status / revision_no 列只为兼容已有库结构保留（revision_no 固定 1）。
	RevisionPublished = "published"
)

// AIModel 模型 / 工作流配置的指针表。
type AIModel struct {
	Key                 string    `gorm:"primaryKey;size:128" json:"key"`        // 模型 key
	Kind                string    `gorm:"size:16;not null;index" json:"kind"`    // video / image / audio
	Enabled             bool      `gorm:"not null;default:false" json:"enabled"` // 是否启用（对用户可见）
	Sort                int       `gorm:"not null;default:100" json:"sort"`      // 排序，越小越靠前
	PublishedRevisionID *uint64   `json:"published_revision_id"`                 // 配置正文所在行（ai_config_revisions.id）；为空表示还没保存过配置
	UpdatedAt           time.Time `json:"updated_at"`                            // 更新时间
}

func (AIModel) TableName() string { return "ai_models" }

// AIConfigRevision 模型配置正文（正文用 json 而不是 jsonb：jsonb 会重排对象键，input_schema 的书写顺序就是前端的渲染顺序）。
// 每个模型只有一行，保存时原地覆盖；表名与 status / revision_no 列是历史遗留，见 RevisionPublished。
type AIConfigRevision struct {
	ID         uint64    `gorm:"primaryKey" json:"id"`                                                        // 配置行 ID
	Target     string    `gorm:"size:16;not null;uniqueIndex:uk_rev_target_no,priority:1" json:"target"`      // model
	TargetKey  string    `gorm:"size:128;not null;uniqueIndex:uk_rev_target_no,priority:2" json:"target_key"` // 目标 key（模型 key）
	RevisionNo int       `gorm:"not null;uniqueIndex:uk_rev_target_no,priority:3" json:"revision_no"`         // 固定 1
	BodyJSON   JSONText  `gorm:"type:json;not null" json:"body_json"`                                         // 配置正文
	Status     string    `gorm:"size:16;not null" json:"status"`                                              // 固定 published
	CreatedBy  uint64    `gorm:"not null;default:0" json:"created_by"`                                        // 最后保存人
	Note       string    `gorm:"size:255;not null;default:''" json:"note"`                                    // 备注
	CreatedAt  time.Time `json:"created_at"`                                                                  // 创建时间
}

func (AIConfigRevision) TableName() string { return "ai_config_revisions" }

// AISecret 渠道凭证（名字固定为 channel:<渠道 key>）：AES-256-GCM 密文，只写不读，永远不下发。
type AISecret struct {
	Name       string    `gorm:"primaryKey;size:128" json:"name"`       // 凭证名称
	Ciphertext []byte    `gorm:"not null" json:"-"`                     // AES-256-GCM 密文
	Nonce      []byte    `gorm:"not null" json:"-"`                     // GCM 随机数
	KeyVersion int       `gorm:"not null;default:1" json:"key_version"` // 加密主密钥版本
	UpdatedBy  uint64    `gorm:"not null;default:0" json:"updated_by"`  // 最后修改人
	UpdatedAt  time.Time `json:"updated_at"`                            // 更新时间
}

func (AISecret) TableName() string { return "ai_secrets" }
