package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/datatypes"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
)

// ConversationRepo 是对话与生成记录的数据访问接口（真实实现是 repository.ConversationRepository）。
type ConversationRepo interface {
	// EnsureDefault 返回用户的默认创作，不存在时创建；并发创建时以唯一索引兜底，后到的读取先到的。
	EnsureDefault(ctx context.Context, userID uint64) (*model.Conversation, error)
	// Create 新建对话。
	Create(ctx context.Context, c *model.Conversation) error
	// GetByID 按 id + user_id 查询，不存在或不属于该用户返回 repository.ErrNotFound。
	GetByID(ctx context.Context, userID, id uint64) (*model.Conversation, error)
	// List 返回用户全部未删除的对话：默认创作在前，其余按最近记录时间倒序。
	List(ctx context.Context, userID uint64) ([]model.Conversation, error)
	// Count 统计用户未删除的对话数（含默认创作）。
	Count(ctx context.Context, userID uint64) (int64, error)
	// Rename 改标题，不存在或不属于该用户返回 repository.ErrNotFound。
	Rename(ctx context.Context, userID, id uint64, title string) error
	// Delete 软删除对话，不存在或不属于该用户返回 repository.ErrNotFound。
	Delete(ctx context.Context, userID, id uint64) error
	// AddRecordCount 给对话的记录数加 delta；at 非空时同时更新最近记录时间。
	AddRecordCount(ctx context.Context, id uint64, delta int, at *time.Time) error

	// CreateRecord 新建记录；幂等键与同一用户下的已有记录重复时返回 repository.ErrDuplicate。
	CreateRecord(ctx context.Context, r *model.ConversationRecord) error
	// GetRecord 按 id + user_id 查询未删除的记录，不存在返回 repository.ErrNotFound。
	GetRecord(ctx context.Context, userID, id uint64) (*model.ConversationRecord, error)
	// FindRecordByIdem 按 user_id + 幂等键查询未删除的记录，不存在返回 repository.ErrNotFound。
	FindRecordByIdem(ctx context.Context, userID uint64, key string) (*model.ConversationRecord, error)
	// ListRecords 查询对话里 id 小于 before（0 表示不限）的记录，id 倒序，最多 limit 条。
	ListRecords(ctx context.Context, userID, convID, before uint64, limit int) ([]model.ConversationRecord, error)
	// SaveRecordTasks 写入提交结果：每格的任务 id（失败为 null）、失败原因和冻结积分合计。
	SaveRecordTasks(ctx context.Context, id uint64, taskIDs, submitErrors datatypes.JSON, quote int) error
	// DeleteRecord 软删除记录，不存在或不属于该用户返回 repository.ErrNotFound。
	DeleteRecord(ctx context.Context, userID, id uint64) error
	// ConversationIDsByRecordIDs 返回这些记录所属的对话 id（去重，只含该用户未删除的记录）。
	ConversationIDsByRecordIDs(ctx context.Context, userID uint64, recordIDs []uint64) ([]uint64, error)
}

// ConversationTasks 是对话依赖的生成任务能力（真实实现是 *GenerationTaskService）。
type ConversationTasks interface {
	// Create 提交生成任务，逐项返回任务或失败原因；请求级错误整体返回 error。
	Create(ctx context.Context, userID uint64, idempotencyKey string, req *model.CreateGenerationTaskReq) (*model.CreateGenerationTaskResp, error)
	// Cancel 取消任务并退回冻结的积分。
	Cancel(ctx context.Context, userID, id uint64) (*model.GenerationTaskView, error)
	// ViewsByIDs 批量查询该用户的任务快照。
	ViewsByIDs(ctx context.Context, userID uint64, ids []uint64) ([]model.GenerationTaskView, error)
	// List 对账查询，这里只用 status=active 取该用户进行中的任务。
	List(ctx context.Context, userID uint64, req *model.ListGenerationTaskReq) ([]model.GenerationTaskView, error)
}

var _ ConversationTasks = (*GenerationTaskService)(nil)

