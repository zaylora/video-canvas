package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/videoframe"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
	"video-canvas/internal/stepkit"
)

// 营销工作室的运行参数。
const (
	// MarketingRefMaxSeconds 是参考视频时长上限（秒）：模型能力里没有时长字段，按 Seedance 2.x 的上限定（契约变更记录 9）。
	MarketingRefMaxSeconds = 15
	// marketingDefaultDuration 是成片时长参数缺省时的取值（秒）。
	marketingDefaultDuration = 15
	// marketingProjectPlaceholder 是事实卡出来之前的项目标题。
	marketingProjectPlaceholder = "未命名商品"
	// marketingStepTimeout 是单个环节（含重写一次）的最长执行时间。
	marketingStepTimeout = 8 * time.Minute
	// marketingCancelPoll 是环节运行中检查轮次是否被取消的间隔。
	marketingCancelPoll = 2 * time.Second
	// marketingMaxInlineVideo 是视频内联给模型的字节上限，与 Agent 视频理解一致。
	marketingMaxInlineVideo = 20 << 20
	// marketingMaxVideoSpool 是本地存储下视频落临时文件交给 ffmpeg 的字节上限。
	marketingMaxVideoSpool = 512 << 20
	// marketingMaxImageBytes 是单张商品图读入内存的上限。
	marketingMaxImageBytes = 20 << 20
	// marketingFramesWarning 是拆解退回截帧时给轮次的提示：没有声音。
	marketingFramesWarning = "拆解未包含声音，台词按画面推断"
	// marketingFrameEdge 是退回截帧时每帧的长边（像素）。
	marketingFrameEdge = 768
	// marketingPageSize 是项目列表缺省每页条数。
	marketingPageSize = 20
)

// MarketingRepo 是营销服务依赖的数据访问：事务原语加列表、统计和聚合查询，由 repository.MarketingRepository 实现。
type MarketingRepo interface {
	repository.MarketingTx
	// WithTx 执行营销事务。
	WithTx(ctx context.Context, fn func(repository.MarketingTx) error) error
	ListProjects(ctx context.Context, userID uint64, keyword string, page, size int) ([]model.MarketingProject, int64, error)
	ScriptCounts(ctx context.Context, projectIDs []uint64) ([]repository.MarketingStatusCount, error)
	ScriptBars(ctx context.Context, projectIDs []uint64) ([]repository.MarketingBar, error)
	RoundBriefs(ctx context.Context, projectIDs []uint64) (scripting, latest []repository.MarketingRoundBrief, err error)
	ActiveProjectTasks(ctx context.Context, userID uint64, nodeIDs []string) (int64, error)
	ProjectScriptIDs(ctx context.Context, userID, projectID uint64) ([]uint64, error)
	CoverKeys(ctx context.Context, userID uint64, assetIDs []uint64) (map[uint64]string, error)
	FailRate7d(ctx context.Context, stepKey string, since time.Time) (float64, error)
}

// MarketingAssets 是营销服务读取素材的入口（素材服务实现）。
type MarketingAssets interface {
	// View 返回素材视图，不存在或不属于该用户返回 errcode.ErrAssetNotFound。
	View(ctx context.Context, userID, id uint64) (*model.AssetView, error)
	// Get 返回素材元数据，不存在或不属于该用户返回 provider.ErrAssetNotFound。
	Get(ctx context.Context, userID, assetID uint64) (*model.Asset, error)
	// Open 打开素材内容。
	Open(ctx context.Context, userID, assetID uint64) (*provider.AssetFile, error)
	// FileURL 把存储 key 转成稳定访问地址。
	FileURL(key string) string
}

// MarketingTaskCreator 创建出片的生成任务（生成任务服务实现）。
type MarketingTaskCreator interface {
	Create(ctx context.Context, userID uint64, idempotencyKey string, req *model.CreateGenerationTaskReq) (*model.CreateGenerationTaskResp, error)
}

// MarketingCanvasCreator 新建画布（画布服务实现）。
type MarketingCanvasCreator interface {
	Create(ctx context.Context, userID uint64, req *model.CreateCanvasProjectReq) (*model.CanvasProject, error)
}

