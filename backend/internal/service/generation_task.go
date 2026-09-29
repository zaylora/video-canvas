package service

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/datatypes"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/dsl"
	"video-canvas/internal/config"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/pkg/ws"
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
	// TouchByProviderTask 把某个平台任务对应的非终态任务的 next_poll_at 设为 now，返回命中行数（webhook 用）。
	TouchByProviderTask(ctx context.Context, provider, providerTaskID string, now time.Time) (int64, error)
}

// 任务的统一错误码（error_code 字段）与给用户看的文案。平台原始错误信息只进日志，绝不透给用户。
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
	executorCancelTimeout    = 5 * time.Second  // 取消时“尽力通知平台”的超时
	taskUserVisibleErrPrefix = "生成参数不合法："
	webhookPathPrefix        = "/api/v1/webhooks/"
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

// WithInputValidator 替换输入校验函数，默认 dsl.ValidateInput。
func WithInputValidator(fn func(dsl.InputSchema, map[string]any) (map[string]any, []dsl.FieldError)) GenerationTaskOption {
	return func(s *GenerationTaskService) { s.validateInput = fn }
}

// WithMediaFieldNames 替换“取媒体字段名”函数，默认 dsl.MediaFieldNames。
func WithMediaFieldNames(fn func(dsl.InputSchema) []string) GenerationTaskOption {
	return func(s *GenerationTaskService) { s.mediaFields = fn }
}

// WithExprEvaluator 替换表达式求值函数，默认 dsl.EvalExpr（webhook 提取平台任务 id 用）。
func WithExprEvaluator(fn func(string, *dsl.RenderContext) (any, error)) GenerationTaskOption {
	return func(s *GenerationTaskService) { s.evalExpr = fn }
}

// WithTaskClock 替换时间来源（测试用）。
func WithTaskClock(now func() time.Time) GenerationTaskOption {
	return func(s *GenerationTaskService) { s.now = now }
}

// GenerationTaskService 负责任务的提交、取消、查询、状态迁移与积分事务。
// worker（internal/provider/worker）只通过本服务导出的迁移方法改变任务状态，自己不碰事务和积分。
type GenerationTaskService struct {
	repo        GenerationTaskRepo
	registry    provider.Registry
	executor    provider.Executor
	assets      provider.AssetStore
	broadcaster ws.Broadcaster
	cfg         config.AI

	kick chan struct{} // 进程内信号：有新任务 / 需要立即处理时唤醒 worker

	validateInput func(dsl.InputSchema, map[string]any) (map[string]any, []dsl.FieldError)
	mediaFields   func(dsl.InputSchema) []string
	evalExpr      func(string, *dsl.RenderContext) (any, error)
	now           func() time.Time
}

