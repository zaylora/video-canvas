package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"video-canvas/internal/config"
	"video-canvas/internal/initialize"
	"video-canvas/internal/pkg/logger"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "plugin-runner" {
		if err := runPluginRunner(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "plugin-runner 退出错误:", err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "服务器退出错误:", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("c", "configs/config.yaml", "配置文件路径")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}

	if err := logger.Init(cfg.Log); err != nil {
		return fmt.Errorf("初始化日志: %w", err)
	}
	defer logger.Sync()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app, err := initialize.NewApp(cfg)
	if err != nil {
		return err
	}
	return app.Run(ctx)
}
