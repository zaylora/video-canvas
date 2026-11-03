package repository

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"

	"video-canvas/internal/model"
)

// AssistantAuditRecord 是可生成反向单的已应用条目，不包含凭证。
type AssistantAuditRecord struct {
	ID     uint64
	Target string
	Key    string
	Detail model.JSONText
}

// RecentModelUsage 统计近段时间模型用量，包括生成任务和对话调用，不返回用户内容。
func (r *AdminAssistantRepository) RecentModelUsage(ctx context.Context, since time.Time) (map[string]int64, error) {
	out := map[string]int64{}
	for _, table := range []string{"generation_tasks", "agent_model_calls"} {
		column := "model_key"
		if table == "generation_tasks" {
			column = "model_id"
		}
		var rows []struct {
			ModelKey string
			Total    int64
		}
		if err := r.db.WithContext(ctx).Table(table).Select(column+" AS model_key, COUNT(*) AS total").Where("created_at >= ?", since).Group(column).Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			out[row.ModelKey] += row.Total
		}
	}
	return out, nil
}

// AdminAssistantRepository 读取助手已写入的两类审计。
type AdminAssistantRepository struct{ db *gorm.DB }

// NewAdminAssistantRepository 创建助手审计读取仓储。
func NewAdminAssistantRepository(db *gorm.DB) *AdminAssistantRepository {
	return &AdminAssistantRepository{db: db}
}

// ChangeAudits 查指定变更单的已应用条目，按各自审计 ID 排序；没有返回空切片。
func (r *AdminAssistantRepository) ChangeAudits(ctx context.Context, order string) ([]AssistantAuditRecord, error) {
	var ai []model.AIAuditLog
	if err := r.db.WithContext(ctx).Where("detail_json->>'via' = 'assistant' AND detail_json->>'order_no' = ?", order).Order("id ASC").Find(&ai).Error; err != nil {
		return nil, err
	}
	out := make([]AssistantAuditRecord, 0, len(ai))
	for _, row := range ai {
		target := row.TargetType
		var detail struct {
			Target string `json:"target"`
		}
		_ = json.Unmarshal(row.DetailJSON, &detail)
		if detail.Target != "" {
			target = detail.Target
		}
		out = append(out, AssistantAuditRecord{ID: row.ID, Target: target, Key: row.TargetKey, Detail: row.DetailJSON})
	}
	var settings []model.AdminAuditLog
	if err := r.db.WithContext(ctx).Where("action = ? AND detail_json->>'via' = 'assistant' AND detail_json->>'order_no' = ?", model.AdminAuditSettingsPricing, order).Order("id ASC").Find(&settings).Error; err != nil {
		return nil, err
	}
	for _, row := range settings {
		out = append(out, AssistantAuditRecord{ID: row.ID, Target: "pricing_settings", Key: "pricing", Detail: row.DetailJSON})
	}
	return out, nil
}
