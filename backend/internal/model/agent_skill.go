package model

import (
	"time"

	"gorm.io/datatypes"
)

// 技能版本的状态。仓储层的查询默认只看 ready，pending 与 deleting 对任何读路径都不可见。
const (
	SkillVersionPending  = "pending"  // 已分配版本号和对象 key，对象可能还没写好
	SkillVersionReady    = "ready"    // 对象已写好、事务已提交
	SkillVersionDeleting = "deleting" // 已删除，等清理任务删掉对象后硬删这一行
)

// 技能管理的审计动作和目标类型。
const (
	AdminAuditSkillImport        = "agent_skill.import"
	AdminAuditSkillEnable        = "agent_skill.enable"
	AdminAuditSkillDisable       = "agent_skill.disable"
	AdminAuditSkillActiveVersion = "agent_skill.activate_version"
	AdminAuditSkillRename        = "agent_skill.rename"
	AdminAuditSkillDeleteVersion = "agent_skill.delete_version"
	AdminAuditSkillDelete        = "agent_skill.delete"

	AdminAuditTargetAgentSkill = "agent_skill"
)

// AgentSkill 是后台导入的 Agent 技能。内置技能随二进制发布，不在这张表里。
type AgentSkill struct {
	ID            uint64    `gorm:"primaryKey" json:"id"`                     // 技能 id（审计 target_id）
	Name          string    `gorm:"size:64;not null;uniqueIndex" json:"name"` // frontmatter 的 name，不可变；也是 skill_read 参数和 skill:key
	Title         string    `gorm:"size:128;not null" json:"title"`           // 显示名，可改，不产生版本
	Enabled       bool      `gorm:"not null;default:false" json:"enabled"`    // 是否出现在目录和 @ 弹层
	ActiveVersion *int      `json:"active_version"`                           // 生效版本号；首个版本入库时设为 1
	LatestVersion int       `gorm:"not null;default:0" json:"latest_version"` // 已分配的最大版本号，只增不减（删除后不复用）
	CreatedBy     uint64    `gorm:"not null;default:0" json:"created_by"`     // 首次导入人
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// TableName 返回表名。
func (AgentSkill) TableName() string { return "agent_skills" }

// AgentSkillVersion 是技能的一个不可变版本：入库后只可整行删除。
type AgentSkillVersion struct {
	ID                uint64         `gorm:"primaryKey" json:"id"`
	SkillID           uint64         `gorm:"not null;uniqueIndex:idx_skill_version,priority:1" json:"skill_id"`
	Version           int            `gorm:"not null;uniqueIndex:idx_skill_version,priority:2" json:"version"`
	State             string         `gorm:"size:16;not null;index" json:"state"` // pending / ready / deleting
	SHA256            string         `gorm:"size:64;not null;index" json:"sha256"`
	Description       string         `gorm:"size:1024;not null" json:"description"`
	FrontmatterJSON   datatypes.JSON `gorm:"type:jsonb" json:"frontmatter"`
	UnsupportedFields datatypes.JSON `gorm:"type:jsonb" json:"unsupported_fields"`
	BodyText          string         `gorm:"type:text;not null" json:"-"` // SKILL.md 正文，skill_read 直接用；列表不读
	FilesJSON         datatypes.JSON `gorm:"type:jsonb" json:"files"`     // [{path,size,sha256,kind,lang,text}]
	FileCount         int            `gorm:"not null;default:0" json:"file_count"`
	TotalBytes        int64          `gorm:"not null;default:0" json:"total_bytes"`
	HasScripts        bool           `gorm:"not null;default:false" json:"has_scripts"`
	IssuesJSON        datatypes.JSON `gorm:"type:jsonb" json:"issues"`
	StorageID         uint64         `gorm:"not null;default:0" json:"-"`
	PackageKey        string         `gorm:"size:255;not null" json:"-"` // agent-skills/<name>/<uuid>.zip，不用 sha256 做 key，避免公开桶下被推测
	CreatedBy         uint64         `gorm:"not null;default:0" json:"created_by"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"-"` // 状态流转时间，pending/deleting 的清理按它判断超时
}

// TableName 返回表名。
func (AgentSkillVersion) TableName() string { return "agent_skill_versions" }

// AgentSkillImport 是两步导入的暂存：第一步上传并预检的产物，确认后才入库为版本。
type AgentSkillImport struct {
	ID         string         `gorm:"primaryKey;size:36" json:"id"`   // uuid
	UserID     uint64         `gorm:"not null;index" json:"user_id"`  // 上传人，只能本人确认
	ResultJSON datatypes.JSON `gorm:"type:jsonb;not null" json:"-"`   // 预检结果
	SHA256     string         `gorm:"size:64;not null" json:"sha256"` // 规范化整包哈希
	StorageID  uint64         `gorm:"not null;default:0" json:"-"`
	PackageKey string         `gorm:"size:255;not null" json:"-"` // agent-skills/_staging/<id>.zip
	ExpiresAt  time.Time      `gorm:"not null;index" json:"expires_at"`
	CreatedAt  time.Time      `json:"created_at"`
}

// TableName 返回表名。
func (AgentSkillImport) TableName() string { return "agent_skill_imports" }
