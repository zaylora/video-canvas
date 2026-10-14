package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/model"
	. "video-canvas/internal/service"
)

// fakeReleases 是 ReleaseFetcher 的 fake：返回预设的版本或错误，并记录调用次数。
type fakeReleases struct {
	rel   *model.ReleaseInfo
	err   error
	calls int
}

func (f *fakeReleases) Latest(context.Context) (*model.ReleaseInfo, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	r := *f.rel
	return &r, nil
}

type fakeMigrations struct {
	files []model.MigrationFileView
	err   error
}

func (f *fakeMigrations) MigrationStatus(context.Context) ([]model.MigrationFileView, error) {
	return f.files, f.err
}

// fakeClock 是可手动拨动的时钟。
type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time      { return c.t }
func (c *fakeClock) Add(d time.Duration) { c.t = c.t.Add(d) }
func newClock() *fakeClock               { return &fakeClock{t: time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)} }
func release(v string) *model.ReleaseInfo {
	return &model.ReleaseInfo{Version: v, Notes: "## [" + v + "]", URL: "https://example/" + v, PublishedAt: time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)}
}
func newSystem(current string, rel *fakeReleases, clock *fakeClock, enabled bool) *SystemUpdateService {
	return NewSystemUpdateService(SystemUpdateOptions{
		CurrentVersion: current, CheckEnabled: enabled, Releases: rel, Migrations: &fakeMigrations{}, Now: clock.Now,
	})
}

