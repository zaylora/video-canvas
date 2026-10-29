package service

import (
	"context"
	"errors"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

// CreateAgentJob 使用审批ID幂等提交任务，审批重试返回原任务且不重复冻结积分。
func (s *DramaService) CreateAgentJob(ctx context.Context, userID, dramaID, approvalID uint64, req *model.DramaJobReq) (*model.DramaJobView, error) {
	// 1. 审批ID由服务端提供，零值不能形成可靠的幂等键。
	if approvalID == 0 {
		return nil, errcode.ErrInvalidParams.WithMsg("审批ID不能为空")
	}
	// 2. 复用普通提交的全部范围、锁定、并发和积分规则。
	return s.createDramaJob(ctx, userID, dramaID, &approvalID, req)
}

// approvedDramaJob 在项目锁下恢复原审批任务，跨项目复用审批ID按不存在处理。
func (s *DramaService) approvedDramaJob(ctx context.Context, userID, dramaID, approvalID uint64) (*model.DramaJobView, error) {
	var result *model.DramaJobView
	err := s.repo.WithTx(ctx, func(tx repository.DramaTx) error {
		if _, err := tx.Project(ctx, userID, dramaID, true); err != nil {
			return err
		}
		job, err := tx.JobByApproval(ctx, userID, approvalID)
		if errors.Is(err, repository.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if job.DramaID != dramaID {
			return errcode.ErrDramaNotFound
		}
		view := jobView(*job)
		result = &view
		return nil
	})
	return result, dramaError(err)
}