// MarketingFrames 是视频处理能力（videoframe.Extractor 实现）；可为 nil。
type MarketingFrames interface {
	Available() bool
	Duration(ctx context.Context, src string) (float64, error)
	Cuts(ctx context.Context, src string, threshold float64) ([]float64, error)
	FramesAt(ctx context.Context, src string, times []float64, o videoframe.FrameOpts) ([]videoframe.Frame, error)
}

// MarketingDeps 是营销服务的依赖，全部以接口注入。
type MarketingDeps struct {
	Repo        MarketingRepo
	Steps       *stepkit.Registry    // 环节注册表（已登记营销四环节）
	Resolver    *stepkit.Resolver    // 取环节当前生效的方法
	Caller      stepkit.Caller       // 调用文本模型
	Models      provider.Registry    // 文本模型与视频模型
	Assets      MarketingAssets      // 素材
	Tasks       MarketingTaskCreator // 出片任务
	Canvases    MarketingCanvasCreator
	Frames      MarketingFrames  // 截帧与镜头切点；可为 nil
	Credits     StepCreditPolicy // 新账户初始积分
	Broadcaster ws.Broadcaster   // 为 nil 时不推送
	Now         func() time.Time // 时钟，默认 time.Now
}

// MarketingService 是营销工作室业务：项目与轮次、环节编排（River job）、拆解与脚本编辑、出片、发到画布。
type MarketingService struct {
	repo     MarketingRepo
	steps    *stepkit.Registry
	resolver *stepkit.Resolver
	runner   *stepkit.Runner
	models   provider.Registry
	assets   MarketingAssets
	tasks    MarketingTaskCreator
	canvases MarketingCanvasCreator
	frames   MarketingFrames
	credits  StepCreditPolicy
	bc       ws.Broadcaster
	now      func() time.Time
}

// NewMarketingService 创建营销服务；Broadcaster 为 nil 时换成空实现。
func NewMarketingService(d MarketingDeps) *MarketingService {
	s := &MarketingService{repo: d.Repo, steps: d.Steps, resolver: d.Resolver, runner: stepkit.NewRunner(d.Caller), models: d.Models,
		assets: d.Assets, tasks: d.Tasks, canvases: d.Canvases, frames: d.Frames, credits: d.Credits, bc: d.Broadcaster, now: d.Now}
	if s.bc == nil {
		s.bc = ws.NopBroadcaster{}
	}
	if s.now == nil {
		s.now = time.Now
	}
	return s
}

// MarketingStats 把营销环节运行记录接成环节后台的失败率统计（StepAdminDeps.Stats）。
type MarketingStats struct {
	repo interface {
		FailRate7d(ctx context.Context, stepKey string, since time.Time) (float64, error)
	}
	now func() time.Time
}

// NewMarketingStats 创建失败率统计；now 为空用 time.Now。
func NewMarketingStats(repo interface {
	FailRate7d(ctx context.Context, stepKey string, since time.Time) (float64, error)
}, now func() time.Time) *MarketingStats {
	if now == nil {
		now = time.Now
	}
	return &MarketingStats{repo: repo, now: now}
}

// FailRate7d 实现 StepStats：营销环节读 marketing_step_runs，其他环节（短剧等）没有运行记录，按 0 返回。
func (m *MarketingStats) FailRate7d(ctx context.Context, stepKey string) (float64, error) {
	if len(stepKey) < len("marketing.") || stepKey[:len("marketing.")] != "marketing." {
		return 0, nil
	}
	return m.repo.FailRate7d(ctx, stepKey, m.now().Add(-7*24*time.Hour))
}

// 编译期确认：失败率统计满足环节后台的统计接口。
var _ StepStats = (*MarketingStats)(nil)

// hexID 把主键编码成对外的十六进制串。
func hexID(id uint64) idcodec.ID { return idcodec.ID(id) }

// marketingNode 是脚本出片任务的节点 id。
func marketingNode(scriptID uint64) string {
	return model.MarketingNodePrefix + idcodec.Encode(scriptID)
}

// jsonEqual 按 JSON 语义比较两段 JSON（忽略键序与空白）；任一为空按「都为空才相等」处理。
func sameJSON(a, b []byte) bool {
	if len(a) == 0 || len(b) == 0 {
		return len(a) == 0 && len(b) == 0
	}
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}

// mktNotFound 判断仓储的「不存在」。
func mktNotFound(err error) bool { return errors.Is(err, repository.ErrNotFound) }
