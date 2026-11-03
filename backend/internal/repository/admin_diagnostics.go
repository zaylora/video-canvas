package repository

import (
	"context"
	"time"

	"video-canvas/internal/model"
)

// AdminRequestFilter 只查询发起管理员自己的失败生成与试跑记录。
type AdminRequestFilter struct {
	UserID     uint64
	TaskID     uint64
	ModelKey   string
	ChannelKey string
	Since      time.Time
	Limit      int
}

// AdminRequestRecord 不读取提示词、输入素材、渠道请求头或完整配置快照。
type AdminRequestRecord struct {
	ID           uint64
	ModelKey     string
	ChannelKey   string
	IsTest       bool
	Status       string
	ErrorCode    string
	ErrorMessage string
	CreatedAt    time.Time
}

// RecentRequests 按用户、时间和可选模型/渠道过滤，最多读取十条；配置快照只投影渠道 key。
func (r *AdminAssistantRepository) RecentRequests(ctx context.Context, f AdminRequestFilter) ([]AdminRequestRecord, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 5
	}
	if limit > 10 {
		limit = 10
	}
	q := r.db.WithContext(ctx).Model(&model.GenerationTask{}).
		Select("id, model_id AS model_key, config_snapshot->'channel'->>'key' AS channel_key, is_test, status, error_code, error_message, created_at").
		Where("user_id = ? AND created_at >= ?", f.UserID, f.Since).
		Where("status IN ?", []string{model.TaskFailed, model.TaskExpired})
	if f.TaskID != 0 {
		q = q.Where("id = ?", f.TaskID)
	}
	if f.ModelKey != "" {
		q = q.Where("model_id = ?", f.ModelKey)
	}
	if f.ChannelKey != "" {
		q = q.Where("config_snapshot->'channel'->>'key' = ?", f.ChannelKey)
	}
	out := []AdminRequestRecord{}
	err := q.Order("created_at DESC, id DESC").Limit(limit).Find(&out).Error
	return out, err
}
