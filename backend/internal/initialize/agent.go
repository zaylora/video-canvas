package initialize

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
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

// AgentProxyFor 创建前端 Agent 接口的反向代理；没启用 Agent 返回 nil（这些接口统一返回「Agent 暂不可用」）。
func AgentProxyFor(ac config.Agent) (*agenthandler.AgentProxyHandler, error) {
	if !ac.Enabled {
		return nil, nil
	}
	return agenthandler.NewAgentProxy(ac.URL, ac.ServiceToken)
}

// AgentRemoteRunsFor 创建读 Agent 服务里运行的客户端（后台运行记录、导出、摘要同步用）；没启用 Agent 返回 nil。
func AgentRemoteRunsFor(ac config.Agent) agentsvc.RemoteRuns {
	if !ac.Enabled {
		return nil
	}
	return &agentsvc.HTTPRemoteRuns{BaseURL: ac.URL, ServiceToken: ac.ServiceToken}
}

// ListenBridge 按配置监听平台 API。它是内网信任的入口，所以只有配了合格的服务令牌才监听（配置校验之外再拦一道）。
func ListenBridge(ac config.Agent) (net.Listener, error) {
	if !config.ValidServiceToken(ac.ServiceToken) {
		return nil, errors.New("平台 API 需要合格的服务令牌（agent.service_token）才能监听")
	}
	var lc net.ListenConfig
	return lc.Listen(context.Background(), "tcp", ac.WithDefaults().BridgeAddr)
}

// agentRuntime 是装配好的画布 Agent 平台 API：监听和 HTTP 服务。
type agentRuntime struct {
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

// newAgentRuntime 按配置装配平台 API：监听内网地址 → 创建桥（模型代理、工具、审批、运行准备）→ 事件推送接到 WebSocket。
// 没启用时返回 nil。
func newAgentRuntime(cfg *config.Config, d agentDeps) (*agentRuntime, error) {
	ac := cfg.Agent
	if !ac.Enabled {
		return nil, nil
	}
	ln, err := ListenBridge(ac)
	if err != nil {
		return nil, fmt.Errorf("监听平台 API 地址失败：%w", err)
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
	// Agent 服务产生的事件经 /events 推过来，由 Go 发给浏览器
	platform := agenthandler.NewPlatformHandler(bridge).WithEventRelay(agentsvc.NewEventRelay(d.broadcaster))
	return &agentRuntime{
		ln: ln,
		// 没有读写超时：模型代理是长连接的流式响应，可能持续几分钟；防慢速攻击靠只监听内网并要求服务令牌
		server: &http.Server{Handler: router.NewBridge(cfg.Server.Mode, platform, ac.ServiceToken), ReadHeaderTimeout: 10 * time.Second},
	}, nil
}

// serve 开始提供平台 API，阻塞到服务关闭。
func (a *agentRuntime) serve() {
	logger.Info("画布 Agent 平台 API 已启动", zap.String("addr", a.ln.Addr().String()))
	if err := a.server.Serve(a.ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("画布 Agent 平台 API 异常退出", zap.Error(err))
	}
}

// stop 关闭平台 API。必须在关库之前调用。运行在 Agent 服务里，由它自己恢复，这里没有进程要杀。
func (a *agentRuntime) stop(ctx context.Context) {
	if err := a.server.Shutdown(ctx); err != nil {
		_ = a.server.Close() // 优雅关闭超时就直接断开：进程马上要退出了
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
