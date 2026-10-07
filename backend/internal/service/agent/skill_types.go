package agent

import (
	"context"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
	"video-canvas/internal/storage"
)

// SkillRepo 是技能管理依赖的数据访问，由 repository.AgentSkillRepository 实现。
type SkillRepo interface {
	// ListSkills 返回全部导入技能，按名字排序。
	ListSkills(ctx context.Context) ([]model.AgentSkill, error)
	// GetSkill 按名字取技能，不存在返回 repository.ErrNotFound。
	GetSkill(ctx context.Context, name string) (*model.AgentSkill, error)
	// ListVersions 返回技能的全部 ready 版本（不含正文、文件清单），版本号倒序。
	ListVersions(ctx context.Context, skillID uint64) ([]model.AgentSkillVersion, error)
	// ListActiveVersions 返回每个技能生效版本的头信息，按 skill_id 索引。
	ListActiveVersions(ctx context.Context) (map[uint64]model.AgentSkillVersion, error)
	// ListVersionNumbers 返回每个技能全部 ready 版本的版本号，按 skill_id 索引。
	ListVersionNumbers(ctx context.Context) (map[uint64][]int, error)
	// GetVersion 取某个 ready 版本（含正文与文件清单），不存在返回 repository.ErrNotFound。
	GetVersion(ctx context.Context, skillID uint64, version int) (*model.AgentSkillVersion, error)
	// GetVersionByID 按版本 id 取 ready 版本，不存在返回 repository.ErrNotFound。
	GetVersionByID(ctx context.Context, id uint64) (*model.AgentSkillVersion, error)
	// FindVersionBySHA 找技能下同内容的 ready 版本，没有返回 repository.ErrNotFound。
	FindVersionBySHA(ctx context.Context, skillID uint64, sha string) (*model.AgentSkillVersion, error)
	// ReserveVersion 分配版本号并写入 pending 版本行（技能不存在则一并创建）。
	ReserveVersion(ctx context.Context, in repository.ReserveSkillInput) (*model.AgentSkill, *model.AgentSkillVersion, error)
	// PromoteVersion 把 pending 版本置为 ready，首个版本同时成为生效版本。
	PromoteVersion(ctx context.Context, versionID uint64) (*model.AgentSkill, error)
	// DiscardVersion 删除一个 pending 版本行。
	DiscardVersion(ctx context.Context, versionID uint64) error
	// SetEnabled 改启用状态，技能不存在返回 repository.ErrNotFound。
	SetEnabled(ctx context.Context, skillID uint64, enabled bool) error
	// SetActiveVersion 改生效版本号，技能不存在返回 repository.ErrNotFound。
	SetActiveVersion(ctx context.Context, skillID uint64, version int) error
	// SetTitle 改显示名，技能不存在返回 repository.ErrNotFound。
	SetTitle(ctx context.Context, skillID uint64, title string) error
	// MarkVersionDeleting 把 ready 版本标为 deleting；不存在返回 ErrNotFound，是生效版本返回 ErrInUse。
	MarkVersionDeleting(ctx context.Context, skillID uint64, version int) error
	// DeleteSkill 删除技能行并把全部版本标为 deleting；启用中返回 ErrInUse。
	DeleteSkill(ctx context.Context, skillID uint64) error
	// ListEnabled 返回已启用且生效版本就绪的技能。
	ListEnabled(ctx context.Context) ([]repository.EnabledSkill, error)
	// CreateImport 登记导入暂存。
	CreateImport(ctx context.Context, imp *model.AgentSkillImport) error
	// GetImport 取导入暂存，不存在返回 repository.ErrNotFound。
	GetImport(ctx context.Context, id string) (*model.AgentSkillImport, error)
	// DeleteImport 删除暂存行，返回是否真的删到了（并发确认时只有一个调用方拿到 true）。
	DeleteImport(ctx context.Context, id string) (bool, error)
	// ListExpiredImports 返回已过期的暂存。
	ListExpiredImports(ctx context.Context, now time.Time, limit int) ([]model.AgentSkillImport, error)
	// ListGarbageVersions 返回待清理的 deleting 版本和超时的 pending 版本。
	ListGarbageVersions(ctx context.Context, pendingBefore time.Time, limit int) ([]model.AgentSkillVersion, error)
	// DeleteVersionRow 硬删一个版本行。
	DeleteVersionRow(ctx context.Context, id uint64) error
}

// SkillStores 是技能整包所在的对象存储：新包写默认存储，读取和删除按版本记录的 storage_id，换了默认存储也找得到旧包。
// 由 storage.Registry 实现。
type SkillStores interface {
	// Default 返回当前默认存储。
	Default(ctx context.Context) (*storage.Handle, error)
	// Get 按存储配置 id 取存储。
	Get(ctx context.Context, id uint64) (*storage.Handle, error)
}

