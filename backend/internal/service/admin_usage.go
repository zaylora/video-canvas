package service

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
	"video-canvas/pkg/pagination"
)

// 后台「使用记录」：全站生成任务的流水、摘要与详情，只读。admin 与 super_admin 同权（路由层控制）。

const (
	usageDefaultSpan = 7 * 24 * time.Hour  // 没传时间范围时的默认跨度
	usageMaxSpan     = 90 * 24 * time.Hour // 时间范围最大跨度：全站按状态 / 模型过滤没有组合索引，靠时间范围收窄扫描量
)

// taskRefPattern 匹配 32 位十六进制的任务编号（idcodec 编码后的长度）。
var taskRefPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// AdminUsageRepo 是使用记录的数据访问接口，由 repository.AdminUsageRepository 实现。
type AdminUsageRepo interface {
	// List 按筛选条件返回一页任务流水（Offset / Limit），id 倒序。
	List(ctx context.Context, f repository.UsageFilter) ([]model.UsageRow, error)
	// Count 返回满足筛选条件的总数，翻页用。
	Count(ctx context.Context, f repository.UsageFilter) (int64, error)
	// Summary 按状态分组统计任务数与实扣积分，筛选条件与 List 相同。
	Summary(ctx context.Context, f repository.UsageFilter) ([]model.UsageStatusCount, error)
	// GetTaskByRef 按任务编号取任务（含试跑），不存在返回 repository.ErrNotFound。
	GetTaskByRef(ctx context.Context, ref string) (*model.GenerationTask, error)
	// GetUser 按 id 取用户（含已软删除的），不存在返回 repository.ErrNotFound。
	GetUser(ctx context.Context, id uint64) (*model.User, error)
	// ListEvents 返回任务的状态时间线，id 升序；老任务没有事件，返回空切片。
	ListEvents(ctx context.Context, taskID uint64) ([]model.GenerationTaskEvent, error)
	// ListTaskLedger 返回任务的积分流水，id 升序。
	ListTaskLedger(ctx context.Context, taskID uint64) ([]model.CreditLedger, error)
}

// AdminUsageService 实现后台使用记录的查询。
type AdminUsageService struct {
	repo AdminUsageRepo
	now  func() time.Time
}

// AdminUsageOption 是 AdminUsageService 的可选配置。
type AdminUsageOption func(*AdminUsageService)

// WithUsageClock 替换时钟（测试用，固定“现在”以验证默认时间范围）。
func WithUsageClock(now func() time.Time) AdminUsageOption {
	return func(s *AdminUsageService) { s.now = now }
}

// NewAdminUsageService 创建使用记录服务。
func NewAdminUsageService(repo AdminUsageRepo, opts ...AdminUsageOption) *AdminUsageService {
	s := &AdminUsageService{repo: repo, now: time.Now}
	for _, o := range opts {
		o(s)
	}
	return s
}

// List 返回全站任务流水（page / page_size 分页，沿用其他后台列表的 {list,total,page,page_size} 契约），按 id 倒序。
func (s *AdminUsageService) List(ctx context.Context, req *model.ListUsageReq) (*pagination.Result[model.UsageItem], error) {
	// 1. 筛选条件：时间范围、状态映射、关键字拆分
	f, err := s.filter(&req.UsageFilterReq)
	if err != nil {
		return nil, err
	}
	// 2. 分页：页码最小 1，每页缺省 20、最多 100
	q := pagination.AdminQuery(req.Page, req.PageSize)
	f.Offset, f.Limit = q.Offset(), q.Limit()
	// 3. 查询当前页与满足条件的总数（两者共用同一份筛选）
	rows, err := s.repo.List(ctx, f)
	if err != nil {
		return nil, err
	}
	total, err := s.repo.Count(ctx, f)
	if err != nil {
		return nil, err
	}
	items := make([]model.UsageItem, len(rows)) // 永远不是 nil，序列化为 []
	for i, r := range rows {
		items[i] = usageItem(r)
	}
	return &pagination.Result[model.UsageItem]{List: items, Total: total, Page: q.Page, PageSize: q.PageSize}, nil
}

// usageItem 把仓储行转成响应行。
func usageItem(r model.UsageRow) model.UsageItem {
	return model.UsageItem{
		ID: r.ID, TaskRef: r.TaskRef, User: model.UsageUser{ID: r.UserID, Username: r.Username},
		Kind: r.Kind, ModelKey: r.ModelKey, ModelName: r.ModelName, Status: r.Status,
		Credits: r.Credits, ChargedCredits: r.ChargedCredits, ErrorCode: r.ErrorCode, ErrorMessage: r.ErrorMessage,
		DurationMs: r.DurationMs, IsTest: r.IsTest, CreatedAt: r.CreatedAt,
	}
}

