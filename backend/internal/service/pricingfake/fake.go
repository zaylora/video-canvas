// Package pricingfake 提供定价服务和 handler 测试共用的事务内存仓储。
package pricingfake

import (
	"context"
	"encoding/json"
	"fmt"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
)

// Repo 模拟配置和审计一同提交或回滚。
type Repo struct {
	Values   map[string]string
	Logs     []*model.AdminAuditLog
	ReadErr  error
	WriteErr error
	AuditErr error
}

// New 返回空测试仓储。
func New() *Repo { return &Repo{Values: map[string]string{}} }

// GetAll 返回副本以免测试意外跳过事务直接修改存储。
func (r *Repo) GetAll(context.Context) (map[string]string, error) {
	out := map[string]string{}
	for k, v := range r.Values {
		out[k] = v
	}
	return out, r.ReadErr
}

// SetMany 模拟批量写入。
func (r *Repo) SetMany(_ context.Context, values map[string]string, _ uint64) error {
	if r.WriteErr != nil {
		return r.WriteErr
	}
	for k, v := range values {
		r.Values[k] = v
	}
	return nil
}

// Insert 模拟事务审计失败。
func (r *Repo) Insert(_ context.Context, row *model.AdminAuditLog) error {
	if r.AuditErr != nil {
		return r.AuditErr
	}
	row.ID = uint64(len(r.Logs) + 1)
	r.Logs = append(r.Logs, row)
	return nil
}

// FindApplied 返回匹配审批条目，未找到为 ErrNotFound。
func (r *Repo) FindApplied(_ context.Context, aid string, index int) (*model.AdminAuditLog, error) {
	for _, row := range r.Logs {
		var detail map[string]any
		_ = json.Unmarshal(row.DetailJSON, &detail)
		if detail["approval_id"] == aid && fmt.Sprint(detail["index"]) == fmt.Sprint(index) {
			return row, nil
		}
	}
	return nil, repository.ErrNotFound
}

// WithPricingTx 只有事务回调成功时才替换持久状态。
func (r *Repo) WithPricingTx(ctx context.Context, fn func(repository.PricingTx) error) error {
	values, _ := r.GetAll(ctx)
	tx := &Repo{Values: values, Logs: append([]*model.AdminAuditLog{}, r.Logs...), ReadErr: r.ReadErr, WriteErr: r.WriteErr, AuditErr: r.AuditErr}
	if err := fn(tx); err != nil {
		return err
	}
	r.Values, r.Logs = tx.Values, tx.Logs
	return nil
}