// SkillIssue 是预检的一条问题；level 为 error（阻断确认）/ warn / info。
type SkillIssue struct {
	Level   string `json:"level"`
	Code    string `json:"code"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

// SkillFileView 是包内一个文件的清单项。
type SkillFileView struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256,omitempty"`
	Kind   string `json:"kind"` // skill / script / doc / asset / other
	Lang   string `json:"lang,omitempty"`
	Text   bool   `json:"text"` // 可预览
}

// 技能来源。
const (
	SkillSourceBuiltin  = "builtin"
	SkillSourceImported = "imported"
)

// SkillImportPlan 说明确认之后会发生什么。
type SkillImportPlan struct {
	Action        string `json:"action"`                   // create：新技能；new_version：已有技能的新版本；blocked：不能确认
	Version       int    `json:"version,omitempty"`        // 将产生的版本号
	ActiveVersion *int   `json:"active_version,omitempty"` // new_version 时当前生效的版本（不会变）
	Enabled       bool   `json:"enabled"`                  // new_version 时技能当前是否启用
}

// SkillImportView 是上传并预检后的结果：预检不通过也是正常结果，问题在 Issues 里。
type SkillImportView struct {
	ID          string          `json:"id"` // 暂存 id；空表示包完全不可读，没有暂存
	ExpiresAt   *time.Time      `json:"expires_at,omitempty"`
	Name        string          `json:"name"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Frontmatter map[string]any  `json:"frontmatter"`
	Unsupported []string        `json:"unsupported_fields"`
	Files       []SkillFileView `json:"files"`
	HasScripts  bool            `json:"has_scripts"`
	TotalBytes  int64           `json:"total_bytes"`
	Issues      []SkillIssue    `json:"issues"`
	CanConfirm  bool            `json:"can_confirm"`
	Plan        SkillImportPlan `json:"plan"`
}

// SkillItem 是管理页列表里的一行。
type SkillItem struct {
	Name              string    `json:"name"`
	Title             string    `json:"title"`
	Description       string    `json:"description"`
	Source            string    `json:"source"` // builtin / imported
	Readonly          bool      `json:"readonly"`
	Enabled           bool      `json:"enabled"`
	ActiveVersion     *int      `json:"active_version"`
	LatestVersion     int       `json:"latest_version"`
	PendingVersion    bool      `json:"pending_version"` // 有比生效版本更新的版本等待启用
	VersionCount      int       `json:"version_count"`
	FileCount         int       `json:"file_count"`
	TotalBytes        int64     `json:"total_bytes"`
	HasScripts        bool      `json:"has_scripts"`
	UnsupportedFields []string  `json:"unsupported_fields"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// SkillVersionHead 是版本列表里的一项。
type SkillVersionHead struct {
	Version           int       `json:"version"`
	Active            bool      `json:"active"`
	SHA256            string    `json:"sha256"`
	Description       string    `json:"description"`
	FileCount         int       `json:"file_count"`
	TotalBytes        int64     `json:"total_bytes"`
	HasScripts        bool      `json:"has_scripts"`
	UnsupportedFields []string  `json:"unsupported_fields"`
	CreatedBy         uint64    `json:"created_by"`
	CreatedAt         time.Time `json:"created_at"`
}

// SkillDetail 是技能详情：列表项加版本列表。
type SkillDetail struct {
	SkillItem
	Versions []SkillVersionHead `json:"versions"`
	Body     string             `json:"body,omitempty"` // 只有内置技能给正文（它没有版本可查看）；导入技能的正文在版本详情里
}

// SkillVersionView 是一个版本的完整信息。
type SkillVersionView struct {
	SkillVersionHead
	Name        string          `json:"name"`
	Frontmatter map[string]any  `json:"frontmatter"`
	Body        string          `json:"body"`
	Files       []SkillFileView `json:"files"`
	Issues      []SkillIssue    `json:"issues"`
}

// SkillFileContent 是读出的一个包内文件：文本给内容，二进制只给大小。
type SkillFileContent struct {
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	Binary    bool   `json:"binary"`
	Text      string `json:"text,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

// SkillDeleteCheck 是删除前的预检。
type SkillDeleteCheck struct {
	CanDelete    bool   `json:"can_delete"`
	Reason       string `json:"reason,omitempty"`
	VersionCount int    `json:"version_count"`
}

// SkillBrief 是 Agent 目录和 @ 弹层里的一项：只含已启用技能的名字、显示名、说明。
type SkillBrief struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Source      string `json:"source"`
}

// SkillContent 是 skill_read 读到的技能正文与资源清单。
type SkillContent struct {
	Name       string
	Source     string
	VersionID  uint64 // 导入技能的版本 id；运行期据此固定版本，内置为 0
	Version    int
	Body       string
	Files      []SkillFileView
	HasScripts bool
}