func NewGenerationTaskService(deps GenerationTaskDeps, opts ...GenerationTaskOption) *GenerationTaskService {
	s := &GenerationTaskService{
		repo:          deps.Repo,
		registry:      deps.Registry,
		executor:      deps.Executor,
		assets:        deps.Assets,
		broadcaster:   deps.Broadcaster,
		cfg:           deps.Config,
		kick:          make(chan struct{}, 1),
		validateInput: dsl.ValidateInput,
		mediaFields:   dsl.MediaFieldNames,
		evalExpr:      dsl.EvalExpr,
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

func (s *GenerationTaskService) maxActiveTasks() int {
	if s.cfg.MaxActiveTasksPerUser > 0 {
		return s.cfg.MaxActiveTasksPerUser
	}
	return defaultMaxActiveTasks
}

// ---------------------------------------------------------------------------
// 提交
// ---------------------------------------------------------------------------

// Create 提交生成任务：冻结快照 → 校验输入与素材 → 单个事务内冻结积分并插入 pending 任务。
// idempotencyKey 非空时同一 (用户, key) 只会创建一个任务，重复提交返回已存在的任务且不再冻结积分。
// 返回的任务处于 pending，由 worker 异步提交给平台，接口本身不依赖平台响应速度。
func (s *GenerationTaskService) Create(ctx context.Context, userID uint64, idempotencyKey string, req *model.CreateGenerationTaskReq) (*model.GenerationTaskView, error) {
	// 1. 校验幂等键长度：超过列宽会在插入时才失败，提前按参数错误返回
	if len(idempotencyKey) > maxIdempotencyKeyLen {
		return nil, errcode.ErrInvalidParams.WithMsg("Idempotency-Key 过长")
	}

	// 2. 幂等快速路径：同一个 key 已经创建过任务，直接返回它，不再做任何校验和冻结
	//    （即使模型此后被下线，重复请求也应得到第一次的结果）
	if idempotencyKey != "" {
		existing, err := s.repo.FindByIdempotencyKey(ctx, userID, idempotencyKey)
		if err == nil {
			return taskView(existing), nil
		}
		if !errors.Is(err, repository.ErrNotFound) {
			return nil, err
		}
	}

	// 3. 冻结模型快照：任务此后只按快照执行，运营改配置不影响进行中的任务。
	//    模型不存在 / 未发布 / 已下线统一返回“模型不可用”
	snap, err := s.registry.Snapshot(ctx, req.ModelID)
	if errors.Is(err, provider.ErrModelUnavailable) {
		return nil, errcode.ErrModelUnavailable
	}
	if err != nil {
		return nil, err
	}

	// 4. 模型的种类必须与请求一致，避免拿视频模型提交图片任务
	if snap.Model.Kind != req.Kind {
		return nil, errcode.ErrTaskInput.WithMsg(taskUserVisibleErrPrefix + "模型的生成种类与请求不一致")
	}

	// 5. 按 input_schema 校验并规范化输入（补默认值、类型转换、必填/枚举/长度），
	//    再校验媒体字段引用的素材都属于当前用户
	input, err := s.prepareInput(ctx, userID, snap, req.Input)
	if err != nil {
		return nil, err
	}
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	snapJSON, err := json.Marshal(snap)
	if err != nil {
		return nil, err
	}

	// 6. 组装任务：deadline 取模型配置，缺省 30 分钟；next_poll_at = 现在，让 worker 立即领取
	now := s.now()
	credits := snap.Model.Credits
	if credits < 0 {
		credits = 0
	}
	task := &model.GenerationTask{
		UserID:         userID,
		NodeID:         req.NodeID,
		Kind:           req.Kind,
		ModelKey:       req.ModelID,
		Provider:       providerKeyOf(snap),
		Status:         model.TaskPending,
		InputJSON:      datatypes.JSON(inputJSON),
		ConfigSnapshot: datatypes.JSON(snapJSON),
		Credits:        credits,
		Version:        1,
		IdempotencyKey: idempotencyKey,
		NextPollAt:     now,
		DeadlineAt:     now.Add(taskDeadline(snap)),
	}
	if req.CanvasID > 0 {
		canvasID := req.CanvasID
		task.CanvasProjectID = &canvasID
	}

	// 7. 单个事务：锁账户 → 幂等复查 → 校验余额与并发上限 → 插入任务 → 冻结积分 → 写 freeze 流水。
	//    任何一步失败整体回滚，不会出现“积分冻结了但任务没创建”
	var existing *model.GenerationTask
	err = s.repo.WithTx(ctx, func(tx repository.GenerationTaskTx) error {
		// 7.1 惰性创建积分账户（新用户初始积分来自配置），再对账户行加锁：
		//     同一个用户的并发提交在这里被串行化，余额与并发数的检查因此不会被并发穿透
		if err := tx.EnsureCredit(ctx, userID, s.cfg.InitialCredits); err != nil {
			return err
		}
		acc, err := tx.LockCredit(ctx, userID)
		if err != nil {
			return err
		}

		// 7.2 加锁后再查一次幂等键：步骤 2 到这里之间可能已有同 key 的请求提交成功。
		//     命中就直接返回它，不冻结积分
		if idempotencyKey != "" {
			ex, err := tx.FindByIdempotencyKey(ctx, userID, idempotencyKey)
			if err == nil {
				existing = ex
				return nil
			}
			if !errors.Is(err, repository.ErrNotFound) {
				return err
			}
		}

		// 7.3 可用余额 = 余额 - 冻结，不足返回 402
		if acc.Balance-acc.Frozen < credits {
			return errcode.ErrInsufficientCredits
		}

		// 7.4 进行中的任务数不能超过上限，超过返回 429
		active, err := tx.CountActive(ctx, userID)
		if err != nil {
			return err
		}
		if active >= int64(s.maxActiveTasks()) {
			return errcode.ErrTooManyTasks
		}

		// 7.5 插入任务。即使前面的复查漏掉了（理论上不会），(user_id, idempotency_key) 唯一索引也会拦住：
		//     inserted=false 时回滚整个事务（此时还没动积分），由外层去取已存在的任务
		inserted, err := tx.InsertTask(ctx, task)
		if err != nil {
			return err
		}
		if !inserted {
			return errIdempotentConflict
		}

		// 7.6 冻结积分并写 freeze 流水（流水需要任务 id，所以在插入任务之后）
		if err := tx.AddCredit(ctx, userID, 0, credits); err != nil {
			return err
		}
		if _, err := tx.InsertLedger(ctx, &model.CreditLedger{
			UserID: userID, TaskID: task.ID, Type: model.LedgerFreeze, Amount: credits,
		}); err != nil {
			return err
		}
		return nil
	})

	// 8. 处理事务结果：幂等冲突取已存在的任务；其它错误原样返回（errcode 直接透传给前端）
	if errors.Is(err, errIdempotentConflict) {
		ex, ferr := s.repo.FindByIdempotencyKey(ctx, userID, idempotencyKey)
		if ferr != nil {
			return nil, ferr
		}
		return taskView(ex), nil
	}
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return taskView(existing), nil
	}

	// 9. 事务提交之后才唤醒 worker 和推送，避免对方读到还没提交的数据
	s.Kick()
	s.publish(ctx, task)
	return taskView(task), nil
}

// SubmitTest 创建运营试跑任务（管理端 test-run 用）：is_test=true，不冻结也不扣积分，
// 不校验并发上限，状态变化也不推送给普通用户。快照由调用者传入，这样可以用未发布的草稿配置试跑。
func (s *GenerationTaskService) SubmitTest(ctx context.Context, userID uint64, snap *dsl.Snapshot, input map[string]any) (*model.GenerationTaskView, error) {
	// 1. 快照必须有效
	if snap == nil {
		return nil, errcode.ErrInvalidParams.WithMsg("缺少试跑配置")
	}

	// 2. 与正式提交同样的输入校验与素材归属校验（素材必须属于发起试跑的管理员）
	normalized, err := s.prepareInput(ctx, userID, snap, input)
	if err != nil {
		return nil, err
	}
	inputJSON, err := json.Marshal(normalized)
	if err != nil {
		return nil, err
	}
	snapJSON, err := json.Marshal(snap)
	if err != nil {
		return nil, err
	}

	// 3. 插入任务：credits 记 0（不参与积分与对账），没有幂等键
	now := s.now()
	task := &model.GenerationTask{
		UserID:         userID,
		Kind:           snap.Model.Kind,
		ModelKey:       snap.Model.Key,
		Provider:       providerKeyOf(snap),
		Status:         model.TaskPending,
		InputJSON:      datatypes.JSON(inputJSON),
		ConfigSnapshot: datatypes.JSON(snapJSON),
		Credits:        0,
		Version:        1,
		IsTest:         true,
		NextPollAt:     now,
		DeadlineAt:     now.Add(taskDeadline(snap)),
	}
	if _, err := s.repo.InsertTask(ctx, task); err != nil {
		return nil, err
	}

	// 4. 唤醒 worker；不推送（is_test 任务对普通用户不可见，管理端自己轮询 GetTestTask）
	s.Kick()
	return taskView(task), nil
}

// prepareInput 校验并规范化输入，并确认媒体字段引用的素材都属于该用户。
// 字段级错误统一拼进 ErrTaskInput 的文案，返回 400。
func (s *GenerationTaskService) prepareInput(ctx context.Context, userID uint64, snap *dsl.Snapshot, raw map[string]any) (map[string]any, error) {
	// 1. 按 input_schema 校验（必填、枚举、长度、范围）并补默认值
	input, fieldErrs := s.validateInput(snap.Model.InputSchema, raw)
	if len(fieldErrs) > 0 {
		return nil, errcode.ErrTaskInput.WithMsg(taskUserVisibleErrPrefix + joinFieldErrors(fieldErrs))
	}

	// 2. 逐个校验媒体字段的素材：不存在 / 不属于自己统一按“素材不存在”处理，避免暴露别人的素材；
	//    素材种类还必须与字段类型一致（image 字段不能填视频素材）
	var assetErrs []dsl.FieldError
	for _, name := range s.mediaFields(snap.Model.InputSchema) {
		v, ok := input[name]
		if !ok || v == nil {
			continue // 可选的媒体字段没传
		}
		assetID, ok := toUint64(v)
		if !ok {
			assetErrs = append(assetErrs, dsl.FieldError{Field: name, Message: "素材 id 格式错误"})
			continue
		}
		asset, err := s.assets.Get(ctx, userID, assetID)
		if errors.Is(err, provider.ErrAssetNotFound) {
			assetErrs = append(assetErrs, dsl.FieldError{Field: name, Message: "素材不存在"})
			continue
		}
		if err != nil {
			return nil, err
		}
		if field, ok := snap.Model.InputSchema.Get(name); ok && asset.Kind != field.Type {
			assetErrs = append(assetErrs, dsl.FieldError{Field: name, Message: "素材类型与字段不匹配"})
		}
	}
	if len(assetErrs) > 0 {
		return nil, errcode.ErrTaskInput.WithMsg(taskUserVisibleErrPrefix + joinFieldErrors(assetErrs))
	}
	return input, nil
}

// ---------------------------------------------------------------------------
// 查询 / 对账 / 取消
// ---------------------------------------------------------------------------

// Get 查询当前用户的一个任务。别人的任务、不存在的任务、运营试跑任务统一返回“任务不存在”。
func (s *GenerationTaskService) Get(ctx context.Context, userID, id uint64) (*model.GenerationTaskView, error) {
	// 1. 按 id + user_id 查询；is_test 任务不对普通用户接口暴露，同样当作不存在
	t, err := s.repo.GetByID(ctx, userID, id)
	if errors.Is(err, repository.ErrNotFound) || (err == nil && t.IsTest) {
		return nil, errcode.ErrTaskNotFound
	}
	if err != nil {
		return nil, err
	}
	return taskView(t), nil
}

// GetTestTask 查询运营试跑任务（管理端轮询试跑进度）。只返回属于该用户的 is_test 任务。
func (s *GenerationTaskService) GetTestTask(ctx context.Context, userID, id uint64) (*model.GenerationTaskView, error) {
	t, err := s.repo.GetByID(ctx, userID, id)
	if errors.Is(err, repository.ErrNotFound) || (err == nil && !t.IsTest) {
		return nil, errcode.ErrTaskNotFound
	}
	if err != nil {
		return nil, err
	}
	return taskView(t), nil
}

// List 对账查询：ids=1,2,3（最多 100 个）或 status=active（该用户所有进行中的任务）二选一。
// 返回的切片永远不是 nil，只包含当前用户的正式任务。
func (s *GenerationTaskService) List(ctx context.Context, userID uint64, req *model.ListGenerationTaskReq) ([]model.GenerationTaskView, error) {
	// 1. ids 与 status 必须且只能传一个
	hasIDs, hasStatus := strings.TrimSpace(req.IDs) != "", req.Status != ""
	if hasIDs == hasStatus {
		return nil, errcode.ErrInvalidParams.WithMsg("ids 与 status=active 必须且只能传一个")
	}

	// 2. status=active：该用户所有非终态任务（WS 重连后对账）
	var (
		tasks []model.GenerationTask
		err   error
	)
	if hasStatus {
		tasks, err = s.repo.ListActive(ctx, userID)
	} else {
		// 3. ids：逐个解析，非法 / 超过 100 个返回 400；查询带 user_id，别人的任务查不到，直接不出现在结果里
		ids, perr := parseTaskIDs(req.IDs)
		if perr != nil {
			return nil, errcode.ErrInvalidParams.WithMsg(perr.Error())
		}
		tasks, err = s.repo.ListByIDs(ctx, userID, ids)
	}
	if err != nil {
		return nil, err
	}

	// 4. 转成视图；outputs 永远是数组
	views := make([]model.GenerationTaskView, 0, len(tasks))
	for i := range tasks {
		views = append(views, *taskView(&tasks[i]))
	}
	return views, nil
}

// Cancel 软取消：只有非终态任务能取消。取消与退回冻结积分在同一个事务里，
// 取消成功后尽力通知平台停止（失败只记日志，不影响取消结果），最后推送。
func (s *GenerationTaskService) Cancel(ctx context.Context, userID, id uint64) (*model.GenerationTaskView, error) {
	// 1. 查任务（带 user_id）：别人的、不存在的、试跑任务都返回“任务不存在”
	t, err := s.repo.GetByID(ctx, userID, id)
	if errors.Is(err, repository.ErrNotFound) || (err == nil && t.IsTest) {
		return nil, errcode.ErrTaskNotFound
	}
	if err != nil {
		return nil, err
	}

	// 2. 已经是终态不能取消，返回 409
	if model.IsTerminalStatus(t.Status) {
		return nil, errcode.ErrTaskNotCancelable
	}

	// 3. CAS 迁移到 canceled 并退回冻结积分（同一事务）。
	//    CAS 没命中说明上一步之后任务刚好走到了终态（成功 / 失败 / 超时），同样返回 409
	updated, applied, err := s.finish(ctx, id, model.ActiveTaskStatuses, model.TaskCanceled, map[string]any{
		"error_code":    TaskErrCanceled,
		"error_message": taskMsgCanceled,
	})
	if err != nil {
		return nil, err
	}
	if !applied {
		return nil, errcode.ErrTaskNotCancelable
	}

	// 4. 事务已提交：尽力通知平台取消。提交之前的任务（没有平台任务 id）无需通知
	s.cancelProvider(ctx, updated)

	// 5. 推送最新状态
	s.publish(ctx, updated)
	return taskView(updated), nil
}

// GetCredits 返回当前用户的积分。账户不存在时按配置的初始积分惰性创建后再返回。
func (s *GenerationTaskService) GetCredits(ctx context.Context, userID uint64) (*model.CreditView, error) {
	// 1. 惰性创建账户：已存在时是空操作，不会覆盖余额
	if err := s.repo.EnsureCredit(ctx, userID, s.cfg.InitialCredits); err != nil {
		return nil, err
	}
	// 2. 读取账户；可用 = 余额 - 冻结
	acc, err := s.repo.GetCredit(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &model.CreditView{Balance: acc.Balance, Frozen: acc.Frozen, Available: acc.Balance - acc.Frozen}, nil
}

// ---------------------------------------------------------------------------
// Webhook
// ---------------------------------------------------------------------------

// WebhookURL 返回注册给平台的回调地址；未配置公网地址或密钥时返回空串（纯轮询）。
func (s *GenerationTaskService) WebhookURL(providerKey string) string {
	if s.cfg.WebhookBaseURL == "" || s.cfg.WebhookSecret == "" {
		return ""
	}
	return strings.TrimRight(s.cfg.WebhookBaseURL, "/") + webhookPathPrefix + providerKey + "/" + s.cfg.WebhookSecret
}

// HandleWebhook 处理平台回调：只把对应任务的 next_poll_at 设为现在并唤醒 worker，结果仍以主动查询为准。
// 除了密钥不对（404）以外一律返回 nil：找不到任务、回调体无法解析都不报错，避免被人探测有哪些任务。
func (s *GenerationTaskService) HandleWebhook(ctx context.Context, providerKey, secret string, body []byte) error {
	// 1. 校验路径里的密钥：常量时间比较防止时序攻击。
	//    服务端没配置密钥时一律 404，等同于没有这个接口
	if s.cfg.WebhookSecret == "" || subtle.ConstantTimeCompare([]byte(secret), []byte(s.cfg.WebhookSecret)) != 1 {
		return errcode.ErrNotFound
	}

	// 2. 取平台配置里声明的“回调体 → 平台任务 id”表达式；平台不存在或没声明 webhook 就忽略
	provider, err := s.registry.Provider(ctx, providerKey)
	if err != nil || provider == nil || provider.Webhook == nil || provider.Webhook.TaskID == "" {
		return nil
	}

	// 3. 解析回调体。用 UseNumber：平台任务 id 可能是超过 2^53 的整数，float64 会丢精度。
	//    永远不信任回调内容，这里只取任务 id，不读任何状态字段
	var req any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&req); err != nil {
		return nil
	}
	v, err := s.evalExpr(provider.Webhook.TaskID, &dsl.RenderContext{Req: req})
	if err != nil || v == nil {
		return nil
	}
	providerTaskID := strings.TrimSpace(fmt.Sprint(v))
	if providerTaskID == "" {
		return nil
	}

	// 4. 把该任务的下一次查询提前到现在，并唤醒 worker；找不到任务（0 行）也不报错
	n, err := s.repo.TouchByProviderTask(ctx, providerKey, providerTaskID, s.now())
	if err != nil {
		// 数据库错误只记日志，仍返回 200：平台回调失败会重试，没必要；反正轮询兜底
		logger.Warn("处理平台回调失败", zap.String("provider", providerKey), zap.Error(err))
		return nil
	}
	if n > 0 {
		s.Kick()
	}
	return nil
}

// ---------------------------------------------------------------------------
// 状态迁移（worker 使用）
// 所有方法返回 (applied, err)：applied=false 表示 CAS 没命中，任务已被其它流程（取消 / 超时 / 别的实例）迁移，
// 调用方应放弃对该任务的后续处理，这不是错误。事务提交之后才推送。
// ---------------------------------------------------------------------------

// ClaimDue 领取最多 limit 个到期任务，并写入 now+lease 的租约。
func (s *GenerationTaskService) ClaimDue(ctx context.Context, limit int, lease time.Duration) ([]model.GenerationTask, error) {
	return s.repo.ClaimDue(ctx, s.now(), lease, limit)
}

// ExtendLease 续租，防止长时间操作（转存大文件）期间租约过期被别的实例重复领取。
func (s *GenerationTaskService) ExtendLease(ctx context.Context, id uint64, lease time.Duration) (bool, error) {
	return s.repo.ExtendLease(ctx, id, s.now().Add(lease))
}

// MarkSubmitted 平台受理成功：pending → queued，记录平台任务 id 与提交时间，安排第一次查询。
func (s *GenerationTaskService) MarkSubmitted(ctx context.Context, t *model.GenerationTask, providerTaskID string, nextPollAt time.Time) (bool, error) {
	// 1. CAS：只有仍是 pending 才迁移（期间被用户取消了就不迁移）；释放租约，poll_attempts 清零重新计数
	updated, applied, err := s.transit(ctx, t.ID, []string{model.TaskPending}, map[string]any{
		"status":           model.TaskQueued,
		"provider_task_id": providerTaskID,
		"submitted_at":     s.now(),
		"next_poll_at":     nextPollAt,
		"poll_attempts":    0,
		"lease_until":      nil,
	}, true)
	if err != nil || !applied {
		return false, err
	}
	// 2. 提交之后推送（用户看到“排队中”）
	s.publish(ctx, updated)
	return true, nil
}

// MarkPolled 记录一次成功的查询结果（平台仍在 queued / running）：更新状态与进度，安排下一次查询。
// 状态和进度都没变化时只调整调度字段，不 bump version、不推送。
func (s *GenerationTaskService) MarkPolled(ctx context.Context, t *model.GenerationTask, status string, progress *int, attempts int, nextPollAt time.Time) (bool, error) {
	// 1. 只允许迁到 queued / running
	if status != model.TaskQueued && status != model.TaskRunning {
		return false, fmt.Errorf("MarkPolled 不支持的状态：%s", status)
	}
	// 2. 平台没给进度（nil）时保留原进度，不覆盖成空；有变化才 bump version 并推送
	fields := map[string]any{
		"status":        status,
		"poll_attempts": attempts,
		"next_poll_at":  nextPollAt,
		"lease_until":   nil,
	}
	changed := status != t.Status
	if progress != nil {
		fields["progress"] = *progress
		if t.Progress == nil || *t.Progress != *progress {
			changed = true
		}
	}
	// 3. CAS：任务已被取消 / 超时则不再更新
	updated, applied, err := s.transit(ctx, t.ID, []string{model.TaskQueued, model.TaskRunning}, fields, changed)
	if err != nil || !applied {
		return false, err
	}
	if changed {
		s.publish(ctx, updated)
	}
	return true, nil
}

// MarkFinalizing 平台已出片：queued / running → finalizing，立即安排转存（next_poll_at = 现在并唤醒 worker）。
func (s *GenerationTaskService) MarkFinalizing(ctx context.Context, t *model.GenerationTask) (bool, error) {
	updated, applied, err := s.transit(ctx, t.ID, []string{model.TaskQueued, model.TaskRunning}, map[string]any{
		"status":        model.TaskFinalizing,
		"progress":      100,
		"poll_attempts": 0, // finalizing 阶段的 poll_attempts 用来计转存重试次数，重新从 0 开始
		"next_poll_at":  s.now(),
		"lease_until":   nil,
	}, true)
	if err != nil || !applied {
		return false, err
	}
	s.publish(ctx, updated)
	s.Kick()
	return true, nil
}

// Retry 一次可重试的失败：状态不变，记录累计重试次数并安排下一次处理时间，释放租约。
// 用户看不到的调度信息变化，不 bump version、不推送。
func (s *GenerationTaskService) Retry(ctx context.Context, t *model.GenerationTask, attempts int, nextPollAt time.Time) (bool, error) {
	_, applied, err := s.transit(ctx, t.ID, []string{t.Status}, map[string]any{
		"poll_attempts": attempts,
		"next_poll_at":  nextPollAt,
		"lease_until":   nil,
	}, false)
	return applied, err
}

// Complete 转存完成：finalizing → succeeded，写 output_json 与 finished_at，并结算积分（同一事务）。
func (s *GenerationTaskService) Complete(ctx context.Context, t *model.GenerationTask, outputs []model.TaskOutput) (bool, error) {
	// 1. outputs 永远序列化成数组（nil 也是 []），前端不用处理 null
	if outputs == nil {
		outputs = []model.TaskOutput{}
	}
	raw, err := json.Marshal(outputs)
	if err != nil {
		return false, err
	}
	// 2. 事务：CAS 置 succeeded + 结算积分（balance 和 frozen 各减 credits，写 settle 流水）
	updated, applied, err := s.finish(ctx, t.ID, []string{model.TaskFinalizing}, model.TaskSucceeded, map[string]any{
		"output_json":   datatypes.JSON(raw),
		"progress":      100,
		"error_code":    "",
		"error_message": "",
	})
	if err != nil || !applied {
		return false, err
	}
	s.publish(ctx, updated)
	return true, nil
}

// Fail 任务失败：任意非终态 → failed，errorCode 是统一错误码，message 是给用户看的文案，并退回冻结积分。
func (s *GenerationTaskService) Fail(ctx context.Context, t *model.GenerationTask, errorCode, message string) (bool, error) {
	updated, applied, err := s.finish(ctx, t.ID, model.ActiveTaskStatuses, model.TaskFailed, map[string]any{
		"error_code":    errorCode,
		"error_message": message,
	})
	if err != nil || !applied {
		return false, err
	}
	s.publish(ctx, updated)
	return true, nil
}

// Expire 任务超过 deadline_at：任意非终态 → expired，并退回冻结积分。平台侧的取消由调用方（worker）先尽力完成。
func (s *GenerationTaskService) Expire(ctx context.Context, t *model.GenerationTask) (bool, error) {
	updated, applied, err := s.finish(ctx, t.ID, model.ActiveTaskStatuses, model.TaskExpired, map[string]any{
		"error_code":    TaskErrTimeout,
		"error_message": taskMsgTimeout,
	})
	if err != nil || !applied {
		return false, err
	}
	s.publish(ctx, updated)
	return true, nil
}

// transit 是不涉及积分的状态迁移：单条 CAS 更新，命中返回更新后的整行。
// applied=false 表示状态已被别的流程改变（不是错误）。
func (s *GenerationTaskService) transit(ctx context.Context, id uint64, from []string, fields map[string]any, bumpVersion bool) (*model.GenerationTask, bool, error) {
	t, err := s.repo.UpdateIf(ctx, id, from, fields, bumpVersion)
	if errors.Is(err, repository.ErrStateConflict) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return t, true, nil
}

// finish 是迁移到终态并处理积分的单个事务，所有终态都走这里：
//   - 成功（succeeded）：balance -= credits，frozen -= credits，写 settle 流水
//   - 其它终态（failed / canceled / expired）：frozen -= credits，写 refund 流水
//
// 状态用 CAS 迁移，并发的重复迁移只有一个成功；(task_id, type) 唯一流水是第二道保险，
// 冲突表示这笔账已经处理过，不再改余额。流水与余额更新在同一个事务里，要么都生效要么都不生效。
// is_test 任务不动积分。事务提交后由调用方推送。
func (s *GenerationTaskService) finish(ctx context.Context, id uint64, from []string, target string, extra map[string]any) (*model.GenerationTask, bool, error) {
	// 1. 组装要更新的字段：终态、完成时间、释放租约（终态任务不会再被调度）
	fields := map[string]any{
		"status":      target,
		"finished_at": s.now(),
		"lease_until": nil,
	}
	for k, v := range extra {
		fields[k] = v
	}

	var updated *model.GenerationTask
	err := s.repo.WithTx(ctx, func(tx repository.GenerationTaskTx) error {
		// 2. CAS 迁移并 version+1；没命中返回 ErrStateConflict，事务回滚（此时没有任何改动）
		t, err := tx.UpdateIf(ctx, id, from, fields, true)
		if err != nil {
			return err
		}
		updated = t

		// 3. 试跑任务不冻结积分，也就没有什么可结算 / 退回的
		if t.IsTest {
			return nil
		}

		// 4. 写结算或退款流水：成功结算，其它退回
		ledgerType, dBalance := model.LedgerRefund, 0
		if target == model.TaskSucceeded {
			ledgerType, dBalance = model.LedgerSettle, -t.Credits
		}
		inserted, err := tx.InsertLedger(ctx, &model.CreditLedger{
			UserID: t.UserID, TaskID: t.ID, Type: ledgerType, Amount: t.Credits,
		})
		if err != nil {
			return err
		}
		if !inserted {
			// 流水已存在：这笔账之前处理过（重复执行），当作成功，不再改余额，避免重复扣款 / 退款
			logger.Warn("积分流水已存在，跳过重复结算", zap.Uint64("task_id", t.ID), zap.String("type", ledgerType))
			return nil
		}

		// 5. 更新账户：无论结算还是退回，冻结都要减掉；只有结算才扣余额
		return tx.AddCredit(ctx, t.UserID, dBalance, -t.Credits)
	})
	if errors.Is(err, repository.ErrStateConflict) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return updated, true, nil
}

// cancelProvider 尽力通知平台取消任务；平台不支持取消或调用失败都不影响任务已经取消的结果，只记日志。
func (s *GenerationTaskService) cancelProvider(ctx context.Context, t *model.GenerationTask) {
	if t.ProviderTaskID == "" {
		return // 还没提交给平台，没有什么可取消的
	}
	var snap dsl.Snapshot
	if err := json.Unmarshal(t.ConfigSnapshot, &snap); err != nil {
		logger.Warn("解析任务快照失败，跳过平台取消", zap.Uint64("task_id", t.ID), zap.Error(err))
		return
	}
	// 与请求的取消脱钩：客户端断开不应该中断通知平台
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), executorCancelTimeout)
	defer cancel()
	err := s.executor.Cancel(cctx, &snap, provider.TaskRef{ID: t.ID, UserID: t.UserID, ProviderTaskID: t.ProviderTaskID})
	if err != nil && !errors.Is(err, provider.ErrCancelUnsupported) {
		logger.Warn("通知平台取消任务失败", zap.Uint64("task_id", t.ID), zap.String("provider", t.Provider), zap.Error(err))
	}
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