// Summary 返回与列表同一组筛选条件下的任务数与实扣积分；failed 只含 failed，取消与超时各自计数。
func (s *AdminUsageService) Summary(ctx context.Context, req *model.UsageFilterReq) (*model.UsageSummary, error) {
	// 1. 与列表共用同一个筛选解析，保证摘要和列表口径一致
	f, err := s.filter(req)
	if err != nil {
		return nil, err
	}
	rows, err := s.repo.Summary(ctx, f)
	if err != nil {
		return nil, err
	}
	// 2. 按状态折成摘要：进行中的四种状态合并成 active
	sum := &model.UsageSummary{}
	for _, r := range rows {
		sum.Total += r.Count
		sum.ChargedCredits += r.Charged
		switch r.Status {
		case model.TaskSucceeded:
			sum.Succeeded += r.Count
		case model.TaskFailed:
			sum.Failed += r.Count
		case model.TaskCanceled:
			sum.Canceled += r.Count
		case model.TaskExpired:
			sum.Expired += r.Count
		default:
			sum.Active += r.Count
		}
	}
	return sum, nil
}

// filter 把请求参数解析成仓储的筛选条件。
func (s *AdminUsageService) filter(req *model.UsageFilterReq) (repository.UsageFilter, error) {
	// 1. 时间范围：缺 to 取现在，缺 from 取 to 往前 7 天；格式错误、from 不早于 to、跨度超过 90 天都是参数错误
	from, to, err := s.timeRange(req.From, req.To)
	if err != nil {
		return repository.UsageFilter{}, err
	}
	f := repository.UsageFilter{
		UserID: req.UserID, Kind: req.Kind, Model: req.Model, From: from, To: to, IncludeTest: req.IncludeTest,
	}
	// 2. 状态映射：all / 空不筛；active 是全部非终态；其余只筛自己（failed 不含取消与超时）
	switch req.Status {
	case "", "all":
	case "active":
		f.Statuses = model.ActiveTaskStatuses
	default:
		f.Statuses = []string{req.Status}
	}
	// 3. 关键字：32 位十六进制是任务编号（统一成小写精确匹配），否则当作用户名 / 邮箱片段
	q := strings.TrimSpace(req.Q)
	if ref := strings.ToLower(q); taskRefPattern.MatchString(ref) {
		f.TaskRef = ref
	} else {
		f.UserKeyword = q
	}
	return f, nil
}

// timeRange 解析并校验 from / to（RFC3339）。
func (s *AdminUsageService) timeRange(fromStr, toStr string) (from, to time.Time, err error) {
	to = s.now()
	if toStr != "" {
		if to, err = time.Parse(time.RFC3339, toStr); err != nil {
			return from, to, errcode.ErrInvalidParams.WithMsg("结束时间格式不正确，应为 RFC3339")
		}
	}
	from = to.Add(-usageDefaultSpan)
	if fromStr != "" {
		if from, err = time.Parse(time.RFC3339, fromStr); err != nil {
			return from, to, errcode.ErrInvalidParams.WithMsg("开始时间格式不正确，应为 RFC3339")
		}
	}
	if !from.Before(to) {
		return from, to, errcode.ErrInvalidParams.WithMsg("开始时间必须早于结束时间")
	}
	if to.Sub(from) > usageMaxSpan {
		return from, to, errcode.ErrInvalidParams.WithMsg("时间范围不能超过 90 天")
	}
	return from, to, nil
}

