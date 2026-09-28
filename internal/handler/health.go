package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"video-canvas/internal/pkg/response"
	"video-canvas/pkg/version"
)

type HealthHandler struct {
	db  *gorm.DB
	rdb *redis.Client
}

func NewHealthHandler(db *gorm.DB, rdb *redis.Client) *HealthHandler {
	return &HealthHandler{db: db, rdb: rdb}
}

// Check 检查各依赖是否可用，任一不可用时返回 503，方便负载均衡/K8s 探活。
func (h *HealthHandler) Check(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	deps := gin.H{"database": "ok", "redis": "disabled"}
	healthy := true

	if sqlDB, err := h.db.DB(); err != nil || sqlDB.PingContext(ctx) != nil {
		deps["database"] = "down"
		healthy = false
	}
	if h.rdb != nil {
		deps["redis"] = "ok"
		if err := h.rdb.Ping(ctx).Err(); err != nil {
			deps["redis"] = "down"
			healthy = false
		}
	}

	data := gin.H{"status": "ok", "deps": deps, "version": version.Get()}
	if !healthy {
		data["status"] = "degraded"
		c.JSON(http.StatusServiceUnavailable, response.Body{Code: 0, Msg: "degraded", Data: data})
		return
	}
	response.OK(c, data)
}
