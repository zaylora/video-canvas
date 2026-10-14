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
