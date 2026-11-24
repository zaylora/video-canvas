package service

import (
	"context"
	"strconv"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
)

// --- 用户自助入口 ---
//
// 下面这组方法给「我的 MCP 接入」用：任何登录用户都能给自己开接入，不必等管理员。
// 与上面的管理端方法的区别只有两点：归属用户固定为调用者本人，且每个方法都先确认项目确实属于他。

// OwnedCreate 用户自助建项目：归属固定为本人，请求体里的 owner_user_id 一律忽略；
// 返回项目与首枚令牌（明文只在这一次响应里出现）。
func (s *MCPProjectService) OwnedCreate(ctx context.Context, ownerID uint64, req *model.MCPProjectReq) (*model.MCPProjectCreated, error) {
	n, err := s.d.Repo.CountProjectsByOwner(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	if n >= mcpMaxProjectsPerUser {
		return nil, errcode.ErrInvalidParams.WithMsg("每人最多建 " + strconv.Itoa(mcpMaxProjectsPerUser) + " 个接入项目，先删掉不用的")
	}
	owned := *req
	owned.OwnerUserID = strconv.FormatUint(ownerID, 10)
	return s.Create(ctx, ownerID, &owned)
}

// OwnedUpdate 用户改自己的项目。归属不可通过这个入口转给别人。
func (s *MCPProjectService) OwnedUpdate(ctx context.Context, ownerID, projectID uint64, req *model.MCPProjectReq) (*model.MCPProjectView, error) {
	if _, err := s.ownedProject(ctx, ownerID, projectID); err != nil {
		return nil, err
	}
	owned := *req
	owned.OwnerUserID = strconv.FormatUint(ownerID, 10)
	return s.Update(ctx, ownerID, projectID, &owned)
}

// OwnedDelete 用户删自己的项目。
func (s *MCPProjectService) OwnedDelete(ctx context.Context, ownerID, projectID uint64) error {
	if _, err := s.ownedProject(ctx, ownerID, projectID); err != nil {
		return err
	}
	return s.Delete(ctx, ownerID, projectID)
}

// OwnedListTokens 列自己项目的令牌。
func (s *MCPProjectService) OwnedListTokens(ctx context.Context, ownerID, projectID uint64) ([]*model.MCPTokenView, error) {
	if _, err := s.ownedProject(ctx, ownerID, projectID); err != nil {
		return nil, err
	}
	return s.ListTokens(ctx, projectID)
}

// OwnedIssueToken 给自己项目生成（或轮换）那枚令牌。
func (s *MCPProjectService) OwnedIssueToken(ctx context.Context, ownerID, projectID uint64) (*model.MCPIssuedToken, error) {
	if _, err := s.ownedProject(ctx, ownerID, projectID); err != nil {
		return nil, err
	}
	return s.IssueToken(ctx, ownerID, projectID)
}

// OwnedRevokeToken 注销自己项目那枚令牌。
func (s *MCPProjectService) OwnedRevokeToken(ctx context.Context, ownerID, projectID uint64) error {
	if _, err := s.ownedProject(ctx, ownerID, projectID); err != nil {
		return err
	}
	return s.RevokeToken(ctx, ownerID, projectID)
}

// ownedProject 读项目并确认归这个用户所有。
// 不是自己的按「项目不存在」回：既不泄露别人项目是否存在，也不用另立错误码。
func (s *MCPProjectService) ownedProject(ctx context.Context, ownerID, projectID uint64) (*model.MCPProject, error) {
	p, err := s.project(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if p.OwnerUserID != ownerID {
		return nil, errcode.ErrMCPProjectNotFound
	}
	return p, nil
}