// ---------------------------------------------------------------------------
// 辅助函数
// ---------------------------------------------------------------------------

// taskView 把任务行转成对外视图；outputs 从 output_json 解出，永远是数组而不是 null。
func taskView(t *model.GenerationTask) *model.GenerationTaskView {
	outputs := []model.TaskOutput{}
	if len(t.OutputJSON) > 0 {
		var decoded []model.TaskOutput
		if err := json.Unmarshal(t.OutputJSON, &decoded); err != nil {
			logger.Warn("解析任务 output_json 失败", zap.Uint64("task_id", t.ID), zap.Error(err))
		} else if decoded != nil {
			outputs = decoded
		}
	}
	return &model.GenerationTaskView{
		ID:              t.ID,
		CanvasProjectID: t.CanvasProjectID,
		NodeID:          t.NodeID,
		Kind:            t.Kind,
		ModelID:         t.ModelKey,
		Status:          t.Status,
		Progress:        t.Progress,
		Outputs:         outputs,
		ErrorCode:       t.ErrorCode,
		ErrorMessage:    t.ErrorMessage,
		Credits:         t.Credits,
		Version:         t.Version,
		DeadlineAt:      t.DeadlineAt,
		CreatedAt:       t.CreatedAt,
		FinishedAt:      t.FinishedAt,
	}
}

