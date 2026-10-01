package service

import (
	"context"
	"errors"
	"time"

	"gorm.io/datatypes"

	"video-canvas/internal/config"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/repository"
)

// GenerationTaskRepo 是生成任务与积分的数据访问接口（真实实现是 repository.GenerationTaskRepository）。
// 事务内可用的原语来自 repository.GenerationTaskTx；WithTx 把它们绑定到同一个数据库事务，
// 状态迁移与积分变更的组合逻辑（业务规则）全部写在本层。
type GenerationTaskRepo interface {
	repository.GenerationTaskTx
	// WithTx 在一个事务里执行 fn，fn 返回错误则整体回滚。
	WithTx(ctx context.Context, fn func(tx repository.GenerationTaskTx) error) error
	// GetByID 按 id + user_id 查询任务（含 is_test），不存在或不属于该用户返回 repository.ErrNotFound。
	GetByID(ctx context.Context, userID, id uint64) (*model.GenerationTask, error)
	// ListByIDs 批量查询该用户的正式任务（排除 is_test）。
	ListByIDs(ctx context.Context, userID uint64, ids []uint64) ([]model.GenerationTask, error)
	// ListActive 查询该用户所有非终态的正式任务（排除 is_test）。
	ListActive(ctx context.Context, userID uint64) ([]model.GenerationTask, error)
	// GetCredit 读取积分账户，不存在返回 repository.ErrNotFound。
	GetCredit(ctx context.Context, userID uint64) (*model.UserCredit, error)
	// ClaimDue 领取到期且租约已过期的非终态任务并写租约（worker 用）。
	ClaimDue(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]model.GenerationTask, error)
	// ExtendLease 续租（worker 长操作期间的心跳），任务已终态时返回 false。
	ExtendLease(ctx context.Context, id uint64, until time.Time) (bool, error)
	// SaveTrace 写入试跑任务的执行追踪（只更新 is_test 任务），任务不存在或不是试跑任务返回 repository.ErrNotFound。
	SaveTrace(ctx context.Context, id uint64, trace datatypes.JSON) error
}

// 任务的统一错误码（error_code 字段）与给用户看的文案。上游原始错误信息只进日志，绝不透给用户。
const (
	TaskErrCanceled = "canceled"
	TaskErrTimeout  = "timeout"

	taskMsgCanceled = "已取消，积分已退回"
	taskMsgTimeout  = "生成超时，积分已退回"
)

const (
	defaultTaskDeadline      = 30 * time.Minute // 模型没配置 deadline 时的默认超时
	defaultMaxActiveTasks    = 4                // 配置缺省时的并发上限
	maxReconcileIDs          = 100              // 批量对账一次最多查多少个任务
	maxIdempotencyKeyLen     = 128              // 与 generation_tasks.idempotency_key 列宽一致
	executorCancelTimeout    = 5 * time.Second  // 取消时“尽力通知上游”的超时
	taskUserVisibleErrPrefix = "生成参数不合法："
	taskProgressDone         = 100     // 进入 finalizing 时的进度：上游已出结果，只剩转存
	syncTaskIDPrefix         = "sync-" // 同步接口没有上游任务 id 时的占位前缀（sync-<任务 id>）
	emptyJSONArray           = "[]"    // outputs / provider_result 为空时写入的 JSON，前端与 worker 不用处理 null
	taskFieldProviderState   = "provider_state"
)

// 用于让事务回滚的内部哨兵：提交时命中了幂等唯一索引（并发的同 key 请求先提交了）。
var errIdempotentConflict = errors.New("idempotent conflict")

// GenerationTaskDeps 是任务服务的依赖，全部以接口注入。
type GenerationTaskDeps struct {
	Repo        GenerationTaskRepo
	Registry    provider.Registry
	Executor    provider.Executor
	Assets      provider.AssetStore
	Broadcaster ws.Broadcaster // 为 nil 时不推送
	Config      config.AI
}

// GenerationTaskOption 用于替换可注入的部分（主要给测试用）。
type GenerationTaskOption func(*GenerationTaskService)

// WithInputValidator 替换输入校验函数，默认 modelcfg.ValidateInput。
func WithInputValidator(fn func(kind string, caps modelcfg.Capabilities, input map[string]any) (map[string]any, []modelcfg.FieldError)) GenerationTaskOption {
	return func(s *GenerationTaskService) { s.validateInput = fn }
}

// WithTaskClock 替换时间来源（测试用）。
func WithTaskClock(now func() time.Time) GenerationTaskOption {
	return func(s *GenerationTaskService) { s.now = now }
}

// GenerationTaskService 负责任务的提交、取消、查询、状态迁移与积分事务，并实现 provider.TaskStore。
// worker（internal/provider/worker）只通过本服务导出的迁移方法改变任务状态，自己不碰事务和积分。
type GenerationTaskService struct {
	repo        GenerationTaskRepo
	registry    provider.Registry
	executor    provider.Executor
	assets      provider.AssetStore
	broadcaster ws.Broadcaster
	cfg         config.AI

	kick chan struct{} // 进程内信号：有新任务 / 需要立即处理时唤醒 worker

	validateInput func(kind string, caps modelcfg.Capabilities, input map[string]any) (map[string]any, []modelcfg.FieldError)
	now           func() time.Time
}

// 编译期确认：任务服务满足 worker 的存储契约与试跑任务接口，真实仓储满足本服务声明的仓储接口。
var (
	_ provider.TaskStore = (*GenerationTaskService)(nil)
	_ TestTaskCreator    = (*GenerationTaskService)(nil)
	_ GenerationTaskRepo = (*repository.GenerationTaskRepository)(nil)
)

// NewGenerationTaskService 创建任务服务；Broadcaster 为 nil 时换成空实现，调用方不用判空。
func NewGenerationTaskService(deps GenerationTaskDeps, opts ...GenerationTaskOption) *GenerationTaskService {
	s := &GenerationTaskService{
		repo:          deps.Repo,
		registry:      deps.Registry,
		executor:      deps.Executor,
		assets:        deps.Assets,
		broadcaster:   deps.Broadcaster,
		cfg:           deps.Config,
		kick:          make(chan struct{}, 1),
		validateInput: modelcfg.ValidateInput,
		now:           time.Now,
	}
	if s.broadcaster == nil {
		s.broadcaster = ws.NopBroadcaster{}
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// KickChan 返回唤醒 worker 的 channel（容量 1，发送不阻塞），组装 worker 时传入。
func (s *GenerationTaskService) KickChan() <-chan struct{} { return s.kick }

// Kick 唤醒 worker 立即执行一次调度。channel 已有未消费的信号时直接丢弃：一次调度会处理所有到期任务。
func (s *GenerationTaskService) Kick() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// maxActiveTasks 返回每个用户进行中任务的上限，配置缺省时用 defaultMaxActiveTasks。
func (s *GenerationTaskService) maxActiveTasks() int {
	if s.cfg.MaxActiveTasksPerUser > 0 {
		return s.cfg.MaxActiveTasksPerUser
	}
	return defaultMaxActiveTasks
}

// publish 在事务提交之后向任务所属用户推送最新快照；is_test 任务不推送。
func (s *GenerationTaskService) publish(ctx context.Context, t *model.GenerationTask) {
	if t == nil || t.IsTest {
		return
	}
	channel := ws.UserChannel(t.UserID)
	s.broadcaster.Publish(ctx, channel, ws.Message{
		Type:    ws.TypeTaskUpdated,
		Channel: channel,
		Data:    taskView(t),
	})
}