const (
	// MaxConversations 是每个用户最多的对话数（含默认创作），防止空对话无限增长。
	MaxConversations = 200

	newConversationTitle = "新对话"
	maxConversationTitle = 50 // 标题最多多少个字
	autoTitleRunes       = 16 // 新对话标题取提示词的前多少个字
	defaultRecordPage    = 20
	maxRecordPage        = 50
	// recordNodePrefix 是记录里任务的 node_id 前缀：rec:{记录id}:{格子序号}，用来从任务反查所属记录。
	recordNodePrefix = "rec:"
)

// ConversationTarget 指明提交到哪段对话：Default 是默认创作，New 是新建一段，否则用 ID。
type ConversationTarget struct {
	ID      uint64 // 已有对话的主键
	Default bool   // 默认创作（不存在时自动创建）
	New     bool   // 新建一段对话
}

// ConversationService 是首页生成的对话与记录业务。任务状态只认 generation_tasks，这里只保存「提交了什么」。
type ConversationService struct {
	repo  ConversationRepo
	tasks ConversationTasks
	now   func() time.Time
}

// NewConversationService 创建对话服务。
func NewConversationService(repo ConversationRepo, tasks ConversationTasks) *ConversationService {
	return &ConversationService{repo: repo, tasks: tasks, now: time.Now}
}

// List 返回当前用户的对话列表：默认创作永远第一条（没有时自动创建），并标出有进行中任务的对话。
func (s *ConversationService) List(ctx context.Context, userID uint64) ([]model.ConversationView, error) {
	// 1. 先保证默认创作存在，侧栏永远有地方可去
	if _, err := s.repo.EnsureDefault(ctx, userID); err != nil {
		return nil, err
	}
	convs, err := s.repo.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	// 2. 进行中的任务 → 记录 → 对话：只看 node_id 带 rec: 前缀的任务，画布节点的任务不影响对话
	active, err := s.activeConversations(ctx, userID)
	if err != nil {
		return nil, err
	}
	views := make([]model.ConversationView, 0, len(convs))
	for i := range convs {
		views = append(views, conversationView(&convs[i], active[convs[i].ID]))
	}
	return views, nil
}

// activeConversations 返回有进行中任务的对话 id 集合。
func (s *ConversationService) activeConversations(ctx context.Context, userID uint64) (map[uint64]bool, error) {
	tasks, err := s.tasks.List(ctx, userID, &model.ListGenerationTaskReq{Status: "active"})
	if err != nil {
		return nil, err
	}
	var recordIDs []uint64
	for i := range tasks {
		if id, ok := recordIDOfNode(tasks[i].NodeID); ok {
			recordIDs = append(recordIDs, id)
		}
	}
	out := map[uint64]bool{}
	if len(recordIDs) == 0 {
		return out, nil
	}
	convIDs, err := s.repo.ConversationIDsByRecordIDs(ctx, userID, recordIDs)
	if err != nil {
		return nil, err
	}
	for _, id := range convIDs {
		out[id] = true
	}
	return out, nil
}

// Create 新建一段对话，标题为空时叫「新对话」；每个用户最多 MaxConversations 段。
func (s *ConversationService) Create(ctx context.Context, userID uint64, req *model.CreateConversationReq) (*model.ConversationView, error) {
	// 1. 标题去掉首尾空白，空则用默认名
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = newConversationTitle
	}
	// 2. 数量上限检查后新建
	c, err := s.createConversation(ctx, userID, title)
	if err != nil {
		return nil, err
	}
	v := conversationView(c, false)
	return &v, nil
}