// providerKeyOf 取快照里的平台 key（优先平台配置本身，缺省用模型声明的 provider）。
func providerKeyOf(snap *dsl.Snapshot) string {
	if snap.Provider.Key != "" {
		return snap.Provider.Key
	}
	return snap.Model.Provider
}

// taskDeadline 取模型配置的超时时长，没配置时默认 30 分钟。
func taskDeadline(snap *dsl.Snapshot) time.Duration {
	if d := snap.Model.Deadline.D(); d > 0 {
		return d
	}
	return defaultTaskDeadline
}

// joinFieldErrors 把字段级错误拼成一句话：字段名：原因；字段名：原因。
func joinFieldErrors(errs []dsl.FieldError) string {
	parts := make([]string, 0, len(errs))
	for _, e := range errs {
		if e.Field == "" {
			parts = append(parts, e.Message)
			continue
		}
		parts = append(parts, e.Field+"："+e.Message)
	}
	return strings.Join(parts, "；")
}

// parseTaskIDs 解析逗号分隔的任务 id：不能为空项、必须是正整数、最多 maxReconcileIDs 个，重复的只保留一个。
func parseTaskIDs(raw string) ([]uint64, error) {
	parts := strings.Split(raw, ",")
	if len(parts) > maxReconcileIDs {
		return nil, fmt.Errorf("ids 最多 %d 个", maxReconcileIDs)
	}
	seen := make(map[uint64]struct{}, len(parts))
	ids := make([]uint64, 0, len(parts))
	for _, p := range parts {
		id, err := strconv.ParseUint(strings.TrimSpace(p), 10, 64)
		if err != nil || id == 0 {
			return nil, errors.New("ids 格式错误，应为逗号分隔的任务 id")
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, nil
}

// toUint64 把规范化后的素材 id 转成 uint64；兼容 dsl 可能返回的各种数字表示。
func toUint64(v any) (uint64, bool) {
	switch n := v.(type) {
	case uint64:
		return n, n > 0
	case uint:
		return uint64(n), n > 0
	case int:
		return uint64(n), n > 0
	case int64:
		return uint64(n), n > 0
	case float64:
		return uint64(n), n >= 1 && n == float64(uint64(n))
	case json.Number:
		id, err := strconv.ParseUint(n.String(), 10, 64)
		return id, err == nil && id > 0
	case string:
		id, err := strconv.ParseUint(strings.TrimSpace(n), 10, 64)
		return id, err == nil && id > 0
	}
	return 0, false
}

// 编译期确认真实仓储满足 service 声明的接口。
var _ GenerationTaskRepo = (*repository.GenerationTaskRepository)(nil)
