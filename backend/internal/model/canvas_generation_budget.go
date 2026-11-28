package model

// CanvasGenerationBudgetRequest selects a shared credit ceiling across invocations.
type CanvasGenerationBudgetRequest struct {
	Key   string `json:"key" binding:"required,max=64"`
	Limit *int   `json:"limit" binding:"required,min=0"`
}

// CanvasGenerationBudget fixes the maximum credits for one member's run.
type CanvasGenerationBudget struct {
	ID      uint64 `gorm:"primaryKey"`
	Canvas  uint64 `gorm:"not null;uniqueIndex:idx_canvas_generation_budget,priority:1"`
	User    uint64 `gorm:"not null;uniqueIndex:idx_canvas_generation_budget,priority:2"`
	Epoch   uint64 `gorm:"not null;uniqueIndex:idx_canvas_generation_budget,priority:3"`
	Key     string `gorm:"size:64;not null;uniqueIndex:idx_canvas_generation_budget,priority:4"`
	Maximum int    `gorm:"not null"`
}
