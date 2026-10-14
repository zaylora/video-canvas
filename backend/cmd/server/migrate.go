package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"video-canvas/internal/config"
	"video-canvas/internal/initialize"
	"video-canvas/internal/migration"
	"video-canvas/internal/pkg/logger"
)

const migrateUsage = `用法：server [-c 配置文件] migrate <status|up> [-c 配置文件]

  status  列出每个迁移文件是否已执行（只读，不改库）
  up      执行全部未执行的迁移（服务启动时也会自动执行）`

// runMigrate 处理 migrate 子命令，不启动 HTTP 服务。configPath 是主命令解析到的配置路径，子命令后面的 -c 优先。
// 容器里用法：docker compose run --rm --no-deps backend migrate status
func runMigrate(args []string, configPath string, out io.Writer) error {
	if len(args) == 0 || (args[0] != "status" && args[0] != "up") {
		return errors.New(migrateUsage)
	}
	fs := flag.NewFlagSet("migrate "+args[0], flag.ContinueOnError)
	path := fs.String("c", configPath, "配置文件路径")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	cfg, err := config.Load(*path)
	if err != nil {
		return err
	}
	if err := logger.Init(cfg.Log); err != nil {
		return fmt.Errorf("初始化日志: %w", err)
	}
	defer logger.Sync()

	db, err := initialize.OpenDB(cfg.Database)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	if args[0] == "up" {
		if err := migration.Run(ctx, db, initialize.MigrationOptions(cfg.Database)); err != nil {
			return err
		}
	}
	st, err := migration.Status(ctx, db, nil)
	if err != nil {
		return err
	}
	printStatus(out, st)
	return nil
}

func printStatus(out io.Writer, st []migration.FileStatus) {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "文件\t状态\t执行时间\t执行版本")
	pending := 0
	for _, s := range st {
		state := "未执行"
		switch {
		case s.Unknown:
			state = "库里有、程序里没有"
		case s.Modified:
			state = "已执行，但文件被改过"
		case s.Applied:
			state = "已执行"
		default:
			pending++
		}
		at := ""
		if s.Applied {
			at = s.AppliedAt.Local().Format("2006-01-02 15:04:05")
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", s.Filename, state, at, s.AppVersion)
	}
	_ = w.Flush()
	fmt.Fprintf(out, "\n共 %d 个迁移，%d 个未执行\n", len(st), pending)
}
