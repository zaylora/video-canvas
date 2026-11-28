package model

import (
	"context"
	"time"
)

// Automation scopes grant graph reads or graph reads and ordinary edits.
const (
	CanvasAutomationRead         = "read"
	CanvasAutomationEdit         = "edit"
	CanvasAutomationGenerate     = "generate"
	CanvasAutomationBudgetPrefix = "automation:"
)

// CanvasAutomationKey limits a credential to one member, canvas, and session.
type CanvasAutomationKey struct {
	ID            uint64    `gorm:"primaryKey"`
	CanvasID      uint64    `gorm:"not null;index:idx_canvas_automation_member,priority:1"`
	UserID        uint64    `gorm:"not null;index:idx_canvas_automation_member,priority:2"`
	Epoch         uint64    `gorm:"not null"`
	Scope         string    `gorm:"size:16;not null"`
	BudgetCredits *int      `gorm:"type:integer;check:canvas_automation_budget_by_scope,(scope = 'generate' AND budget_credits IS NOT NULL AND budget_credits > 0) OR (scope IN ('read', 'edit') AND budget_credits IS NULL)"`
	TokenVersion  int       `gorm:"not null"`
	TokenHash     string    `gorm:"size:64;not null;uniqueIndex" json:"-"`
	ExpiresAt     time.Time `gorm:"not null"`
	RevokedAt     *time.Time
	CreatedAt     time.Time `gorm:"not null"`
}

// CanEdit reports whether the key may submit ordinary canvas edits.
func (key CanvasAutomationKey) CanEdit() bool {
	return key.Scope == CanvasAutomationEdit || key.Scope == CanvasAutomationGenerate
}

type canvasAutomationContextKey struct{}

// WithCanvasAutomation stores the verified request identity for transactional checks.
func WithCanvasAutomation(ctx context.Context, key CanvasAutomationKey) context.Context {
	return context.WithValue(ctx, canvasAutomationContextKey{}, key)
}

// CanvasAutomationFromContext identifies requests made with an automation key.
func CanvasAutomationFromContext(ctx context.Context) (CanvasAutomationKey, bool) {
	key, ok := ctx.Value(canvasAutomationContextKey{}).(CanvasAutomationKey)
	return key, ok
}
