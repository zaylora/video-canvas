package initialize

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/config"
	agenthandler "video-canvas/internal/handler/agent"
	"video-canvas/internal/llmgateway"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/pkg/videoframe"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/webaccess"
	"video-canvas/internal/repository"
	"video-canvas/internal/router"
	"video-canvas/internal/service"
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
	Script string // 入口脚本的绝对路径：仓库里是 agent/src/main.ts，部署镜像里是 agent/dist/main.js
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
	script, err := agentEntryScript(dir)
	if err != nil {
		return AgentRuntimePaths{}, fmt.Errorf("agent.runtime_dir=%q 里找不到 src/main.ts 或 dist/main.js，请在 %s 下运行 npm run build：%w", cfg.RuntimeDir, dir, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "node_modules")); err != nil {
		return AgentRuntimePaths{}, fmt.Errorf("agent 目录还没有安装依赖，请在 %s 下运行 npm ci：%w", dir, err)
	}
	return AgentRuntimePaths{Node: node, Script: script}, nil
}

// AgentLauncher 按配置选启动器：http 模式连独立的 Agent 服务，否则每个片段起一个 Node 子进程。
func AgentLauncher(ac config.Agent) agentsvc.Launcher {
	if ac.Runtime == config.AgentRuntimeHTTP {
		return &agentsvc.HTTPLauncher{BaseURL: ac.URL, ServiceToken: ac.ServiceToken}
	}
	return agentsvc.ExecLauncher{}
}

// BridgeURLFor 是 Agent 访问桥用的地址：http 和 durable 模式用配置里写的对外地址（Agent 容器要能连上），
// 否则是本机的监听地址。
func BridgeURLFor(ac config.Agent, listen net.Addr) string {
	if ac.RemoteService() {
		return strings.TrimRight(ac.BridgeURL, "/")
	}
	return BridgeLocalURL(listen)
}

// AgentProxyFor 在 durable 模式下创建前端 Agent 接口的反向代理；没启用 Agent 或不是 durable 返回 nil（路由走 Go 自己的实现）。
func AgentProxyFor(ac config.Agent) (*agenthandler.AgentProxyHandler, error) {
	if !ac.Enabled || ac.Runtime != config.AgentRuntimeDurable {
		return nil, nil
	}
	return agenthandler.NewAgentProxy(ac.URL, ac.ServiceToken)
}

