package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
)

// Get 查询当前用户的一个任务。别人的任务、不存在的任务、运营试跑任务统一返回“任务不存在”。
func (s *GenerationTaskService) Get(ctx context.Context, userID, id uint64) (*model.GenerationTaskView, error) {
	// 1. 按 id + user_id 查询；is_test 任务不对普通用户接口暴露，同样当作不存在
	t, err := s.userTask(ctx, userID, id, false)
	if err != nil {
		return nil, err
	}
	return taskView(t), nil
}

// GetTestTask 查询运营试跑任务（管理端轮询试跑进度）。只返回属于该用户的 is_test 任务，其余统一返回“任务不存在”。
func (s *GenerationTaskService) GetTestTask(ctx context.Context, userID, id uint64) (*model.GenerationTaskView, error) {
	// 1. 按 id + user_id 查询，并要求是试跑任务
	t, err := s.userTask(ctx, userID, id, true)
	if err != nil {
		return nil, err
	}
	return taskView(t), nil
}

// GetTestTrace 读取试跑任务的执行追踪（trace_json，宿主已脱敏）。只返回属于该用户的 is_test 任务，
// 别人的任务、正式任务、不存在的任务统一返回“任务不存在”；还没有追踪时返回空切片。
func (s *GenerationTaskService) GetTestTrace(ctx context.Context, userID, id uint64) ([]provider.TraceStep, error) {
	// 1. 按 id + user_id 查询，并要求是试跑任务：正式任务不记追踪，也不能借这个接口探测
	t, err := s.userTask(ctx, userID, id, true)
	if err != nil {
		return nil, err
	}
	// 2. 解出步骤；还没写过追踪（任务刚创建）时是空数组
	steps := []provider.TraceStep{}
	if len(t.TraceJSON) == 0 {
		return steps, nil
	}
	if err := json.Unmarshal(t.TraceJSON, &steps); err != nil {
		return nil, fmt.Errorf("解析任务 %d 的 trace_json 失败：%w", id, err)
	}
	return steps, nil
}