// Get 返回一个任务的完整详情：基础信息、用户、输入、产物、失败原因（含上游错误原文）、状态时间线、积分流水和试跑追踪。
func (s *AdminUsageService) Get(ctx context.Context, ref string) (*model.UsageDetail, error) {
	// 1. 按任务编号取任务（不限用户，含试跑）；编号不存在或格式不对都是“任务不存在”
	t, err := s.repo.GetTaskByRef(ctx, strings.ToLower(strings.TrimSpace(ref)))
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrTaskNotFound
	}
	if err != nil {
		return nil, err
	}
	// 2. 用户：已不存在时详情仍然可看（用户名为空，保留 id），任务不能因为用户被删而查不到
	user := model.UsageUserDetail{ID: t.UserID}
	u, err := s.repo.GetUser(ctx, t.UserID)
	switch {
	case err == nil:
		user = model.UsageUserDetail{ID: u.ID, Username: u.Username, Email: u.Email}
	case !errors.Is(err, repository.ErrNotFound):
		return nil, err
	}
	// 3. 状态时间线与积分流水
	events, err := s.repo.ListEvents(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	ledger, err := s.repo.ListTaskLedger(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	d := &model.UsageDetail{
		Task:    usageTask(t),
		User:    user,
		Input:   decodeObject(t.InputJSON),
		Outputs: decodeOutputs(t.OutputJSON),
		Events:  make([]model.UsageEvent, len(events)),
		Ledger:  make([]model.UsageLedger, len(ledger)),
	}
	for i, e := range events {
		d.Events[i] = model.UsageEvent{
			ID: e.ID, Event: e.Event, FromStatus: e.FromStatus, ToStatus: e.ToStatus,
			Detail: decodeNullableObject(e.DetailJSON), CreatedAt: e.CreatedAt,
		}
	}
	for i, l := range ledger {
		// 与用户抽屉的积分流水同一口径：freeze / settle 为负，refund 为正
		d.Ledger[i] = model.UsageLedger{ID: l.ID, Type: l.Type, Amount: signedLedgerAmount(l.Type, l.Amount), CreatedAt: l.CreatedAt}
	}
	// 4. 失败信息：用户文案来自任务；上游错误原文来自失败类事件（宿主已遮掉渠道 Key），没有就是 null
	d.Error = model.UsageError{Code: t.ErrorCode, Message: t.ErrorMessage, Upstream: upstreamOf(d.Events)}
	// 5. 试跑追踪只有试跑任务才有
	if t.IsTest && len(t.TraceJSON) > 0 {
		d.Trace = json.RawMessage(append([]byte(nil), t.TraceJSON...))
	}
	return d, nil
}

// usageTask 组装详情里的任务基础信息。创建时的配置快照损坏时不让详情失败：展示名回落 model key，渠道与插件留空。
func usageTask(t *model.GenerationTask) model.UsageTask {
	out := model.UsageTask{
		ID: t.ID, TaskRef: t.TaskRef, Kind: t.Kind, ModelKey: t.ModelKey, ModelName: t.ModelKey, NodeID: t.NodeID,
		Status: t.Status, Progress: t.Progress, Credits: t.Credits, ChargedCredits: t.ChargedCredits,
		ErrorCode: t.ErrorCode, ErrorMessage: t.ErrorMessage, IsTest: t.IsTest, CreatedAt: t.CreatedAt,
		SubmittedAt: t.SubmittedAt, FinishedAt: t.FinishedAt, DeadlineAt: t.DeadlineAt,
	}
	if t.CanvasProjectID != nil {
		out.CanvasID = idcodec.Encode(*t.CanvasProjectID)
	}
	var snap provider.Snapshot
	if err := json.Unmarshal(t.ConfigSnapshot, &snap); err == nil {
		if snap.Model.Label != "" {
			out.ModelName = snap.Model.Label
		}
		out.Channel = snap.Channel.Key
		if snap.Plugin.Key != "" {
			out.Plugin = snap.Plugin.Key + "@" + snap.Plugin.Version
		}
	}
	return out
}

// decodeObject 把 JSON 对象解成 map；为空或损坏返回空 map（前端按空对象展示）。
func decodeObject(raw []byte) map[string]any {
	m := map[string]any{}
	if len(raw) == 0 {
		return m
	}
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		return map[string]any{}
	}
	return m
}

// decodeNullableObject 与 decodeObject 相同，但没有内容或损坏时返回 nil（序列化成 null）。
func decodeNullableObject(raw []byte) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}

// decodeOutputs 把 output_json 解成产物列表；为空或损坏返回空切片。
func decodeOutputs(raw []byte) []model.TaskOutput {
	outs := []model.TaskOutput{}
	if len(raw) == 0 {
		return outs
	}
	if err := json.Unmarshal(raw, &outs); err != nil || outs == nil {
		return []model.TaskOutput{}
	}
	return outs
}

// upstreamOf 取最后一个带上游错误原文的事件里的原文，没有返回 nil。
func upstreamOf(events []model.UsageEvent) *string {
	for i := len(events) - 1; i >= 0; i-- {
		if s, ok := events[i].Detail["upstream"].(string); ok && s != "" {
			return &s
		}
	}
	return nil
}
