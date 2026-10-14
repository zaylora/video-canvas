package service_test

import (
	"context"
	"errors"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	. "video-canvas/internal/service"
)

type fakeUpdater struct {
	st       *model.UpdaterStatus
	err      error
	upgraded []string
	upErr    error
}

func (f *fakeUpdater) Status(context.Context) (*model.UpdaterStatus, error) {
	if f.err != nil {
		return nil, f.err
	}
	st := *f.st
	return &st, nil
}

func (f *fakeUpdater) Upgrade(_ context.Context, target string) error {
	if f.upErr != nil {
		return f.upErr
	}
	f.upgraded = append(f.upgraded, target)
	return nil
}

type fakeTaskCounter struct{ n int64 }

func (f fakeTaskCounter) CountAllActive(context.Context) (int64, error) { return f.n, nil }

type fakeChecker struct{ v *model.UpdateCheckView }

func (f fakeChecker) CheckUpdate(context.Context, bool) *model.UpdateCheckView { return f.v }

type upgradeEnv struct {
	svc     *SystemUpgradeService
	updater *fakeUpdater
	audit   *fakeAudit
}

func newUpgradeEnv(current string, st *model.UpdaterStatus, check *model.UpdateCheckView) *upgradeEnv {
	up := &fakeUpdater{st: st}
	audit := &fakeAudit{}
	mig := &fakeMigrations{files: []model.MigrationFileView{
		{Filename: "0001_baseline.sql", Applied: true, AppVersion: "0.4.0"},
		{Filename: "0002_add_x.sql", Applied: true, AppVersion: current},
	}}
	svc := NewSystemUpgradeService(SystemUpgradeOptions{
		CurrentVersion: current, Updater: up, Tasks: fakeTaskCounter{n: 3}, Checker: fakeChecker{v: check}, Migrations: mig, Audit: audit,
	})
	return &upgradeEnv{svc: svc, updater: up, audit: audit}
}

func idleStatus() *model.UpdaterStatus {
	return &model.UpdaterStatus{ImageTag: "0.4.1", Backups: []model.UpgradeBackup{}}
}

func hasUpdate(latest string) *model.UpdateCheckView {
	return &model.UpdateCheckView{Enabled: true, Current: "0.4.1", Latest: latest, HasUpdate: true}
}

func TestSystemUpgradeService_Info(t *testing.T) {
	ctx := context.Background()

	t.Run("没有配置 updater", func(t *testing.T) {
		svc := NewSystemUpgradeService(SystemUpgradeOptions{CurrentVersion: "0.4.1", Tasks: fakeTaskCounter{}, Checker: fakeChecker{}, Migrations: &fakeMigrations{}})
		v, err := svc.Info(ctx)
		if err != nil || v.Available || v.UnavailableReason == "" {
			t.Fatalf("%+v %v", v, err)
		}
	})

	t.Run("updater 连不上", func(t *testing.T) {
		e := newUpgradeEnv("0.4.1", idleStatus(), hasUpdate("0.5.0"))
		e.updater.err = errors.New("dial unix: no such file")
		v, _ := e.svc.Info(ctx)
		if v.Available || v.UnavailableReason == "" {
			t.Fatalf("%+v", v)
		}
	})

	t.Run("可用：带上任务数、当前版本执行过的迁移", func(t *testing.T) {
		e := newUpgradeEnv("0.4.1", idleStatus(), hasUpdate("0.5.0"))
		v, err := e.svc.Info(ctx)
		if err != nil || !v.Available || v.ImageTag != "0.4.1" || v.ActiveTasks != 3 || v.UsesLatest {
			t.Fatalf("%+v %v", v, err)
		}
		if len(v.CurrentMigrations) != 1 || v.CurrentMigrations[0] != "0002_add_x.sql" {
			t.Fatalf("只列出当前版本执行的迁移：%v", v.CurrentMigrations)
		}
		if v.RollbackTarget != "" {
			t.Fatalf("没有升级记录时不能回退：%q", v.RollbackTarget)
		}
	})

	t.Run("最近一次成功升级到当前版本时，可以回退到它之前的版本", func(t *testing.T) {
		st := idleStatus()
		st.Last = &model.UpgradeRunStatus{Target: "0.4.1", Previous: "0.4.0", State: model.UpgradeStateSucceeded}
		v, _ := newUpgradeEnv("0.4.1", st, hasUpdate("0.5.0")).svc.Info(ctx)
		if v.RollbackTarget != "0.4.0" {
			t.Fatalf("%q", v.RollbackTarget)
		}
	})

	t.Run("这些情况都不能回退", func(t *testing.T) {
		cases := []*model.UpgradeRunStatus{
			{Target: "0.4.1", Previous: "0.4.0", State: model.UpgradeStateFailedRolledBack}, // 上次没成功
			{Target: "0.4.0", Previous: "0.3.2", State: model.UpgradeStateSucceeded},        // 记录的不是当前版本（之后又手动换过）
			{Target: "0.4.1", Previous: "latest", State: model.UpgradeStateSucceeded},       // 之前是 latest，不知道是哪个版本
			{Target: "0.4.1", Previous: "", State: model.UpgradeStateSucceeded},
		}
		for _, last := range cases {
			st := idleStatus()
			st.Last = last
			v, _ := newUpgradeEnv("0.4.1", st, hasUpdate("0.5.0")).svc.Info(ctx)
			if v.RollbackTarget != "" {
				t.Fatalf("%+v 不应能回退，得到 %q", last, v.RollbackTarget)
			}
		}
	})

	t.Run("部署用 latest", func(t *testing.T) {
		st := idleStatus()
		st.ImageTag = "latest"
		v, _ := newUpgradeEnv("0.4.1", st, hasUpdate("0.5.0")).svc.Info(ctx)
		if !v.UsesLatest {
			t.Fatalf("%+v", v)
		}
	})
}

