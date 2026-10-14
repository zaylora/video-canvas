package service

import (
	"context"
	"strings"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
)

// UpdaterClient 调用 updater 容器（真实实现是 updater.Client）。
type UpdaterClient interface {
	Status(ctx context.Context) (*model.UpdaterStatus, error)
	Upgrade(ctx context.Context, target string) error
}

// ActiveTaskCounter 统计全站进行中的生成任务（真实实现是 repository.GenerationTaskRepository）。
type ActiveTaskCounter interface {
	CountAllActive(ctx context.Context) (int64, error)
}

// UpdateChecker 给出检查到的最新版本（真实实现是 SystemUpdateService）。
type UpdateChecker interface {
	CheckUpdate(ctx context.Context, force bool) *model.UpdateCheckView
}

// SystemUpgradeOptions 是 SystemUpgradeService 的依赖。
type SystemUpgradeOptions struct {
	CurrentVersion string
	Updater        UpdaterClient // 没配置 updater token 时为 nil，即未启用一键升级
	Tasks          ActiveTaskCounter
	Checker        UpdateChecker
	Migrations     MigrationStatusReader
	Audit          AdminAuditWriter
}

// SystemUpgradeService 提供一键升级与回退：判断能不能升、升到哪个版本，然后交给 updater 执行。
// 升级步骤本身（备份、拉镜像、重建、失败回退）全在 update.sh 里，这里不重复。
type SystemUpgradeService struct {
	opts    SystemUpgradeOptions
	current string
}

// NewSystemUpgradeService 创建升级服务。
func NewSystemUpgradeService(opts SystemUpgradeOptions) *SystemUpgradeService {
	return &SystemUpgradeService{opts: opts, current: strings.TrimPrefix(opts.CurrentVersion, "v")}
}

// Info 返回升级面板的数据。updater 不可用不算错误，原因放在 UnavailableReason。
func (s *SystemUpgradeService) Info(ctx context.Context) (*model.UpgradeInfoView, error) {
	v := &model.UpgradeInfoView{Backups: []model.UpgradeBackup{}, CurrentMigrations: []string{}}
	n, err := s.opts.Tasks.CountAllActive(ctx)
	if err != nil {
		return nil, err
	}
	v.ActiveTasks = n
	if files, err := s.opts.Migrations.MigrationStatus(ctx); err == nil {
		for _, f := range files {
			if f.Applied && strings.TrimPrefix(f.AppVersion, "v") == s.current {
				v.CurrentMigrations = append(v.CurrentMigrations, f.Filename)
			}
		}
	}

	st, reason := s.status(ctx)
	if st == nil {
		v.UnavailableReason = reason
		return v, nil
	}
	v.Available = true
	v.ImageTag = st.ImageTag
	v.UsesLatest = st.ImageTag == "latest"
	v.Running = st.Running
	v.Last = st.Last
	v.LogTail = st.LogTail
	if st.Backups != nil {
		v.Backups = st.Backups
	}
	v.RollbackTarget = s.rollbackTarget(st)
	return v, nil
}

// Upgrade 升级到检查到的最新版本。
//
// 逻辑：
//  1. updater 必须可用、空闲，部署不能用 latest（用 latest 无法确认版本也无法回退）；
//  2. 目标必须正好是检查到的最新正式版本，并且比当前新：不接受任意版本号，防止被引导装上别的镜像；
//  3. 写审计后交给 updater，立即返回；进度由前端轮询 Info。
func (s *SystemUpgradeService) Upgrade(ctx context.Context, actorID uint64, target string) error {
	target = strings.TrimPrefix(strings.TrimSpace(target), "v")
	if err := s.ready(ctx); err != nil {
		return err
	}
	check := s.opts.Checker.CheckUpdate(ctx, false)
	if check == nil || !check.HasUpdate || check.Latest != target {
		msg := "当前没有可以升级的新版本"
		if check != nil && check.HasUpdate {
			msg = "只能升级到检查到的最新版本 v" + check.Latest
		}
		return errcode.ErrUpgradeTarget.WithMsg(msg)
	}
	adminAudit(ctx, s.opts.Audit, actorID, model.AdminAuditSystemUpgrade, model.AdminAuditTargetSystem, 0,
		map[string]any{"from": s.current, "to": target})
	return s.opts.Updater.Upgrade(ctx, target)
}

// Rollback 回退到最近一次成功升级之前的版本。数据库迁移不会撤销，由前端在确认时提示。
func (s *SystemUpgradeService) Rollback(ctx context.Context, actorID uint64, target string) error {
	target = strings.TrimPrefix(strings.TrimSpace(target), "v")
	if err := s.ready(ctx); err != nil {
		return err
	}
	st, _ := s.status(ctx)
	want := ""
	if st != nil {
		want = s.rollbackTarget(st)
	}
	if want == "" || want != target {
		msg := "没有可以回退的版本：只能回退到最近一次一键升级之前的版本"
		if want != "" {
			msg = "只能回退到 v" + want
		}
		return errcode.ErrUpgradeTarget.WithMsg(msg)
	}
	adminAudit(ctx, s.opts.Audit, actorID, model.AdminAuditSystemRollback, model.AdminAuditTargetSystem, 0,
		map[string]any{"from": s.current, "to": target})
	return s.opts.Updater.Upgrade(ctx, target)
}

// status 读 updater 状态；不可用时返回 nil 和原因。
func (s *SystemUpgradeService) status(ctx context.Context) (*model.UpdaterStatus, string) {
	if s.opts.Updater == nil {
		return nil, "没有启用一键升级：在部署目录的 .env 里设置 UPDATER_TOKEN，再执行 docker compose --profile updater up -d"
	}
	st, err := s.opts.Updater.Status(ctx)
	if err != nil {
		return nil, "无法连接 updater：" + err.Error()
	}
	return st, ""
}

// ready 检查能不能发起一次升级或回退。
func (s *SystemUpgradeService) ready(ctx context.Context) error {
	st, reason := s.status(ctx)
	switch {
	case st == nil:
		return errcode.ErrUpdaterUnavailable.WithMsg(reason)
	case st.Running:
		return errcode.ErrUpgradeRunning
	case st.ImageTag == "latest":
		return errcode.ErrUpgradeLatestTag
	}
	return nil
}

// rollbackTarget 只在最近一次一键升级成功、并且升到的就是当前版本时，给出它之前的版本。
// 升级后又手动换过版本、或之前用的是 latest，都不知道该退到哪，不提供回退。
func (s *SystemUpgradeService) rollbackTarget(st *model.UpdaterStatus) string {
	last := st.Last
	if last == nil || last.State != model.UpgradeStateSucceeded || last.Target != s.current {
		return ""
	}
	prev := strings.TrimPrefix(last.Previous, "v")
	if _, ok := parseSemver(prev); !ok || prev == s.current {
		return ""
	}
	return prev
}
