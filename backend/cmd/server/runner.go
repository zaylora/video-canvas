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

	"video-canvas/internal/provider/pluginrunner"
)

func runPluginRunner(args []string) error {
	fs := flag.NewFlagSet("plugin-runner", flag.ContinueOnError)
	network := fs.String("network", "tcp", "监听网络类型：tcp 或 unix")
	address := fs.String("address", "127.0.0.1:47650", "监听地址")
	poolSize := fs.Int("pool-size", 8, "每个插件版本的运行时池大小")
	hookTimeout := fs.Duration("hook-timeout", 200*time.Millisecond, "单次钩子执行时限")
	if err := fs.Parse(args); err != nil {
		return err
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
