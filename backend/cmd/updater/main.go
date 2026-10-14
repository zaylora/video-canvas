// updater 是一键升级的执行端，跑在单独的容器里（compose profile: updater，默认不启用）。
// 它挂载 docker.sock 和部署目录，收到 backend 的请求后执行 update.sh；backend 通过共享卷里的 unix socket 调用它。
//
// 环境变量：
//
//	UPDATER_TOKEN       必填，与 backend 的 APP_UPDATE_UPDATER_TOKEN 相同
//	UPDATER_SOCKET      监听的 unix socket，默认 /run/updater/updater.sock
//	UPDATER_DEPLOY_DIR  部署目录在容器里的路径，默认 /deploy
//	UPDATER_SCRIPT      update.sh 路径，默认 /app/update.sh（镜像自带，和 updater 同一版本）
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"video-canvas/internal/updater"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "updater 退出错误:", err)
		os.Exit(1)
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func run() error {
	socket := env("UPDATER_SOCKET", "/run/updater/updater.sock")
	srv, err := updater.NewServer(updater.ServerOptions{
		Token:     os.Getenv("UPDATER_TOKEN"),
		DeployDir: env("UPDATER_DEPLOY_DIR", "/deploy"),
		Script:    env("UPDATER_SCRIPT", "/app/update.sh"),
	})
	if err != nil {
		return err
	}

	_ = os.Remove(socket)
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "unix", socket)
	if err != nil {
		return fmt.Errorf("监听 %s 失败: %w", socket, err)
	}
	defer os.Remove(socket)
	// backend 以非 root 运行；socket 由 token 保护，权限放开给同一个卷里的其他进程
	if err := os.Chmod(socket, 0o666); err != nil { // #nosec G302 -- 访问控制靠 token
		return err
	}

	hs := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 5 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = hs.Shutdown(shutdownCtx)
	}()

	fmt.Fprintln(os.Stderr, "updater 已启动，监听", socket)
	if err := hs.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	// 正在执行的升级不打断：update.sh 半途被杀会留下改了一半的部署
	srv.Wait()
	return nil
}
