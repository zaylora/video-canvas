package agent

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/llmgateway"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider"
)

// ModelPurposeSummary 是 Agent 为压缩上下文发起的摘要调用：照常预检预算、计费，
// 但它不是助手对用户说的话。
const ModelPurposeSummary = "summary"

// ModelPurposeTitle 是 Agent 给会话起标题发起的调用：用用户当前选的模型，照常预检预算、计费，
// 同样不是助手对用户说的话，也不算对话的上下文大小。
const ModelPurposeTitle = "title"

// ModelPurposeActivity 是 Agent 给一段连续的工具调用写一句说明发起的调用：用用户当前选的模型，
// 和会话命名一样照常预检预算、计费，不是助手对用户说的话，也不算对话的上下文大小。
const ModelPurposeActivity = "activity"

// ModelPurposeVideo 标记视频理解子请求：照常计费，不计入主对话的上下文大小。
const ModelPurposeVideo = "video"

// ModelPurposeOf 把请求头 X-Agent-Purpose 的值转成已知的用途；只认已知的，别的值一律当普通对话（返回空串），
// 免得被用来藏起助手的回复。
func ModelPurposeOf(header string) string {
	switch header {
	case ModelPurposeSummary, ModelPurposeTitle, ModelPurposeActivity:
		return header
	default:
		return ""
	}
}

type modelPurposeKey struct{}

// WithModelPurpose 标明这次模型调用的用途；没标明就是普通的对话调用。
func WithModelPurpose(ctx context.Context, purpose string) context.Context {
	return context.WithValue(ctx, modelPurposeKey{}, purpose)
}

// ModelPurposeFrom 取模型调用的用途，没有返回空串。
func ModelPurposeFrom(ctx context.Context) string {
	p, _ := ctx.Value(modelPurposeKey{}).(string)
	return p
}

// ModelStreamer 是网关的透传入口（*llmgateway.Gateway 实现它）。
type ModelStreamer interface {
	// Stream 发送宿主组装的多模态请求，返回完整文本与用量。
	Stream(ctx context.Context, tg llmgateway.Target, req llmgateway.Request, emit func(llmgateway.Event)) (*llmgateway.Result, error)
	// StreamRaw 把调用方拼好的 OpenAI 请求体收紧后发往渠道，响应的 SSE 行原样回调给 raw。
	StreamRaw(ctx context.Context, tg llmgateway.Target, body []byte, lim llmgateway.RawLimits, emit func(llmgateway.Event), raw func([]byte)) (*llmgateway.Result, error)
}

// BridgeDeps 是创建 AgentBridge 需要的依赖。
type BridgeDeps struct {
	Repo       AgentRepo               // 运行、会话的数据访问
	Canvas     *AgentCanvasService     // 读写画布
	Agent      *AgentService           // 审批规则、画布和消息引用的校验
	Billing    *AgentBilling           // 对话计费
	Registry   provider.Registry       // 模型注册表
	Tasks      AgentGenTasks           // 生成任务（task_get 查进度）
	Assets     provider.AssetStore     // 素材（看图、看视频）
	AssetLinks AssetLinker             // 素材的稳定访问地址（附件落库、落画布用）；为 nil 时用 /files/<key>
	Frames     VideoFrames             // 抽帧、截帧；为 nil 时不能抽帧，原视频理解不依赖它
	FrameStore FrameStore              // 把截出的帧存成用户素材；为 nil 时截帧返回「不能处理视频」
	Secrets    provider.SecretResolver // 渠道 Key
	Streamer   ModelStreamer           // 大模型网关
	Skills     SkillLibrary            // 技能库（内置 + 后台已启用）；为 nil 时只有内置技能
	Web        provider.WebAccess      // 联网（搜索、抓网页）；为 nil 表示没有联网能力，联网工具返回「未启用」
	// RunUsage 按调用记录汇总一个运行的用量（模型代理要用它算本轮已花的积分）；为 nil 时模型代理不可用
	RunUsage RunUsageSource
	Now      func() time.Time // 为 nil 取系统时间
}

// bridgeSession 是一个运行在平台这边的内存状态：任务模式、选中的节点、已固定的技能版本、联网次数。
type bridgeSession struct {
	runID, userID, sessionID, canvasID uint64
	modelKey, mode                     string
	selection                          []string
	skillPins                          map[string]uint64 // 技能名 → 这次运行已固定的版本 id；只在内存里
	webCalls                           atomic.Int32      // 这个运行里联网工具已调用的次数（搜索和抓取共用上限）
}

// AgentBridge 是 Agent 服务调用 Go 平台能力的实现（平台 API，见 platform*.go）：模型代理和计费、画布读写、
// 工具、审批的预估和执行、运行准备、观测留存。密钥、计费、画布写入都留在 Go；会话、运行、审批的状态在 Agent 服务里。
type AgentBridge struct {
	d  BridgeDeps
	mu sync.Mutex
	// platformRuns 是每个运行在内存里的状态（联网次数、技能固定版本）：按运行 id 存；见 platform.go。受 mu 保护
	platformRuns map[uint64]*platformRun
	// toolRec 逐条留存工具调用；为 nil 时不记。装配阶段用 SetToolRecorder 绑定
	toolRec ToolCallRecorder
	// reqRec 留存收紧后的 OpenAI 形状请求体（Gemini 翻译前）；为 nil 时不记。装配阶段用 SetModelRequestRecorder 绑定
	reqRec ModelRequestRecorder
	// disabledTools 是配置里停用的工具名，下发工具声明时去掉；装配阶段用 SetDisabledTools 绑定
	disabledTools []string
}

// SetDisabledTools 设置停用的工具名（配置项），PrepareRun 下发工具声明时去掉它们，并把名单告诉 Agent（它自己的工具靠名单决定启用与否）。
func (b *AgentBridge) SetDisabledTools(names []string) { b.disabledTools = slices.Clone(names) }

// NewAgentBridge 创建桥。
func NewAgentBridge(d BridgeDeps) *AgentBridge {
	if d.Now == nil {
		d.Now = time.Now
	}
	// 画布服务要按模型支持的生成方式校验 mode、展示节点当前的方式：来源是模型注册表
	if d.Canvas != nil && d.Registry != nil {
		d.Canvas.SetModelOps(registryModelOps(d.Registry))
	}
	return &AgentBridge{d: d, platformRuns: map[uint64]*platformRun{}}
}

// resolveModel 取模型当前发布版本的快照和渠道 Key。不可用、不是 agent 种类、Key 没设置，都按「Agent 模型不可用」处理。
func (b *AgentBridge) resolveModel(ctx context.Context, modelKey string) (*provider.Snapshot, string, error) {
	snap, err := b.d.Registry.Snapshot(ctx, modelKey)
	if errors.Is(err, provider.ErrModelUnavailable) {
		return nil, "", errcode.ErrAgentModelNA
	}
	if err != nil {
		return nil, "", err
	}
	if snap.Model.Kind != "agent" {
		return nil, "", errcode.ErrAgentModelNA
	}
	key, err := b.d.Secrets.Get(ctx, model.ChannelSecretName(snap.Channel.Key))
	if err != nil {
		logger.Warn("Agent 渠道 Key 读取失败", zap.String("channel", snap.Channel.Key), zap.Error(err))
		return nil, "", errcode.ErrAgentModelNA
	}
	return snap, key, nil
}

// firstNonEmpty 返回第一个非空字符串，都为空返回空串。
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
