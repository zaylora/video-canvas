package service

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"video-canvas/internal/model"
	"video-canvas/pkg/version"
)

const (
	// updateCacheTTL 是检查结果的缓存时间：GitHub 匿名接口每个 IP 每小时只有 60 次额度。
	updateCacheTTL = time.Hour
	// updateForceInterval 内重复的「重新检查」复用上次结果，防止连点刷掉额度。
	updateForceInterval = 30 * time.Second
	// updateRetryAfterFailure 是请求失败后的冷却时间：GitHub 不可达时，不让每次打开后台都卡在超时上。
	updateRetryAfterFailure = 5 * time.Minute
)

// ReleaseFetcher 读取最新的正式发布版本（真实实现是 release.GitHubClient）。
type ReleaseFetcher interface {
	Latest(ctx context.Context) (*model.ReleaseInfo, error)
}

// MigrationStatusReader 读取数据库迁移的执行状态（真实实现是 repository.MigrationStatusRepository）。
type MigrationStatusReader interface {
	MigrationStatus(ctx context.Context) ([]model.MigrationFileView, error)
}

// SystemUpdateOptions 是 SystemUpdateService 的依赖与配置。
type SystemUpdateOptions struct {
	CurrentVersion string // 当前程序版本，一般是 version.Version
	CheckEnabled   bool   // update.check_enabled
	Releases       ReleaseFetcher
	Migrations     MigrationStatusReader
	Now            func() time.Time // 测试注入时钟；nil 时用 time.Now
}

// SystemUpdateService 提供后台版本面板的数据：当前版本、新版本检查、迁移状态。
// 检查结果缓存在进程内存里；多实例各自缓存，最坏多请求几次 GitHub，不影响正确性。
type SystemUpdateService struct {
	opts SystemUpdateOptions

	mu        sync.Mutex // 同时只发一个 GitHub 请求，其他请求等它的结果
	cached    *model.UpdateCheckView
	fetchedAt time.Time // 上次请求 GitHub 的时间（不论成败）
	lastErr   string    // 上次请求失败的原因；成功后清空
}

// NewSystemUpdateService 创建版本服务。
func NewSystemUpdateService(opts SystemUpdateOptions) *SystemUpdateService {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &SystemUpdateService{opts: opts}
}

// Version 返回当前程序的构建信息，以及库里最后一个已执行的迁移。
func (s *SystemUpdateService) Version(ctx context.Context) (*model.SystemVersionView, error) {
	files, err := s.opts.Migrations.MigrationStatus(ctx)
	if err != nil {
		return nil, err
	}
	info := version.Get()
	v := &model.SystemVersionView{
		Version:   s.opts.CurrentVersion,
		GitCommit: info.GitCommit,
		BuildTime: info.BuildTime,
		GoVersion: info.GoVersion,
	}
	// 文件按名字排好序，最后一个已执行的就是当前迁移进度
	for _, f := range files {
		if f.Applied && !f.Unknown {
			v.LatestMigration = f.Filename
		}
	}
	return v, nil
}

// Migrations 返回每个迁移文件的执行状态。
func (s *SystemUpdateService) Migrations(ctx context.Context) ([]model.MigrationFileView, error) {
	return s.opts.Migrations.MigrationStatus(ctx)
}

// CheckUpdate 检查有没有新版本。永不返回错误：检查失败时把原因放进 Warning，有旧结果就带上旧结果。
//
// 逻辑：
//  1. 关闭了检查、或当前是开发构建（版本号不是 x.y.z），直接返回，不请求 GitHub；
//  2. 决定要不要请求：没有缓存、缓存超过 1 小时、或 force 且距上次请求超过 30 秒时请求；
//     但上次请求失败后 5 分钟内不重试；
//  3. 请求成功就更新缓存；失败就记下原因，返回旧缓存（没有旧缓存时只返回当前版本）。
func (s *SystemUpdateService) CheckUpdate(ctx context.Context, force bool) *model.UpdateCheckView {
	current := strings.TrimPrefix(s.opts.CurrentVersion, "v")
	if !s.opts.CheckEnabled {
		return &model.UpdateCheckView{Enabled: false, Current: current}
	}
	if _, ok := parseSemver(current); !ok {
		return &model.UpdateCheckView{Enabled: true, DevBuild: true, Current: current}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.opts.Now()
	if s.shouldFetch(now, force) {
		s.fetchedAt = now
		rel, err := s.opts.Releases.Latest(ctx)
		if err != nil {
			s.lastErr = "检查更新失败：" + err.Error()
		} else {
			s.lastErr = ""
			s.cached = buildUpdateView(current, rel, now)
		}
	}

	if s.cached == nil {
		return &model.UpdateCheckView{Enabled: true, Current: current, Warning: s.lastErr}
	}
	out := *s.cached
	out.Warning = s.lastErr
	return &out
}

// shouldFetch 判断这次要不要真的请求 GitHub，调用方需持有 s.mu。
func (s *SystemUpdateService) shouldFetch(now time.Time, force bool) bool {
	if s.fetchedAt.IsZero() {
		return true
	}
	since := now.Sub(s.fetchedAt)
	if s.lastErr != "" && since < updateRetryAfterFailure {
		return false
	}
	if force {
		return since >= updateForceInterval
	}
	return s.cached == nil || since >= updateCacheTTL
}

func buildUpdateView(current string, rel *model.ReleaseInfo, now time.Time) *model.UpdateCheckView {
	latest := strings.TrimPrefix(rel.Version, "v")
	published, checked := rel.PublishedAt, now
	return &model.UpdateCheckView{
		Enabled:      true,
		Current:      current,
		Latest:       latest,
		HasUpdate:    semverGreater(latest, current),
		ReleaseNotes: rel.Notes,
		ReleaseURL:   rel.URL,
		PublishedAt:  &published,
		CheckedAt:    &checked,
	}
}

// parseSemver 解析 x.y.z（允许 -后缀，比较时忽略后缀）；不是这种格式返回 false。
func parseSemver(v string) ([3]int, bool) {
	var out [3]int
	core, _, _ := strings.Cut(strings.TrimPrefix(v, "v"), "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// semverGreater 判断 a 是否比 b 新；任一方解析失败时返回 false。
func semverGreater(a, b string) bool {
	va, okA := parseSemver(a)
	vb, okB := parseSemver(b)
	if !okA || !okB {
		return false
	}
	for i := range va {
		if va[i] != vb[i] {
			return va[i] > vb[i]
		}
	}
	return false
}