func TestSystemUpdateService_CheckUpdate(t *testing.T) {
	ctx := context.Background()

	t.Run("有更新：返回最新版本和更新日志", func(t *testing.T) {
		rel := &fakeReleases{rel: release("0.4.0")}
		v := newSystem("0.3.2", rel, newClock(), true).CheckUpdate(ctx, false)
		if !v.Enabled || !v.HasUpdate || v.Current != "0.3.2" || v.Latest != "0.4.0" || v.ReleaseNotes == "" || v.ReleaseURL == "" || v.PublishedAt == nil || v.CheckedAt == nil || v.Warning != "" {
			t.Fatalf("%+v", v)
		}
	})

	t.Run("按语义版本比较，不是按字符串", func(t *testing.T) {
		cases := []struct {
			current, latest string
			want            bool
		}{
			{"0.3.2", "0.10.0", true},
			{"0.10.0", "0.9.9", false},
			{"0.4.0", "0.4.0", false},
			{"1.0.0", "0.9.0", false},
			{"v0.3.2", "0.3.3", true}, // 带 v 前缀的构建也能比较
		}
		for _, c := range cases {
			v := newSystem(c.current, &fakeReleases{rel: release(c.latest)}, newClock(), true).CheckUpdate(ctx, false)
			if v.HasUpdate != c.want {
				t.Fatalf("当前 %s、最新 %s：has_update 应为 %v，得到 %+v", c.current, c.latest, c.want, v)
			}
		}
	})

	t.Run("开发构建不检查", func(t *testing.T) {
		rel := &fakeReleases{rel: release("0.4.0")}
		v := newSystem("dev", rel, newClock(), true).CheckUpdate(ctx, true)
		if !v.DevBuild || v.HasUpdate || rel.calls != 0 {
			t.Fatalf("开发构建不应请求 GitHub：%+v calls=%d", v, rel.calls)
		}
	})

	t.Run("关闭检查更新时不请求", func(t *testing.T) {
		rel := &fakeReleases{rel: release("0.4.0")}
		v := newSystem("0.3.2", rel, newClock(), false).CheckUpdate(ctx, true)
		if v.Enabled || v.HasUpdate || rel.calls != 0 || v.Current != "0.3.2" {
			t.Fatalf("%+v calls=%d", v, rel.calls)
		}
	})

	t.Run("1 小时内命中缓存，过期后重新请求", func(t *testing.T) {
		rel := &fakeReleases{rel: release("0.4.0")}
		clock := newClock()
		svc := newSystem("0.3.2", rel, clock, true)
		first := svc.CheckUpdate(ctx, false)
		clock.Add(59 * time.Minute)
		second := svc.CheckUpdate(ctx, false)
		if rel.calls != 1 || !second.CheckedAt.Equal(*first.CheckedAt) {
			t.Fatalf("缓存期内不应重新请求：calls=%d", rel.calls)
		}
		clock.Add(2 * time.Minute)
		rel.rel = release("0.4.1")
		third := svc.CheckUpdate(ctx, false)
		if rel.calls != 2 || third.Latest != "0.4.1" {
			t.Fatalf("缓存过期后应重新请求：calls=%d %+v", rel.calls, third)
		}
	})

	t.Run("强制检查跳过缓存，但 30 秒内重复强制只请求一次", func(t *testing.T) {
		rel := &fakeReleases{rel: release("0.4.0")}
		clock := newClock()
		svc := newSystem("0.3.2", rel, clock, true)
		svc.CheckUpdate(ctx, false)
		clock.Add(10 * time.Second)
		svc.CheckUpdate(ctx, true)
		if rel.calls != 1 {
			t.Fatalf("距上次请求不到 30 秒，强制检查也应复用结果：calls=%d", rel.calls)
		}
		clock.Add(31 * time.Second)
		svc.CheckUpdate(ctx, true)
		if rel.calls != 2 {
			t.Fatalf("超过 30 秒的强制检查应重新请求：calls=%d", rel.calls)
		}
	})

	t.Run("请求失败：有旧结果时返回旧结果并带警告", func(t *testing.T) {
		rel := &fakeReleases{rel: release("0.4.0")}
		clock := newClock()
		svc := newSystem("0.3.2", rel, clock, true)
		svc.CheckUpdate(ctx, false)
		clock.Add(2 * time.Hour)
		rel.err = errors.New("GitHub 接口限流")
		v := svc.CheckUpdate(ctx, false)
		if !v.HasUpdate || v.Latest != "0.4.0" || !strings.Contains(v.Warning, "限流") {
			t.Fatalf("%+v", v)
		}
	})

	t.Run("请求失败且没有旧结果：返回当前版本和失败原因，不报错", func(t *testing.T) {
		rel := &fakeReleases{err: errors.New("连接超时")}
		v := newSystem("0.3.2", rel, newClock(), true).CheckUpdate(ctx, false)
		if v.HasUpdate || v.Current != "0.3.2" || v.Latest != "" || !strings.Contains(v.Warning, "连接超时") {
			t.Fatalf("%+v", v)
		}
	})

	t.Run("失败后不立刻重试，避免每次打开后台都卡在超时上", func(t *testing.T) {
		rel := &fakeReleases{err: errors.New("连接超时")}
		clock := newClock()
		svc := newSystem("0.3.2", rel, clock, true)
		svc.CheckUpdate(ctx, false)
		clock.Add(time.Minute)
		svc.CheckUpdate(ctx, false)
		if rel.calls != 1 {
			t.Fatalf("失败后 5 分钟内不应重试：calls=%d", rel.calls)
		}
		clock.Add(5 * time.Minute)
		svc.CheckUpdate(ctx, false)
		if rel.calls != 2 {
			t.Fatalf("5 分钟后应重试：calls=%d", rel.calls)
		}
	})
}

func TestSystemUpdateService_Version(t *testing.T) {
	ctx := context.Background()
	mig := &fakeMigrations{files: []model.MigrationFileView{
		{Filename: "0001_baseline.sql", Applied: true},
		{Filename: "0002_add_x.sql", Applied: true},
		{Filename: "0003_add_y.sql", Applied: false},
	}}
	svc := NewSystemUpdateService(SystemUpdateOptions{CurrentVersion: "0.4.0", Releases: &fakeReleases{}, Migrations: mig, Now: newClock().Now})

	v, err := svc.Version(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v.Version != "0.4.0" || v.LatestMigration != "0002_add_x.sql" || v.GoVersion == "" {
		t.Fatalf("%+v", v)
	}

	mig.err = errors.New("db down")
	if _, err := svc.Version(ctx); err == nil {
		t.Fatal("读取迁移状态失败时应返回错误")
	}
}
