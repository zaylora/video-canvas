package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"video-canvas/internal/model"
)

// ErrAdminOperationUncertain 表示条目已开始但未确认完成，必须核对业务现状后新建审批。
var ErrAdminOperationUncertain = errors.New("后台操作结果未确认")

// ErrAdminOperationFailed 表示同一审批条目此前失败，禁止自动重放副作用。
var ErrAdminOperationFailed = errors.New("后台操作此前已失败")

// ExecuteAdminOperation 为审批 ID + 下标持久化一次性执行占位，避免并发与崩溃重放重复发积分或邮件。
// 网络与业务调用在事务外执行；业务服务继续写自己的审计。执行中断不重试，调用方应先核对现状。
func (r *AdminAssistantRepository) ExecuteAdminOperation(ctx context.Context, audit *model.AIAuditLog, run func() error) (uint64, error) {
	var detail map[string]any
	if err := json.Unmarshal(audit.DetailJSON, &detail); err != nil {
		return 0, err
	}
	var prior model.AIAuditLog
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		key := fmt.Sprint("assistant:", detail["approval_id"], ":", detail["index"])
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", key).Error; err != nil {
			return err
		}
		err := tx.Where("detail_json->>'via' = 'assistant' AND detail_json->>'approval_id' = ? AND detail_json->>'index' = ?", detail["approval_id"], fmt.Sprint(detail["index"])).Order("id DESC").First(&prior).Error
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		detail["state"] = "running"
		raw, err := json.Marshal(detail)
		if err != nil {
			return err
		}
		audit.DetailJSON = model.JSONText(raw)
		return tx.Create(audit).Error
	})
	if err != nil {
		return 0, err
	}
	if prior.ID != 0 {
		var previous struct {
			State string `json:"state"`
		}
		if err := json.Unmarshal(prior.DetailJSON, &previous); err != nil {
			return 0, err
		}
		switch previous.State {
		case "applied":
			return prior.ID, nil
		case "failed":
			return 0, ErrAdminOperationFailed
		default:
			return 0, ErrAdminOperationUncertain
		}
	}
	err = run()
	detail["state"] = "applied"
	if err != nil {
		detail["state"] = "failed"
	}
	raw, marshalErr := json.Marshal(detail)
	if marshalErr != nil {
		return 0, marshalErr
	}
	// 完成记录另写一条，保留开始时的审计；结果写入失败时，开始记录仍阻止自动重放。
	finished := model.AIAuditLog{ActorID: audit.ActorID, Action: audit.Action, TargetType: audit.TargetType, TargetKey: audit.TargetKey, DetailJSON: model.JSONText(raw)}
	if saveErr := r.db.WithContext(ctx).Create(&finished).Error; saveErr != nil {
		return 0, fmt.Errorf("记录后台操作结果失败: %w", saveErr)
	}
	if err != nil {
		return 0, err
	}
	return finished.ID, nil
}