func TestSystemUpgradeService_Upgrade(t *testing.T) {
	ctx := context.Background()

	t.Run("升级到检查到的最新版本，写审计", func(t *testing.T) {
		e := newUpgradeEnv("0.4.1", idleStatus(), hasUpdate("0.5.0"))
		if err := e.svc.Upgrade(ctx, 7, "0.5.0"); err != nil {
			t.Fatal(err)
		}
		if len(e.updater.upgraded) != 1 || e.updater.upgraded[0] != "0.5.0" {
			t.Fatalf("%v", e.updater.upgraded)
		}
		if len(e.audit.logs) != 1 || e.audit.logs[0].Action != model.AdminAuditSystemUpgrade || e.audit.logs[0].ActorID != 7 {
			t.Fatalf("%+v", e.audit.logs)
		}
	})

	t.Run("带 v 前缀也认", func(t *testing.T) {
		e := newUpgradeEnv("0.4.1", idleStatus(), hasUpdate("0.5.0"))
		if err := e.svc.Upgrade(ctx, 7, "v0.5.0"); err != nil || e.updater.upgraded[0] != "0.5.0" {
			t.Fatalf("%v %v", err, e.updater.upgraded)
		}
	})

	reject := []struct {
		name   string
		mutate func(e *upgradeEnv)
		target string
		want   *errcode.Error
	}{
		{"不是检查到的最新版本", nil, "0.4.9", errcode.ErrUpgradeTarget},
		{"没有新版本", func(e *upgradeEnv) {}, "0.4.1", errcode.ErrUpgradeTarget},
		{"正在升级", func(e *upgradeEnv) { e.updater.st.Running = true }, "0.5.0", errcode.ErrUpgradeRunning},
		{"用 latest", func(e *upgradeEnv) { e.updater.st.ImageTag = "latest" }, "0.5.0", errcode.ErrUpgradeLatestTag},
		{"updater 连不上", func(e *upgradeEnv) { e.updater.err = errors.New("dial") }, "0.5.0", errcode.ErrUpdaterUnavailable},
	}
	for _, c := range reject {
		t.Run("拒绝："+c.name, func(t *testing.T) {
			check := hasUpdate("0.5.0")
			if c.name == "没有新版本" {
				check = &model.UpdateCheckView{Enabled: true, Current: "0.4.1", Latest: "0.4.1"}
			}
			e := newUpgradeEnv("0.4.1", idleStatus(), check)
			if c.mutate != nil {
				c.mutate(e)
			}
			err := e.svc.Upgrade(ctx, 7, c.target)
			if !isCode(err, c.want) {
				t.Fatalf("应返回 %v，得到 %v", c.want, err)
			}
			if len(e.updater.upgraded) != 0 || len(e.audit.logs) != 0 {
				t.Fatal("被拒绝时不应调用 updater，也不应写审计")
			}
		})
	}

	t.Run("updater 报正在运行（并发点击）", func(t *testing.T) {
		e := newUpgradeEnv("0.4.1", idleStatus(), hasUpdate("0.5.0"))
		e.updater.upErr = errcode.ErrUpgradeRunning
		if err := e.svc.Upgrade(ctx, 7, "0.5.0"); !isCode(err, errcode.ErrUpgradeRunning) {
			t.Fatalf("%v", err)
		}
	})
}

func TestSystemUpgradeService_Rollback(t *testing.T) {
	ctx := context.Background()
	st := idleStatus()
	st.Last = &model.UpgradeRunStatus{Target: "0.4.1", Previous: "0.4.0", State: model.UpgradeStateSucceeded}

	e := newUpgradeEnv("0.4.1", st, hasUpdate("0.5.0"))
	if err := e.svc.Rollback(ctx, 7, "0.3.2"); !isCode(err, errcode.ErrUpgradeTarget) {
		t.Fatalf("只能回退到上一个版本：%v", err)
	}
	if err := e.svc.Rollback(ctx, 7, "0.4.0"); err != nil {
		t.Fatal(err)
	}
	if len(e.updater.upgraded) != 1 || e.updater.upgraded[0] != "0.4.0" || e.audit.logs[0].Action != model.AdminAuditSystemRollback {
		t.Fatalf("%v %+v", e.updater.upgraded, e.audit.logs)
	}

	none := newUpgradeEnv("0.4.1", idleStatus(), hasUpdate("0.5.0"))
	if err := none.svc.Rollback(ctx, 7, "0.4.0"); !isCode(err, errcode.ErrUpgradeTarget) {
		t.Fatalf("没有升级记录时不能回退：%v", err)
	}
}

// isCode 判断 err 是不是业务错误 want（按错误码比较，WithMsg 生成的副本也算）。
func isCode(err error, want *errcode.Error) bool {
	var ec *errcode.Error
	return errors.As(err, &ec) && ec.Code == want.Code
}
