package router

import (
	"github.com/gin-gonic/gin"

	"video-canvas/internal/handler"
	"video-canvas/internal/middleware"
)

func registerCanvasAutomation(v1, canvas *gin.RouterGroup, collab *handler.CanvasCollaborationHandler, generation *handler.GenerationTaskHandler, users middleware.UserStateLookup, secret string) {
	canvas.GET("/:id/collaboration/automation-keys", collab.AutomationKeys)
	canvas.POST("/:id/collaboration/automation-keys", collab.IssueAutomationKey)
	canvas.DELETE("/:id/collaboration/automation-keys/:keyId", collab.RevokeAutomationKey)
	scoped := v1.Group("/canvas-automation/:id/collaboration", collab.CanvasAutomationAuth, middleware.RequireActive(users))
	scoped.GET("", collab.Snapshot(secret))
	scoped.GET("/events", collab.Replay)
	scoped.POST("/ops", collab.Commit)
	scoped.GET("/ops/:opId", collab.ReceiptStatus)
	if generation != nil {
		scoped.POST("/generation-tasks", collab.RequireAutomationGeneration, generation.Create)
		scoped.GET("/generation-tasks/:task", collab.RequireAutomationGeneration, generation.GetAutomationTask)
	}
}
