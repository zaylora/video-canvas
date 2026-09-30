package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"video-canvas/internal/provider/plugin"
	"video-canvas/internal/provider/pluginrunner"
)

// healthcheckTimeout 是 -healthcheck 探活的总时限，要小于 compose 里 healthcheck 的 timeout。
const healthcheckTimeout = 2 * time.Second

// checkRunnerHealth 连接 runner 的地址并调用健康检查接口：可用返回 nil，否则返回错误。
// 复用宿主访问 runner 的同一个客户端，保证“健康”的含义与主服务实际使用时一致。
func checkRunnerHealth(network, address string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return plugin.NewHTTPRunnerClient(network, address).Ready(ctx)
}

func runPluginRunner(args []string) error {
	fs := flag.NewFlagSet("plugin-runner", flag.ContinueOnError)
	network := fs.String("network", "tcp", "监听网络类型：tcp 或 unix")
	address := fs.String("address", "127.0.0.1:47650", "监听地址")
	poolSize := fs.Int("pool-size", 8, "每个插件版本的运行时池大小")
	hookTimeout := fs.Duration("hook-timeout", 200*time.Millisecond, "单次钩子执行时限")
	loadTimeout := fs.Duration("load-timeout", 2*time.Second, "执行插件顶层代码（装载、预检）的时限")
	healthcheck := fs.Bool("healthcheck", false, "只做健康检查：连接 -network/-address 指向的 runner，可用退出码 0，否则 1（供容器 healthcheck 使用）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *healthcheck {
		// 健康检查是一次性命令，不监听端口，也不动 unix socket 文件
		if err := checkRunnerHealth(*network, *address, healthcheckTimeout); err != nil {
			return fmt.Errorf("plugin-runner 健康检查失败: %w", err)
		}
		return nil
	}
	if *network == "unix" {
		_ = os.Remove(*address)
	}
	listener, err := net.Listen(*network, *address)
	if err != nil {
		return fmt.Errorf("plugin-runner 监听失败: %w", err)
	}
	if *network == "unix" {
		defer os.Remove(*address)
	}
	server := &http.Server{Handler: pluginrunner.NewServer(pluginrunner.Options{
		PoolSize:       *poolSize,
		DefaultTimeout: *hookTimeout,
		LoadTimeout:    *loadTimeout,
	}).Handler()}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