// createConversation 检查数量上限后新建对话。
func (s *ConversationService) createConversation(ctx context.Context, userID uint64, title string) (*model.Conversation, error) {
	n, err := s.repo.Count(ctx, userID)
	if err != nil {
		return nil, err
	}
	if n >= MaxConversations {
		return nil, errcode.ErrConversationLimit
	}
	c := &model.Conversation{UserID: userID, Title: title}
	if err := s.repo.Create(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

// Rename 改对话标题（含默认创作）。别人的对话和不存在的对话统一返回「对话不存在」。
func (s *ConversationService) Rename(ctx context.Context, userID, id uint64, title string) error {
	// 1. 标题不能为空白，按字数限制 50
	title = strings.TrimSpace(title)
	if title == "" {
		return errcode.ErrInvalidParams.WithMsg("标题不能为空")
	}
	if len([]rune(title)) > maxConversationTitle {
		return errcode.ErrInvalidParams.WithMsg("标题最长 50 个字")
	}
	// 2. 按 id + user_id 更新，更新不到就是不存在或不属于当前用户
	err := s.repo.Rename(ctx, userID, id, title)
	if errors.Is(err, repository.ErrNotFound) {
		return errcode.ErrConversationNotFound
	}
	return err
}

// Delete 软删除对话。默认创作不能删；对话里进行中的任务继续跑完，素材仍在资产里。
func (s *ConversationService) Delete(ctx context.Context, userID, id uint64) error {
	// 1. 先读出来：要判断是不是默认创作，也顺便确认归属
	c, err := s.getConversation(ctx, userID, id)
	if err != nil {
		return err
	}
	if c.IsDefault {
		return errcode.ErrConversationDefault
	}
	// 2. 软删除；并发删除时后到的得到「不存在」
	err = s.repo.Delete(ctx, userID, id)
	if errors.Is(err, repository.ErrNotFound) {
		return errcode.ErrConversationNotFound
	}
	return err
}

// getConversation 按 id + user_id 取对话，不存在或不属于该用户统一返回「对话不存在」，不暴露它是否存在。
func (s *ConversationService) getConversation(ctx context.Context, userID, id uint64) (*model.Conversation, error) {
	c, err := s.repo.GetByID(ctx, userID, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrConversationNotFound
	}
	return c, err
}

// ListRecords 按新到旧分页返回对话里的记录，每条带任务快照。before 是上一页返回的游标（0 表示最新一页）。
func (s *ConversationService) ListRecords(ctx context.Context, userID, convID uint64, req *model.ListConversationRecordsReq, before uint64) (*model.ConversationRecordPage, error) {
	// 1. 归属校验
	if _, err := s.getConversation(ctx, userID, convID); err != nil {
		return nil, err
	}
	// 2. 修正条数：不传取 20，最多 50；多取一条用来判断有没有下一页
	limit := req.Limit
	if limit <= 0 {
		limit = defaultRecordPage
	}
	limit = min(limit, maxRecordPage)
	recs, err := s.repo.ListRecords(ctx, userID, convID, before, limit+1)
	if err != nil {
		return nil, err
	}
	page := &model.ConversationRecordPage{}
	if len(recs) > limit {
		recs = recs[:limit]
		next := idcodec.ID(recs[limit-1].ID)
		page.Next = &next
	}
	// 3. 一次性取回所有任务快照，再按格子对齐
	views, err := s.recordViews(ctx, userID, recs)
	if err != nil {
		return nil, err
	}
	page.Items = views
	return page, nil
}

// Submit 提交一条生成记录：解析目标对话 → 建记录 → 创建生成任务 → 回写任务 id 和冻结积分。
// 请求级错误（参数不合法、模型下线）一个任务也不会创建，并回收刚建的记录和新对话；
// 某一格失败（积分不足、并发已满……）只让那一格为空，其余照常。
func (s *ConversationService) Submit(ctx context.Context, userID uint64, target ConversationTarget, idempotencyKey string, req *model.SubmitConversationRecordReq) (*model.SubmitConversationRecordResp, error) {
	// 1. 幂等键长度：超过列宽会在插入时才失败，提前按参数错误返回（与任务服务同一上限，它要给每格追加 #i）；
	//    同一个 key 已经提交过，直接返回那条记录，不再建任务
	if len(idempotencyKey) > maxIdempotencyKeyLen-len("#9") {
		return nil, errcode.ErrInvalidParams.WithMsg("Idempotency-Key 过长")
	}
	if idempotencyKey != "" {
		if resp, err := s.existingSubmit(ctx, userID, idempotencyKey); resp != nil || err != nil {
			return resp, err
		}
	}

	// 2. 解析目标对话：默认创作自动创建；new 先检查数量上限再新建
	conv, created, err := s.resolveTarget(ctx, userID, target, req)
	if err != nil {
		return nil, err
	}

	// 3. 先建记录拿到 id：任务的 node_id 要带上它，之后才能从任务反查记录
	rec, dup, err := s.createRecord(ctx, userID, conv, created, idempotencyKey, req)
	if rec == nil {
		return dup, err
	}

	// 4. 创建生成任务：每一格一个节点 id（rec:{记录id}:{序号}），幂等键原样交给任务服务
	resp, err := s.tasks.Create(ctx, userID, idempotencyKey, &model.CreateGenerationTaskReq{
		Kind: req.Kind, ModelID: req.ModelID, NodeIDs: recordNodeIDs(rec.ID, req.Count), Input: req.Input,
	})
	if err != nil {
		s.discard(ctx, userID, conv, created, rec)
		return nil, err
	}

	// 5. 逐格整理结果并回写
	idsJSON, errsJSON, quote, err := summarizeItems(resp.Items, req.Count)
	if err != nil {
		return nil, err
	}
	if err := s.repo.SaveRecordTasks(ctx, rec.ID, idsJSON, errsJSON, quote); err != nil {
		return nil, err
	}
	rec.TaskIDsJSON, rec.SubmitErrorsJSON, rec.QuoteCredits = idsJSON, errsJSON, quote

	// 6. 更新对话的记录数和最近时间（侧栏排序用）
	now := s.now()
	if err := s.repo.AddRecordCount(ctx, conv.ID, 1, &now); err != nil {
		return nil, err
	}

	// 7. 任务快照直接用创建结果，不用再查一遍
	byID := map[uint64]model.GenerationTaskView{}
	for _, item := range resp.Items {
		if item.Task != nil {
			byID[item.Task.ID] = *item.Task
		}
	}
	return &model.SubmitConversationRecordResp{ConversationID: idcodec.ID(conv.ID), Record: recordView(rec, byID)}, nil
}

// createRecord 建一条还没有任务的记录。成功返回 (记录, nil, nil)；失败时已回收新建的对话，返回 (nil, 已有结果或 nil, 错误)：
// 幂等键与并发的同 key 请求撞上时，返回那一次提交的结果（唯一索引只含未删除的行，所以一定能查到）。
func (s *ConversationService) createRecord(ctx context.Context, userID uint64, conv *model.Conversation, created bool, key string, req *model.SubmitConversationRecordReq) (*model.ConversationRecord, *model.SubmitConversationRecordResp, error) {
	input, err := json.Marshal(req.Input)
	if err != nil {
		s.discard(ctx, userID, conv, created, nil)
		return nil, nil, errcode.ErrInvalidParams.WithMsg("生成参数格式错误")
	}
	rec := &model.ConversationRecord{
		ConversationID: conv.ID, UserID: userID, Kind: req.Kind, ModelKey: req.ModelID,
		Prompt: req.Prompt, InputJSON: datatypes.JSON(input), Count: req.Count,
		TaskIDsJSON: datatypes.JSON("[]"), SubmitErrorsJSON: datatypes.JSON("[]"), IdempotencyKey: key,
	}
	if err := s.repo.CreateRecord(ctx, rec); err != nil {
		s.discard(ctx, userID, conv, created, nil)
		if !errors.Is(err, repository.ErrDuplicate) {
			return nil, nil, err
		}
		resp, ferr := s.existingSubmit(ctx, userID, key)
		if resp == nil && ferr == nil {
			return nil, nil, err
		}
		return nil, resp, ferr
	}
	return rec, nil, nil
}

// recordNodeIDs 生成记录里每一格任务的 node_id：rec:{记录id}:{序号}。
func recordNodeIDs(recordID uint64, count int) []string {
	nodes := make([]string, count)
	for i := range nodes {
		nodes[i] = fmt.Sprintf("%s%d:%d", recordNodePrefix, recordID, i)
	}
	return nodes
}

// summarizeItems 把任务服务的逐项结果整理成要入库的内容：成功的格子记任务 id 并累加冻结积分，
// 失败的格子记 null 和原因。返回任务 id 数组、失败原因数组（都已编码成 JSON）和冻结积分合计。
func summarizeItems(items []model.CreateTaskItem, count int) (taskIDs, submitErrors datatypes.JSON, quote int, err error) {
	ids := make([]*uint64, count)
	errs := []model.RecordSubmitError{}
	for i, item := range items {
		if i >= count {
			break
		}
		switch {
		case item.Task != nil:
			id := item.Task.ID
			ids[i] = &id
			quote += item.Task.Credits
		case item.Error != nil:
			errs = append(errs, model.RecordSubmitError{Index: i, Status: item.Error.Status, Code: item.Error.Code, Message: item.Error.Message})
		}
	}
	if taskIDs, err = json.Marshal(ids); err != nil {
		return nil, nil, 0, err
	}
	if submitErrors, err = json.Marshal(errs); err != nil {
		return nil, nil, 0, err
	}
	return taskIDs, submitErrors, quote, nil
}

// existingSubmit 按幂等键找已提交的记录，没有返回 (nil, nil)。
func (s *ConversationService) existingSubmit(ctx context.Context, userID uint64, key string) (*model.SubmitConversationRecordResp, error) {
	rec, err := s.repo.FindRecordByIdem(ctx, userID, key)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	views, err := s.recordViews(ctx, userID, []model.ConversationRecord{*rec})
	if err != nil {
		return nil, err
	}
	return &model.SubmitConversationRecordResp{ConversationID: idcodec.ID(rec.ConversationID), Record: views[0]}, nil
}

// resolveTarget 解析提交目标，返回对话以及它是不是本次新建的（失败时要回收）。
func (s *ConversationService) resolveTarget(ctx context.Context, userID uint64, target ConversationTarget, req *model.SubmitConversationRecordReq) (*model.Conversation, bool, error) {
	switch {
	case target.Default:
		c, err := s.repo.EnsureDefault(ctx, userID)
		return c, false, err
	case target.New:
		c, err := s.createConversation(ctx, userID, newTitle(req))
		return c, true, err
	default:
		c, err := s.getConversation(ctx, userID, target.ID)
		return c, false, err
	}
}

// discard 回收一次失败提交留下的东西：记录（已建时）和本次新建的对话。回收失败只能忽略：
// 残留的记录没有任务、对话没有记录，用户看不到也不影响后续，不值得掩盖原始错误。
func (s *ConversationService) discard(ctx context.Context, userID uint64, conv *model.Conversation, created bool, rec *model.ConversationRecord) {
	if rec != nil {
		_ = s.repo.DeleteRecord(ctx, userID, rec.ID)
	}
	if created {
		_ = s.repo.Delete(ctx, userID, conv.ID)
	}
}

// DeleteRecord 删除一条记录。cancelActive 为 true 时先取消其中进行中的任务（积分退回）；
// 为 false 时任务继续跑完，产物仍进资产。
func (s *ConversationService) DeleteRecord(ctx context.Context, userID, convID, recordID uint64, cancelActive bool) error {
	// 1. 归属：对话和记录都必须属于当前用户，且记录必须在这段对话里
	if _, err := s.getConversation(ctx, userID, convID); err != nil {
		return err
	}
	rec, err := s.repo.GetRecord(ctx, userID, recordID)
	if errors.Is(err, repository.ErrNotFound) || (err == nil && rec.ConversationID != convID) {
		return errcode.ErrRecordNotFound
	}
	if err != nil {
		return err
	}

	// 2. 取消进行中的任务：已结束的不碰；取消的瞬间任务刚好结束（不可取消）说明目标已达成，不算失败
	if cancelActive {
		if err := s.cancelActive(ctx, userID, rec); err != nil {
			return err
		}
	}

	// 3. 软删除记录并把对话的记录数减 1
	err = s.repo.DeleteRecord(ctx, userID, recordID)
	if errors.Is(err, repository.ErrNotFound) {
		return errcode.ErrRecordNotFound
	}
	if err != nil {
		return err
	}
	return s.repo.AddRecordCount(ctx, convID, -1, nil)
}

// cancelActive 取消记录里所有非终态的任务。
func (s *ConversationService) cancelActive(ctx context.Context, userID uint64, rec *model.ConversationRecord) error {
	ids := taskIDsOf(rec)
	if len(ids) == 0 {
		return nil
	}
	views, err := s.tasks.ViewsByIDs(ctx, userID, ids)
	if err != nil {
		return err
	}
	for i := range views {
		if model.IsTerminalStatus(views[i].Status) {
			continue
		}
		if _, err := s.tasks.Cancel(ctx, userID, views[i].ID); err != nil && !isTaskGone(err) {
			return err
		}
	}
	return nil
}

// isTaskGone 判断取消失败是否只是任务已经结束或不存在。
func isTaskGone(err error) bool {
	var e *errcode.Error
	return errors.As(err, &e) && (e.Code == errcode.ErrTaskNotCancelable.Code || e.Code == errcode.ErrTaskNotFound.Code)
}

// recordViews 把一批记录转成视图：一次查出所有任务快照，再按格子对齐。
func (s *ConversationService) recordViews(ctx context.Context, userID uint64, recs []model.ConversationRecord) ([]model.ConversationRecordView, error) {
	var ids []uint64
	for i := range recs {
		ids = append(ids, taskIDsOf(&recs[i])...)
	}
	byID := map[uint64]model.GenerationTaskView{}
	if len(ids) > 0 {
		views, err := s.tasks.ViewsByIDs(ctx, userID, ids)
		if err != nil {
			return nil, err
		}
		for _, v := range views {
			byID[v.ID] = v
		}
	}
	out := make([]model.ConversationRecordView, 0, len(recs))
	for i := range recs {
		out = append(out, recordView(&recs[i], byID))
	}
	return out, nil
}

// taskIDsOf 取出记录里已创建的任务 id（跳过提交失败的格子）。
func taskIDsOf(rec *model.ConversationRecord) []uint64 {
	var out []uint64
	for _, id := range decodeTaskIDs(rec) {
		if id != nil {
			out = append(out, *id)
		}
	}
	return out
}

// decodeTaskIDs 解出每格的任务 id；数据损坏或提交中断（长度不足）时补 nil，保证长度等于生成数量。
func decodeTaskIDs(rec *model.ConversationRecord) []*uint64 {
	var ids []*uint64
	_ = json.Unmarshal(rec.TaskIDsJSON, &ids) // 损坏按空处理：这一条记录显示成「没有任务」，不影响整页
	for len(ids) < rec.Count {
		ids = append(ids, nil)
	}
	return ids[:rec.Count]
}

// recordView 组装记录视图：任务按格子对齐，查不到的任务（被清理或不属于用户）显示为 null。
func recordView(rec *model.ConversationRecord, tasks map[uint64]model.GenerationTaskView) model.ConversationRecordView {
	slots := make([]*model.GenerationTaskView, rec.Count)
	for i, id := range decodeTaskIDs(rec) {
		if id == nil {
			continue
		}
		if v, ok := tasks[*id]; ok {
			cp := v
			slots[i] = &cp
		}
	}
	errs := []model.RecordSubmitError{}
	_ = json.Unmarshal(rec.SubmitErrorsJSON, &errs) // 损坏按空处理，原因同上
	input := rec.InputJSON
	if len(input) == 0 {
		input = datatypes.JSON("{}")
	}
	return model.ConversationRecordView{
		ID: idcodec.ID(rec.ID), ConversationID: idcodec.ID(rec.ConversationID), Kind: rec.Kind, ModelID: rec.ModelKey,
		Prompt: rec.Prompt, Input: input, Count: rec.Count, Tasks: slots, SubmitErrors: errs,
		QuoteCredits: rec.QuoteCredits, CreatedAt: rec.CreatedAt,
	}
}

// conversationView 转成列表项。
func conversationView(c *model.Conversation, active bool) model.ConversationView {
	return model.ConversationView{
		ID: idcodec.ID(c.ID), Title: c.Title, IsDefault: c.IsDefault, RecordCount: c.RecordCount,
		LastRecordAt: c.LastRecordAt, Active: active, CreatedAt: c.CreatedAt,
	}
}

// newTitle 决定新对话的标题：显式标题优先，否则取提示词前 16 个字（按字不按字节，换行当空格），都没有叫「新对话」。
func newTitle(req *model.SubmitConversationRecordReq) string {
	if t := strings.TrimSpace(req.Title); t != "" {
		return t
	}
	prompt := strings.Join(strings.Fields(req.Prompt), " ")
	if prompt == "" {
		return newConversationTitle
	}
	runes := []rune(prompt)
	return string(runes[:min(len(runes), autoTitleRunes)])
}

// recordIDOfNode 从任务的 node_id（rec:{记录id}:{序号}）取出记录 id，不是对话任务返回 false。
func recordIDOfNode(nodeID string) (uint64, bool) {
	rest, ok := strings.CutPrefix(nodeID, recordNodePrefix)
	if !ok {
		return 0, false
	}
	idPart, _, _ := strings.Cut(rest, ":")
	id, err := strconv.ParseUint(idPart, 10, 64)
	return id, err == nil && id > 0
}