// agentEntryScript 选入口脚本。仓库里有源码就直接跑源码（Node 22.19+ 自带类型剥离，不用先构建），
// 部署镜像里只有 npm run build 的产物。两者同时存在时用源码：开发时改了 src，不能被旧的 dist 盖住。
func agentEntryScript(dir string) (string, error) {
	src := filepath.Join(dir, "src", "main.ts")
	if _, err := os.Stat(src); err == nil {
		return src, nil
	}
	built := filepath.Join(dir, "dist", "main.js")
	if _, err := os.Stat(built); err != nil {
		return "", err
	}
	return built, nil
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

// ListenBridge 按配置监听桥。回环地址照旧；非回环地址只有配了合格的服务令牌才放行
// （配置校验之外再拦一道：绑到别的地址就只剩令牌这一道屏障了）。
func ListenBridge(ac config.Agent) (net.Listener, error) {
	addr := ac.WithDefaults().BridgeAddr
	if config.ValidServiceToken(ac.ServiceToken) {
		var lc net.ListenConfig
		return lc.Listen(context.Background(), "tcp", addr)
	}
	return ListenLoopback(addr)
}

// BridgeLocalURL 是本机进程回调桥用的地址：监听在 0.0.0.0 或 :: 时 Addr() 是「未指定地址」，不能直接拿来连，换成 127.0.0.1。
func BridgeLocalURL(addr net.Addr) string {
	tcp, ok := addr.(*net.TCPAddr)
	if !ok {
		return "http://" + addr.String()
	}
	if tcp.IP == nil || tcp.IP.IsUnspecified() {
		return "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(tcp.Port))
	}
	return "http://" + tcp.String()
}

// agentRuntime 是装配好的画布 Agent 运行时：桥的监听服务和进程运行时。
type agentRuntime struct {
	mode   string                   // 运行方式：process 或 http，写进启动日志，运维一眼能看出
	rt     *agentsvc.ProcessRuntime // durable 模式为 nil
	server *http.Server
	ln     net.Listener
}

// agentDeps 是装配运行时需要的已有服务。
type agentDeps struct {
	tasks  agentsvc.AgentGenTasks
	assets provider.AssetStore
	// frames / frameStore 是看视频和截帧要用的抽帧器和帧图存储；nil 表示没有视频处理能力
	frames     agentsvc.VideoFrames
	frameStore agentsvc.FrameStore
	repo       *repository.AgentRepository
	agent      *agentsvc.AgentService
	canvas     *agentsvc.AgentCanvasService
	registry   provider.Registry
	secrets    provider.SecretResolver
	skills     agentsvc.SkillLibrary
	// toolCalls 逐条留存工具调用的参数、结果和耗时；nil 表示不记录
	toolCalls agentsvc.ToolCallRecorder
	// modelRequests 留存模型调用实际发给上游的请求体（7 天）；nil 表示不记录
	modelRequests agentsvc.ModelRequestRecorder
	// ipAllowed 是出站请求的 IP 策略；nil 用默认（拒绝全部内网）
	ipAllowed func(net.IP) bool
	// broadcaster 是浏览器 WebSocket 的广播器；durable 模式下 Agent 服务推来的事件经它发出
	broadcaster ws.Broadcaster
}

// webToolNames 是联网工具的名字：关掉联网时从下发给 Agent 的工具里去掉。
var webToolNames = []string{"web_search", "web_fetch"}

// AgentWebAccess 按配置创建 Agent 的联网能力。关闭时返回 nil 和要停用的工具名：模型看不到它们。
// 联网请求都由 Go 发出（Agent 进程不联网），目标是内网或本机时拒绝，用的出站 IP 策略和模型网关一致。
func AgentWebAccess(ac config.Agent, ipAllowed func(net.IP) bool) (provider.WebAccess, []string) {
	if !ac.WebEnabled {
		return nil, webToolNames
	}
	return webaccess.New(webaccess.Config{SearXNGURL: ac.SearXNGURL, IPAllowed: ipAllowed}), nil
}

// AgentDisabledTools 是这次启动停用的工具：配置里点名停用的，加上关掉联网时的联网工具，去重。
func AgentDisabledTools(ac config.Agent) []string {
	out := slices.Clone(ac.DisabledTools)
	if !ac.WebEnabled {
		out = append(out, webToolNames...)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// newAgentRuntime 按配置装配运行时：自检 → 监听回环地址 → 创建桥和进程运行时 → 绑定到 AgentService。
// 没启用时返回 nil。
func newAgentRuntime(cfg *config.Config, d agentDeps) (*agentRuntime, error) {
	ac := cfg.Agent
	if !ac.Enabled {
		return nil, nil
	}
	// http 和 durable 模式下 Agent 在别的容器里，本机不需要 Node，也没有入口脚本可查
	var paths AgentRuntimePaths
	if !ac.RemoteService() {
		var err error
		if paths, err = ResolveAgentRuntime(ac); err != nil {
			return nil, err
		}
	}
	ln, err := ListenBridge(ac)
	if err != nil {
		return nil, fmt.Errorf("监听桥地址失败：%w", err)
	}
	web, _ := AgentWebAccess(ac, d.ipAllowed)
	bridge := agentsvc.NewAgentBridge(agentsvc.BridgeDeps{
		Repo: d.repo, Canvas: d.canvas, Agent: d.agent, Billing: agentsvc.NewAgentBilling(d.repo),
		Registry: d.registry, Secrets: d.secrets, Tasks: d.tasks, Assets: d.assets, Frames: d.frames, FrameStore: d.frameStore, Streamer: llmgateway.New(llmgateway.Options{IPAllowed: d.ipAllowed}), Skills: d.skills, Web: web, RunUsage: d.repo,
	})
	bridge.SetDisabledTools(AgentDisabledTools(ac))
	if d.toolCalls != nil {
		bridge.SetToolRecorder(d.toolCalls)
	}
	if d.modelRequests != nil {
		bridge.SetModelRequestRecorder(d.modelRequests)
	}
	platform := agenthandler.NewPlatformHandler(bridge)
	// durable：会话、运行、审批都在 Agent 服务里，Go 没有进程运行时；Agent 服务产生的事件经 /events 推过来，由 Go 发给浏览器
	var rt *agentsvc.ProcessRuntime
	if ac.Runtime == config.AgentRuntimeDurable {
		platform.WithEventRelay(agentsvc.NewEventRelay(d.broadcaster))
	} else {
		rt = agentsvc.NewProcessRuntime(agentsvc.RuntimeDeps{
			Repo: d.repo, Bridge: bridge, Canvas: d.canvas, Agent: d.agent, Registry: d.registry, Launcher: AgentLauncher(ac),
			Config: agentsvc.RuntimeConfig{NodePath: paths.Node, ScriptPath: paths.Script, BridgeURL: BridgeURLFor(ac, ln.Addr()), ServiceToken: ac.ServiceToken, WorkRoot: ac.WorkDir, DisabledTools: AgentDisabledTools(ac)},
		})
		d.agent.SetRuntime(rt)
	}
	return &agentRuntime{
		mode: ac.WithDefaults().Runtime, rt: rt, ln: ln,
		// 没有读写超时：对话是长连接的流式响应，可能持续几分钟；防慢速攻击靠只监听回环地址或内网（配了服务令牌）
		server: &http.Server{Handler: router.NewBridge(cfg.Server.Mode, agenthandler.NewAgentBridgeHandler(bridge), platform, ac.ServiceToken), ReadHeaderTimeout: 10 * time.Second},
	}, nil
}

// serve 开始提供桥服务，阻塞到服务关闭。
func (a *agentRuntime) serve() {
	logger.Info("画布 Agent 桥已启动", zap.String("addr", a.ln.Addr().String()), zap.String("runtime", a.mode))
	if err := a.server.Serve(a.ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("画布 Agent 桥异常退出", zap.Error(err))
	}
}

// stop 关闭桥并杀掉所有运行进程，等它们的状态修正做完。必须在关库之前调用。
func (a *agentRuntime) stop(ctx context.Context) {
	if err := a.server.Shutdown(ctx); err != nil {
		_ = a.server.Close() // 优雅关闭超时就直接断开：进程马上要被杀了，没有继续等的意义
	}
	if a.rt != nil { // durable 模式没有进程运行时：运行在 Agent 服务里，由它自己恢复
		a.rt.Shutdown()
	}
}

// newVideoFrames 创建抽帧器；找不到 ffmpeg / ffprobe 时只记警告，Agent 的两个视频工具会提示「不能处理视频」。
func newVideoFrames() *videoframe.Extractor {
	e := videoframe.New(videoframe.Options{})
	if !e.Available() {
		logger.Warn("没有找到 ffmpeg / ffprobe，画布 Agent 不能看视频和截帧")
	}
	return e
}

// assetFrameStore 把截出的帧经素材服务存成用户素材。
type assetFrameStore struct{ assets *service.AssetService }

func (s assetFrameStore) SaveFrame(ctx context.Context, userID uint64, fileName string, png []byte) (*model.AssetView, error) {
	return s.assets.Upload(ctx, userID, service.UploadInput{FileName: fileName, Size: int64(len(png)), Body: bytes.NewReader(png)})
}
