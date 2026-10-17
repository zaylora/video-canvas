package repository

import (
	"context"

	"video-canvas/internal/model"
)

// ListChannels 返回已有配置的全部渠道（没配置过的渠道没有行），按 id 升序。
func (r *PaymentRepository) ListChannels(ctx context.Context) ([]model.PaymentChannel, error) {
	var out []model.PaymentChannel
	err := r.db.WithContext(ctx).Order("id ASC").Find(&out).Error
	return out, err
}

// GetChannel 按 provider 查渠道配置，不存在返回 ErrNotFound。
func (r *PaymentRepository) GetChannel(ctx context.Context, provider string) (*model.PaymentChannel, error) {
	var ch model.PaymentChannel
	if err := r.db.WithContext(ctx).Where("provider = ?", provider).First(&ch).Error; err != nil {
		return nil, translate(err)
	}
	return &ch, nil
}

// ListProducts 返回商品，按 sort、id 升序；onSaleOnly 为真时只返回上架的。
func (r *PaymentRepository) ListProducts(ctx context.Context, onSaleOnly bool) ([]model.RechargeProduct, error) {
	var out []model.RechargeProduct
	q := r.db.WithContext(ctx).Order("sort ASC, id ASC")
	if onSaleOnly {
		q = q.Where("on_sale = TRUE")
	}
	err := q.Find(&out).Error
	return out, err
}

// GetProduct 按 id 查商品，不存在返回 ErrNotFound。
func (r *PaymentRepository) GetProduct(ctx context.Context, id uint64) (*model.RechargeProduct, error) {
	var p model.RechargeProduct
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&p).Error; err != nil {
		return nil, translate(err)
	}
	return &p, nil
}
