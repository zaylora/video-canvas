package model

import "time"

// ReleaseInfo 是一个正式发布的版本（来自 GitHub Releases，不含预发布与草稿）。
type ReleaseInfo struct {
	Version     string    // 去掉 v 前缀的版本号，如 0.4.0
	Notes       string    // Release 正文（即 CHANGELOG 对应段落，Markdown）
	URL         string    // Release 页面地址
	PublishedAt time.Time // 发布时间
}

// SystemVersionView 是后台版本面板的「当前版本」部分（GET /admin/system/version）。
type SystemVersionView struct {
	Version         string `json:"version"`
	GitCommit       string `json:"git_commit"`
	BuildTime       string `json:"build_time"`
	GoVersion       string `json:"go_version"`
	LatestMigration string `json:"latest_migration"` // 库里最后一个已执行的迁移文件；还没有迁移记录时为空
}

// UpdateCheckView 是检查新版本的结果（GET /admin/system/updates）。检查失败不报错，原因写在 Warning。
type UpdateCheckView struct {
	Enabled      bool       `json:"enabled"`   // 是否开启了检查更新（update.check_enabled）
	DevBuild     bool       `json:"dev_build"` // 当前是开发构建（版本号不是 x.y.z），不检查
	Current      string     `json:"current"`
	Latest       string     `json:"latest,omitempty"`
	HasUpdate    bool       `json:"has_update"`
	ReleaseNotes string     `json:"release_notes,omitempty"`
	ReleaseURL   string     `json:"release_url,omitempty"`
	PublishedAt  *time.Time `json:"published_at,omitempty"`
	CheckedAt    *time.Time `json:"checked_at,omitempty"` // 结果实际从 GitHub 取回的时间；命中缓存时是缓存的时间
	Warning      string     `json:"warning,omitempty"`    // 检查失败的原因；有旧结果时仍返回旧结果
}

// MigrationFileView 是一个数据库迁移文件的执行状态（GET /admin/system/migrations）。
type MigrationFileView struct {
	Filename   string     `json:"filename"`
	Applied    bool       `json:"applied"`
	AppliedAt  *time.Time `json:"applied_at,omitempty"`
	AppVersion string     `json:"app_version,omitempty"` // 执行它的程序版本
	DurationMs int64      `json:"duration_ms"`
	Modified   bool       `json:"modified"` // 已执行，但文件内容和执行时不一样
	Unknown    bool       `json:"unknown"`  // 库里有记录，程序里没有（库比程序新）
}

// UpgradeRunStatus 是 update.sh 写的进度文件（--status-file），updater 原样转给 backend。
type UpgradeRunStatus struct {
	Target    string `json:"target"`
	Previous  string `json:"previous"`
	Phase     string `json:"phase"`   // prepare / compose / pull / backup / recreate / health / rollback / finish
	State     string `json:"state"`   // 见 UpgradeState* 常量
	Message   string `json:"message"` // 给管理员看的说明；失败时是原因和处理建议
	Backup    string `json:"backup"`  // 这次升级前的数据库备份路径（部署目录内），没有备份时为空
	UpdatedAt string `json:"updated_at"`
}

// update.sh 运行状态。
const (
	UpgradeStateRunning           = "running"
	UpgradeStateSucceeded         = "succeeded"
	UpgradeStateFailed            = "failed"                 // 没改动运行中的服务（拉取失败、备份失败等）
	UpgradeStateFailedRolledBack  = "failed_rolled_back"     // 新版本没起来，已退回旧版本且服务正常
	UpgradeStateFailedNeedsAction = "failed_needs_attention" // 退回后旧版本也没起来，要人工处理
)

// UpgradeBackup 是部署目录 backups/ 下的一份数据库备份。
type UpgradeBackup struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

// UpdaterStatus 是 updater 容器 GET /status 的返回。
type UpdaterStatus struct {
	Running  bool              `json:"running"`   // update.sh 正在执行
	ImageTag string            `json:"image_tag"` // 部署目录 .env 里的 IMAGE_TAG
	Last     *UpgradeRunStatus `json:"last"`      // 最近一次运行的状态；从没运行过为 nil
	Backups  []UpgradeBackup   `json:"backups"`
	LogTail  string            `json:"log_tail"` // 最近一次运行输出的最后若干行，失败时给管理员排查
}

// UpgradeInfoView 是后台升级面板的数据（GET /admin/system/upgrade）。
type UpgradeInfoView struct {
	Available         bool              `json:"available"`          // updater 已启用且连得上
	UnavailableReason string            `json:"unavailable_reason"` // 不可用的原因；可用时为空
	ImageTag          string            `json:"image_tag"`
	UsesLatest        bool              `json:"uses_latest"` // IMAGE_TAG=latest，不能一键升级
	Running           bool              `json:"running"`
	Last              *UpgradeRunStatus `json:"last"`
	Backups           []UpgradeBackup   `json:"backups"`
	LogTail           string            `json:"log_tail"`
	ActiveTasks       int64             `json:"active_tasks"`       // 全站进行中的生成任务数，升级会中断它们
	RollbackTarget    string            `json:"rollback_target"`    // 可以回退到的版本（最近一次成功升级前的版本）；不能回退时为空
	CurrentMigrations []string          `json:"current_migrations"` // 当前版本执行过的数据库迁移，回退时提示这些变更不会撤销
}

// UpgradeReq 是升级 / 回退请求。
type UpgradeReq struct {
	Target string `json:"target" binding:"required" label:"目标版本"`
}
