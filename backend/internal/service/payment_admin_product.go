package service

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

const (
	maxProductCredits = 1_000_000_000 // 单个商品「基础 + 赠送」的积分上限：积分账户用 int，留足余量
	maxProductName    = 64
	maxProductBadge   = 8
)

// ListProducts 返回全部充值商品（含已下架的），按 sort、id 升序。
func (s *PaymentAdminService) ListProducts(ctx context.Context) ([]model.RechargeProduct, error) {
	return s.products.ListProducts(ctx, false)
}

// validateProduct 校验并整理商品请求：名称和角标去首尾空白，价格和积分必须是合法正数，积分合计不能超上限。
func validateProduct(req *model.PaymentProductReq) (name, badge string, err error) {
	name, badge = strings.TrimSpace(req.Name), strings.TrimSpace(req.Badge)
	switch {
	case name == "" || utf8.RuneCountInString(name) > maxProductName:
		return "", "", errcode.ErrInvalidParams.WithMsg("商品名称不能为空，且不超过 64 个字")
	case utf8.RuneCountInString(badge) > maxProductBadge:
		return "", "", errcode.ErrInvalidParams.WithMsg("角标不超过 8 个字")
	case req.PriceFen <= 0:
		return "", "", errcode.ErrInvalidParams.WithMsg("售价必须大于 0")
	case req.BaseCredits <= 0:
		return "", "", errcode.ErrInvalidParams.WithMsg("基础积分必须大于 0")
	case req.BonusCredits < 0:
		return "", "", errcode.ErrInvalidParams.WithMsg("赠送积分不能为负")
	case req.BaseCredits+req.BonusCredits > maxProductCredits:
		return "", "", errcode.ErrInvalidParams.WithMsg("基础积分与赠送积分之和过大")
	}
	return name, badge, nil
}

// CreateProduct 新建商品：默认上架，排在最后。写审计。
func (s *PaymentAdminService) CreateProduct(ctx context.Context, actorID uint64, req *model.PaymentProductReq) (*model.RechargeProduct, error) {
	name, badge, err := validateProduct(req)
	if err != nil {
		return nil, err
	}
	existing, err := s.products.ListProducts(ctx, false)
	if err != nil {
		return nil, err
	}
	next := 0
	for _, p := range existing {
		next = max(next, p.Sort+1)
	}
	p := &model.RechargeProduct{
		Name: name, PriceFen: req.PriceFen, BaseCredits: req.BaseCredits, BonusCredits: req.BonusCredits, Badge: badge,
		Sort: next, OnSale: req.OnSale == nil || *req.OnSale, Version: 1,
	}
	if err := s.products.CreateProduct(ctx, p); err != nil {
		return nil, err
	}
	adminAudit(ctx, s.audit, actorID, model.AdminAuditPayProductCreate, model.AdminAuditTargetPayment, p.ID, productDetail(p))
	return p, nil
}

// UpdateProduct 修改商品（乐观锁）。已有订单的商品也可以改：订单下单时锁定了价格和积分的快照，改价只影响之后的新订单。
// 请求里不带 on_sale 表示保持原来的上下架状态。写审计。
func (s *PaymentAdminService) UpdateProduct(ctx context.Context, actorID, id uint64, req *model.PaymentProductReq) (*model.RechargeProduct, error) {
	name, badge, err := validateProduct(req)
	if err != nil {
		return nil, err
	}
	fields := map[string]any{
		"name": name, "price_fen": req.PriceFen, "base_credits": req.BaseCredits, "bonus_credits": req.BonusCredits, "badge": badge,
	}
	if req.OnSale != nil {
		fields["on_sale"] = *req.OnSale
	}
	if err := s.products.UpdateProductIf(ctx, id, req.Version, fields); err != nil {
		return nil, mapProductErr(err)
	}
	p, err := s.products.GetProduct(ctx, id)
	if err != nil {
		return nil, mapProductErr(err)
	}
	adminAudit(ctx, s.audit, actorID, model.AdminAuditPayProductUpdate, model.AdminAuditTargetPayment, id, productDetail(p))
	return p, nil
}

// DeleteProduct 删除商品；已有订单的商品只能下架，返回 65012。写审计。
func (s *PaymentAdminService) DeleteProduct(ctx context.Context, actorID, id uint64) error {
	if err := s.products.DeleteProduct(ctx, id); err != nil {
		return mapProductErr(err)
	}
	adminAudit(ctx, s.audit, actorID, model.AdminAuditPayProductDelete, model.AdminAuditTargetPayment, id, nil)
	return nil
}

// SortProducts 按 ids 的顺序重排商品；ids 必须是全部商品 id 的一个排列。写审计。
func (s *PaymentAdminService) SortProducts(ctx context.Context, actorID uint64, ids []uint64) error {
	if err := s.products.SetProductSort(ctx, ids); err != nil {
		if errors.Is(err, repository.ErrOrderMismatch) {
			return errcode.ErrInvalidParams.WithMsg("排序列表必须恰好包含全部商品，且不能重复")
		}
		return err
	}
	adminAudit(ctx, s.audit, actorID, model.AdminAuditPayProductSort, model.AdminAuditTargetPayment, 0, map[string]any{"ids": ids})
	return nil
}

// mapProductErr 把仓储错误翻译成业务错误。
func mapProductErr(err error) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return errcode.ErrProductUnavailable
	case errors.Is(err, repository.ErrRevisionConflict):
		return errcode.ErrPayVersionConflict
	case errors.Is(err, repository.ErrInUse):
		return errcode.ErrProductHasOrders
	}
	return err
}

// productDetail 是商品的审计详情。
func productDetail(p *model.RechargeProduct) map[string]any {
	return map[string]any{
		"name": p.Name, "price_fen": p.PriceFen, "base_credits": p.BaseCredits, "bonus_credits": p.BonusCredits, "on_sale": p.OnSale,
	}
}
