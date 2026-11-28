package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
)

const canvasAutomationPrefix = "vca_"

// CanvasAutomationKeyView exposes credential metadata and the one-time issuance secret.
type CanvasAutomationKeyView struct {
	ID            string     `json:"id"`
	CanvasID      string     `json:"canvasId"`
	Scope         string     `json:"scope"`
	BudgetCredits *int       `json:"budgetCredits,omitempty"`
	ExpiresAt     time.Time  `json:"expiresAt"`
	RevokedAt     *time.Time `json:"revokedAt,omitempty"`
	Token         string     `json:"token,omitempty"`
}

func automationKeyView(key model.CanvasAutomationKey) CanvasAutomationKeyView {
	return CanvasAutomationKeyView{ID: idcodec.Encode(key.ID), CanvasID: idcodec.Encode(key.CanvasID), Scope: key.Scope,
		ExpiresAt: key.ExpiresAt, RevokedAt: key.RevokedAt, BudgetCredits: key.BudgetCredits}
}

// IssueAutomationKey creates an expiring credential for the current canvas session.
func (s *CanvasCollaborationService) IssueAutomationKey(ctx context.Context, key model.CanvasAutomationKey, hours int) (*CanvasAutomationKeyView, error) {
	if !s.enabled {
		return nil, errcode.ErrCanvasNotFound
	}
	if hours < 1 || hours > 168 || !validAutomationScope(key) {
		return nil, errcode.ErrInvalidParams
	}
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return nil, err
	}
	token := canvasAutomationPrefix + hex.EncodeToString(secret[:])
	key.TokenHash = hashCanvasAutomationKey(token)
	key.ExpiresAt = time.Now().Add(time.Duration(hours) * time.Hour)
	if err := s.repo.CreateAutomationKey(ctx, &key); err != nil {
		return nil, collaborationError(err)
	}
	view := automationKeyView(key)
	view.Token = token
	return &view, nil
}

func validAutomationScope(key model.CanvasAutomationKey) bool {
	switch key.Scope {
	case model.CanvasAutomationRead, model.CanvasAutomationEdit:
		return key.BudgetCredits == nil
	case model.CanvasAutomationGenerate:
		return key.BudgetCredits != nil && *key.BudgetCredits > 0 && *key.BudgetCredits <= 100000
	default:
		return false
	}
}

// AuthorizeAutomationGeneration validates the paid credential against current canvas membership.
func (s *CanvasCollaborationService) AuthorizeAutomationGeneration(ctx context.Context, key model.CanvasAutomationKey) error {
	return collaborationError(s.repo.AuthorizeAutomationGeneration(ctx, key))
}

// AutomationKeys lists metadata for keys created by the current user.
func (s *CanvasCollaborationService) AutomationKeys(ctx context.Context, actor, canvas uint64) ([]CanvasAutomationKeyView, error) {
	rows, err := s.repo.AutomationKeys(ctx, actor, canvas)
	if err != nil {
		return nil, collaborationError(err)
	}
	views := make([]CanvasAutomationKeyView, 0, len(rows))
	for _, key := range rows {
		views = append(views, automationKeyView(key))
	}
	return views, nil
}

// RevokeAutomationKey invalidates a key issued by the current user.
func (s *CanvasCollaborationService) RevokeAutomationKey(ctx context.Context, actor, canvas, id uint64) error {
	return collaborationError(s.repo.RevokeAutomationKey(ctx, actor, canvas, id))
}

// AuthenticateAutomationKey resolves an opaque secret on its bound canvas only.
func (s *CanvasCollaborationService) AuthenticateAutomationKey(ctx context.Context, canvas uint64, token string) (model.CanvasAutomationKey, error) {
	if !s.enabled || len(token) != len(canvasAutomationPrefix)+64 || !strings.HasPrefix(token, canvasAutomationPrefix) {
		return model.CanvasAutomationKey{}, errcode.ErrUnauthorized
	}
	key, err := s.repo.AutomationKey(ctx, hashCanvasAutomationKey(token), canvas)
	if errors.Is(err, repository.ErrNotFound) {
		return key, errcode.ErrUnauthorized
	}
	return key, err
}

func hashCanvasAutomationKey(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}
