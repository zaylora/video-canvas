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