// userTask 按 id + user_id 取任务，wantTest 决定只接受试跑任务还是只接受正式任务；
// 不存在、不属于该用户、类型不符统一返回 ErrTaskNotFound，不暴露任务是否存在。
func (s *GenerationTaskService) userTask(ctx context.Context, userID, id uint64, wantTest bool) (*model.GenerationTask, error) {
	t, err := s.repo.GetByID(ctx, userID, id)
	if errors.Is(err, repository.ErrNotFound) || (err == nil && t.IsTest != wantTest) {
		return nil, errcode.ErrTaskNotFound
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}

// List 任务查询，三种模式必须且只能用一种：
//   - ids：对账，接受十进制或编码任务 ID（最多 100 个）；
//   - status=active：对账，该用户所有非终态任务；
//   - node_id + canvas_project_id：某张画布某个节点的生成记录（如角色声音），新的在前最多 20 个，带提示词和模型名。
//
// 返回的切片永远不是 nil，只包含当前用户的正式任务。
func (s *GenerationTaskService) List(ctx context.Context, userID uint64, req *model.ListGenerationTaskReq) ([]model.GenerationTaskView, error) {
	// 1. 三种模式必须且只能用一种；节点模式的两个参数缺一个也算没说清楚，按参数错误返回，不退化成别的查询
	hasIDs, hasStatus := strings.TrimSpace(req.IDs) != "", req.Status != ""
	hasNode := req.NodeID != "" || req.CanvasProjectID != ""
	modes := 0
	for _, on := range []bool{hasIDs, hasStatus, hasNode} {
		if on {
			modes++
		}
	}
	if modes != 1 {
		return nil, errcode.ErrInvalidParams.WithMsg("ids、status=active、node_id + canvas_project_id 必须且只能传一种")
	}

	var (
		tasks []model.GenerationTask
		err   error
	)
	switch {
	case hasStatus:
		// 2. status=active：该用户所有非终态任务（WS 重连后对账）
		tasks, err = s.repo.ListActive(ctx, userID)
	case hasNode:
		// 3. 节点模式：两个参数都要有；画布 ID 和其他接口一样接受编码串，也兼容十进制
		return s.listByNode(ctx, userID, req)
	default:
		// 4. ids：逐个解析，非法 / 超过 100 个返回 400；查询带 user_id，别人的任务查不到，直接不出现在结果里
		ids, perr := parseTaskIDs(req.IDs)
		if perr != nil {
			return nil, errcode.ErrInvalidParams.WithMsg(perr.Error())
		}
		tasks, err = s.repo.ListByIDs(ctx, userID, ids)
	}
	if err != nil {
		return nil, err
	}

	// 5. 转成视图；outputs 永远是数组
	views := make([]model.GenerationTaskView, 0, len(tasks))
	for i := range tasks {
		views = append(views, *taskView(&tasks[i]))
	}
	return views, nil
}

// listByNode 是 List 的节点模式：查当前用户在这张画布这个节点上最近的正式任务，新的在前，最多 maxNodeTasks 个，
// 视图额外带上提示词和模型展示名（详情页的生成记录要显示，对账和推送不需要）。
func (s *GenerationTaskService) listByNode(ctx context.Context, userID uint64, req *model.ListGenerationTaskReq) ([]model.GenerationTaskView, error) {
	// 1. 两个参数必须同时传
	if req.NodeID == "" || req.CanvasProjectID == "" {
		return nil, errcode.ErrInvalidParams.WithMsg("node_id 与 canvas_project_id 必须同时传")
	}
	canvasID, ok := parseIDPart(strings.TrimSpace(req.CanvasProjectID))
	if !ok {
		return nil, errcode.ErrInvalidParams.WithMsg("canvas_project_id 格式错误")
	}

	// 2. 查询带 user_id：别人的画布查不到任务，结果为空，不暴露画布是否存在；
	//    不校验画布归属，因为条件里已有 user_id，查到的只会是自己的任务
	tasks, err := s.repo.ListByNode(ctx, userID, canvasID, req.NodeID, maxNodeTasks)
	if err != nil {
		return nil, err
	}

	// 3. 转成视图并补提示词、模型名；解析失败（历史脏数据）时这两项留空，不影响其余字段
	views := make([]model.GenerationTaskView, 0, len(tasks))
	for i := range tasks {
		view := taskView(&tasks[i])
		view.Prompt = taskPromptText(tasks[i].InputJSON)
		view.ModelLabel = taskModelLabel(tasks[i].ConfigSnapshot, tasks[i].ModelKey)
		views = append(views, *view)
	}
	return views, nil
}

// ViewsByIDs 批量查询该用户的正式任务快照（对话记录用）。id 超过一次对账上限时分批查询；
// 别人的任务、不存在的任务、试跑任务都不会出现在结果里，调用方按 id 自行对齐。
func (s *GenerationTaskService) ViewsByIDs(ctx context.Context, userID uint64, ids []uint64) ([]model.GenerationTaskView, error) {
	views := make([]model.GenerationTaskView, 0, len(ids))
	for start := 0; start < len(ids); start += maxReconcileIDs {
		end := min(start+maxReconcileIDs, len(ids))
		tasks, err := s.repo.ListByIDs(ctx, userID, ids[start:end])
		if err != nil {
			return nil, err
		}
		for i := range tasks {
			views = append(views, *taskView(&tasks[i]))
		}
	}
	return views, nil
}

// GetCredits 返回当前用户的积分。账户不存在时按配置的初始积分惰性创建后再返回。
func (s *GenerationTaskService) GetCredits(ctx context.Context, userID uint64) (*model.CreditView, error) {
	// 1. 惰性创建账户：已存在时是空操作，不会覆盖余额；初始积分来自系统设置（缺省 config），
	//    真正建出新账户时仓储在同一事务补写 initial 流水
	initial, err := s.initialCredits(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.repo.EnsureCredit(ctx, userID, initial); err != nil {
		return nil, err
	}
	// 2. 读取账户；可用 = 余额 - 冻结
	acc, err := s.repo.GetCredit(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &model.CreditView{Balance: acc.Balance, Frozen: acc.Frozen, Available: acc.Balance - acc.Frozen}, nil
}
