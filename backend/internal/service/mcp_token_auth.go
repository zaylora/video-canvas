package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/repository"
)

// Authenticate 校验令牌：哈希存在、未注销、未过期、项目启用、归属用户可用，且未超出每分钟上限。
// 每一步都实时查库，所以停用项目或禁用用户之后下一次调用立刻失效，不需要等缓存过期。
func (s *MCPProjectService) Authenticate(ctx context.Context, plain string) (*MCPIdentity, error) {
	plain = strings.TrimSpace(plain)
	if !strings.HasPrefix(plain, model.MCPTokenPrefix) {
		return nil, errcode.ErrMCPTokenInvalid
	}
	t, err := s.d.Repo.TokenByHash(ctx, hashMCPToken(plain))
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errcode.ErrMCPTokenInvalid
		}
		return nil, err
	}
	now := s.d.Now()
	if t.RevokedAt != nil || !now.Before(t.ExpiresAt) {
		return nil, errcode.ErrMCPTokenInvalid
	}
	p, err := s.d.Repo.ProjectByID(ctx, t.ProjectID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errcode.ErrMCPTokenInvalid
		}
		return nil, err
	}
	if !p.Enabled {
		return nil, errcode.ErrMCPTokenInvalid
	}
	owner, err := s.d.Users.GetByID(ctx, p.OwnerUserID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errcode.ErrMCPTokenInvalid
		}
		return nil, err
	}
	if owner.Status != model.UserStatusActive {
		return nil, errcode.ErrMCPTokenInvalid
	}
	if !s.allow(t.ID, now) {
		return nil, errcode.ErrTooManyReqs
	}
	if t.LastUsedAt == nil || now.Sub(*t.LastUsedAt) >= mcpTouchInterval {
		if err := s.d.Repo.TouchToken(ctx, t.ID, now); err != nil {
			logger.Warn("记录 MCP 令牌使用时间失败", zap.Error(err), zap.Uint64("token_id", t.ID))
		}
	}
	return &MCPIdentity{
		ProjectID: p.ID, UserID: p.OwnerUserID,
		DefaultCanvasID: derefCanvasID(p.CanvasID), Scopes: p.ScopeSet(),
	}, nil
}

// derefCanvasID 取默认画布；没配时返回 0。
func derefCanvasID(id *uint64) uint64 {
	if id == nil {
		return 0
	}
	return *id
}

// allow 固定窗口限流：每个令牌每分钟最多 mcpRatePerMinute 次。多实例各自计数。
func (s *MCPProjectService) allow(tokenID uint64, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.windows[tokenID]
	if w == nil || now.Sub(w.start) >= time.Minute {
		w = &mcpRateWindow{start: now}
		s.windows[tokenID] = w
	}
	if w.n >= mcpRatePerMinute {
		return false
	}
	w.n++
	return true
}
