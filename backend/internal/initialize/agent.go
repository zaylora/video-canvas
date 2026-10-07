package initialize

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/config"
	agenthandler "video-canvas/internal/handler/agent"
	"video-canvas/internal/llmgateway"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
	"video-canvas/internal/router"
	agentsvc "video-canvas/internal/service/agent"
)

// 运行时要求的最低 Node 版本（pi 1.0.4 的要求）。
const (
	minNodeMajor = 22
	minNodeMinor = 19
)

// AgentRuntimePaths 是解析好的运行时路径。
type AgentRuntimePaths struct {
	Node   string // node 可执行文件的绝对路径
	Script string // agent/src/main.mjs 的绝对路径
}

// NodeVersionOK 解析 `node --version` 的输出（如 v23.6.0），判断是否满足最低版本；解析不了返回错误。
func NodeVersionOK(out string) (bool, error) {
	parts := strings.SplitN(strings.TrimPrefix(strings.TrimSpace(out), "v"), ".", 3)
	if len(parts) < 2 {
		return false, fmt.Errorf("无法解析 node 版本号 %q", strings.TrimSpace(out))
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return false, fmt.Errorf("无法解析 node 版本号 %q", strings.TrimSpace(out))
	}
	return major > minNodeMajor || (major == minNodeMajor && minor >= minNodeMinor), nil
}

// ResolveAgentRuntime 启动自检：开启了画布 Agent 却缺 Node、版本太低或没装依赖，启动时就报清楚，
// 而不是等用户第一次使用才失败。
func ResolveAgentRuntime(cfg config.Agent) (AgentRuntimePaths, error) {
	node, err := exec.LookPath(cfg.NodePath)
	if err != nil {
		return AgentRuntimePaths{}, fmt.Errorf("agent.enabled=true 但找不到 node（agent.node_path=%q）：%w", cfg.NodePath, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	//nolint:gosec // G204: 可执行文件来自服务端配置，不含用户输入
	out, err := exec.CommandContext(ctx, node, "--version").Output()
	if err != nil {
		return AgentRuntimePaths{}, fmt.Errorf("执行 %s --version 失败：%w", node, err)
	}
	ok, err := NodeVersionOK(string(out))
	if err != nil {
		return AgentRuntimePaths{}, err
	}
	if !ok {
		return AgentRuntimePaths{}, fmt.Errorf("node 版本太低（%s），画布 Agent 需要 >= %d.%d", strings.TrimSpace(string(out)), minNodeMajor, minNodeMinor)
	}
	dir, err := filepath.Abs(cfg.RuntimeDir)
	if err != nil {
		return AgentRuntimePaths{}, err
	}
	script := filepath.Join(dir, "src", "main.mjs")
	if _, err := os.Stat(script); err != nil {
		return AgentRuntimePaths{}, fmt.Errorf("agent.runtime_dir=%q 里找不到 src/main.mjs：%w", cfg.RuntimeDir, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "node_modules")); err != nil {
		return AgentRuntimePaths{}, fmt.Errorf("agent 目录还没有安装依赖，请在 %s 下运行 npm ci：%w", dir, err)
	}
	return AgentRuntimePaths{Node: node, Script: script}, nil
}

// ListenLoopback 监听一个回环地址。配置校验已经要求过回环，这里再确认一次（防御纵深）：
// 桥只该被本机的 Node 进程访问，绑到别的地址就只剩一次性令牌这一道屏障了。
func ListenLoopback(addr string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, fmt.Errorf("桥只能监听回环地址，收到 %q", addr)
	}
	var lc net.ListenConfig
	return lc.Listen(context.Background(), "tcp", addr)
}

// agentRuntime 是装配好的画布 Agent 运行时：桥的监听服务和进程运行时。
type agentRuntime struct {
	rt     *agentsvc.ProcessRuntime
	server *http.Server
	ln     net.Listener
}

// agentDeps 是装配运行时需要的已有服务。
type agentDeps struct {
	tasks    agentsvc.AgentGenTasks
	assets   provider.AssetStore
	repo     *repository.AgentRepository
	agent    *agentsvc.AgentService
	canvas   *agentsvc.AgentCanvasService
	registry provider.Registry
	secrets  provider.SecretResolver
}

// newAgentRuntime 按配置装配运行时：自检 → 监听回环地址 → 创建桥和进程运行时 → 绑定到 AgentService。
// 没启用时返回 nil。
func newAgentRuntime(cfg *config.Config, d agentDeps) (*agentRuntime, error) {
	ac := cfg.Agent
	if !ac.Enabled {
		return nil, nil
	}
	paths, err := ResolveAgentRuntime(ac)
	if err != nil {
		return nil, err
	}
	ln, err := ListenLoopback(ac.BridgeAddr)
	if err != nil {
		return nil, fmt.Errorf("监听桥地址失败：%w", err)
	}
	bridge := agentsvc.NewAgentBridge(agentsvc.BridgeDeps{
		Repo: d.repo, Canvas: d.canvas, Agent: d.agent, Billing: agentsvc.NewAgentBilling(d.repo),
		Registry: d.registry, Secrets: d.secrets, Tasks: d.tasks, Assets: d.assets, Streamer: llmgateway.New(llmgateway.Options{}),
	})
	rt := agentsvc.NewProcessRuntime(agentsvc.RuntimeDeps{
		Repo: d.repo, Bridge: bridge, Canvas: d.canvas, Agent: d.agent, Registry: d.registry, Launcher: agentsvc.ExecLauncher{},
		Config: agentsvc.RuntimeConfig{NodePath: paths.Node, ScriptPath: paths.Script, BridgeURL: "http://" + ln.Addr().String(), WorkRoot: ac.WorkDir},
	})
	d.agent.SetRuntime(rt)
	return &agentRuntime{
		rt: rt, ln: ln,
		// 没有读写超时：对话是长连接的流式响应，可能持续几分钟；防慢速攻击靠只监听回环地址
		server: &http.Server{Handler: router.NewBridge(cfg.Server.Mode, agenthandler.NewAgentBridgeHandler(bridge)), ReadHeaderTimeout: 10 * time.Second},
	}, nil
}

// serve 开始提供桥服务，阻塞到服务关闭。
func (a *agentRuntime) serve() {
	logger.Info("画布 Agent 桥已启动", zap.String("addr", a.ln.Addr().String()))
	if err := a.server.Serve(a.ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("画布 Agent 桥异常退出", zap.Error(err))
	}
}

// stop 关闭桥并杀掉所有运行进程，等它们的状态修正做完。必须在关库之前调用。
func (a *agentRuntime) stop(ctx context.Context) {
	if err := a.server.Shutdown(ctx); err != nil {
		_ = a.server.Close() // 优雅关闭超时就直接断开：进程马上要被杀了，没有继续等的意义
	}
	a.rt.Shutdown()
}
