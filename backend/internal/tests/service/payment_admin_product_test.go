package service_test

import (
	"context"
	"strings"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
)

func ptr[T any](v T) *T { return &v }

func TestPaymentAdmin_Products(t *testing.T) {
	ctx := context.Background()
	valid := model.PaymentProductReq{Name: " 20,000 积分 ", PriceFen: 20000, BaseCredits: 20000, BonusCredits: 1000, Badge: "热门"}

	t.Run("新建：默认上架，排在最后，去掉名称首尾空白，写审计", func(t *testing.T) {
		e := newAdminEnv(t)
		e.store.products[1].Sort = 4
		p, err := e.admin.CreateProduct(ctx, 9, &valid)
		if err != nil || p.Name != "20,000 积分" || !p.OnSale || p.Sort != 5 || p.Version != 1 || p.SoldCount != 0 || p.ID == 0 {
			t.Fatalf("%+v %v", p, err)
		}
		if acts := e.audit.actions(); len(acts) != 1 || acts[0] != model.AdminAuditPayProductCreate {
			t.Fatalf("%v", acts)
		}
	})
	t.Run("新建：可以指定下架", func(t *testing.T) {
		e := newAdminEnv(t)
		r := valid
		r.OnSale = ptr(false)
		if p, err := e.admin.CreateProduct(ctx, 9, &r); err != nil || p.OnSale {
			t.Fatalf("%+v %v", p, err)
		}
	})
	bad := map[string]func(r *model.PaymentProductReq){
		"名称只有空白":    func(r *model.PaymentProductReq) { r.Name = "   " },
		"售价不是正数":    func(r *model.PaymentProductReq) { r.PriceFen = 0 },
		"基础积分不是正数":  func(r *model.PaymentProductReq) { r.BaseCredits = 0 },
		"赠送积分是负数":   func(r *model.PaymentProductReq) { r.BonusCredits = -1 },
		"基础加赠送超过上限": func(r *model.PaymentProductReq) { r.BaseCredits, r.BonusCredits = 900_000_000, 200_000_000 },
		"角标超长":      func(r *model.PaymentProductReq) { r.Badge = strings.Repeat("热", 9) },
	}
	for name, mutate := range bad {
		t.Run("新建："+name+"返回 10001", func(t *testing.T) {
			e := newAdminEnv(t)
			r := valid
			mutate(&r)
			_, err := e.admin.CreateProduct(ctx, 9, &r)
			wantPayCode(t, err, errcode.ErrInvalidParams.Code)
		})
	}

	t.Run("修改：带版本，成功后版本 +1，已有订单也可改价（订单按快照执行），写审计", func(t *testing.T) {
		e := newAdminEnv(t)
		e.store.products[1].Version = 2
		e.store.orders[900] = &model.PaymentOrder{ID: 900, OrderNo: "X", ProductID: 1, Status: model.PayOrderCredited}
		r := valid
		r.Version = 2
		p, err := e.admin.UpdateProduct(ctx, 9, 1, &r)
		if err != nil || p.PriceFen != 20000 || p.Version != 3 || p.Badge != "热门" {
			t.Fatalf("%+v %v", p, err)
		}
		if acts := e.audit.actions(); len(acts) != 1 || acts[0] != model.AdminAuditPayProductUpdate {
			t.Fatalf("%v", acts)
		}
	})
	t.Run("修改：不传 on_sale 保持原状态；传了就改", func(t *testing.T) {
		e := newAdminEnv(t)
		r := valid
		r.Version = 0
		e.store.products[1].Version = 0
		if p, err := e.admin.UpdateProduct(ctx, 9, 1, &r); err != nil || !p.OnSale {
			t.Fatalf("不传 on_sale 应保持上架：%+v %v", p, err)
		}
		r.Version, r.OnSale = 1, ptr(false)
		if p, err := e.admin.UpdateProduct(ctx, 9, 1, &r); err != nil || p.OnSale {
			t.Fatalf("%+v %v", p, err)
		}
	})
	t.Run("修改：版本冲突返回 65011，商品不存在返回 65003", func(t *testing.T) {
		e := newAdminEnv(t)
		r := valid
		r.Version = 99
		_, err := e.admin.UpdateProduct(ctx, 9, 1, &r)
		wantPayCode(t, err, errcode.ErrPayVersionConflict.Code)
		_, err = e.admin.UpdateProduct(ctx, 9, 777, &r)
		wantPayCode(t, err, errcode.ErrProductUnavailable.Code)
	})

	t.Run("删除：没有订单可以删，写审计", func(t *testing.T) {
		e := newAdminEnv(t)
		if err := e.admin.DeleteProduct(ctx, 9, 2); err != nil {
			t.Fatal(err)
		}
		if _, ok := e.store.products[2]; ok {
			t.Fatal("应已删除")
		}
		if acts := e.audit.actions(); len(acts) != 1 || acts[0] != model.AdminAuditPayProductDelete {
			t.Fatalf("%v", acts)
		}
	})
	t.Run("删除：已有订单返回 65012，只能下架；不存在返回 65003", func(t *testing.T) {
		e := newAdminEnv(t)
		e.store.orders[900] = &model.PaymentOrder{ID: 900, OrderNo: "X", ProductID: 1, Status: model.PayOrderCredited}
		wantPayCode(t, e.admin.DeleteProduct(ctx, 9, 1), errcode.ErrProductHasOrders.Code)
		wantPayCode(t, e.admin.DeleteProduct(ctx, 9, 777), errcode.ErrProductUnavailable.Code)
		if len(e.audit.actions()) != 0 {
			t.Fatal("失败不写审计")
		}
	})

	t.Run("排序：按 ids 顺序重排；不是全集返回 10001", func(t *testing.T) {
		e := newAdminEnv(t)
		if err := e.admin.SortProducts(ctx, 9, []uint64{2, 1}); err != nil {
			t.Fatal(err)
		}
		if e.store.products[2].Sort != 0 || e.store.products[1].Sort != 1 {
			t.Fatalf("排序结果不对：%d %d", e.store.products[2].Sort, e.store.products[1].Sort)
		}
		for _, ids := range [][]uint64{{1}, {1, 1}, {1, 9}, {1, 2, 3}} {
			wantPayCode(t, e.admin.SortProducts(ctx, 9, ids), errcode.ErrInvalidParams.Code)
		}
	})
	t.Run("列表：包含已下架的商品，按 sort 排序", func(t *testing.T) {
		e := newAdminEnv(t)
		list, err := e.admin.ListProducts(ctx)
		if err != nil || len(list) != 2 {
			t.Fatalf("后台要看到全部商品（含下架）：%d %v", len(list), err)
		}
	})
}

func TestPaymentAdmin_Orders(t *testing.T) {
	ctx := context.Background()

	t.Run("列表：筛选条件传给仓储，分页默认每页 20，商品名取自快照", func(t *testing.T) {
		e := newAdminEnv(t)
		v := e.order("idem-0001")
		items, total, q, err := e.admin.ListOrders(ctx, &model.ListPaymentOrdersReq{
			AdminListReq: model.AdminListReq{Q: v.OrderNo}, Status: "pending", Method: "wechat", Provider: model.ProviderWechatNative,
			From: "2026-10-16T00:00:00+08:00", To: "2026-10-17T00:00:00+08:00",
		})
		if err != nil || total != 1 || len(items) != 1 || q.PageSize != 20 || q.Page != 1 {
			t.Fatalf("%+v %d %+v %v", items, total, q, err)
		}
		it := items[0]
		if it.OrderNo != v.OrderNo || it.ProductName != "10,000 积分" || it.ProviderName != "微信支付 Native" || it.Username == "" || it.AmountFen != 10000 || it.Credits != 10500 {
			t.Fatalf("%+v", it)
		}
		f := e.store.lastFilter
		if f.Q != v.OrderNo || f.Status != "pending" || f.Method != "wechat" || f.Provider != model.ProviderWechatNative || f.From == nil || f.To == nil || f.Limit != 20 || f.Offset != 0 {
			t.Fatalf("筛选条件没传对：%+v", f)
		}
	})
	t.Run("列表：时间格式错返回 10001", func(t *testing.T) {
		e := newAdminEnv(t)
		_, _, _, err := e.admin.ListOrders(ctx, &model.ListPaymentOrdersReq{From: "yesterday"})
		wantPayCode(t, err, errcode.ErrInvalidParams.Code)
	})
	t.Run("详情：订单 + 收到的通知记录", func(t *testing.T) {
		e := newAdminEnv(t)
		v := e.order("idem-0001")
		e.paid(v.OrderNo, 10000)
		e.notify(payTestWechat)
		d, err := e.admin.GetOrder(ctx, v.OrderNo)
		if err != nil || d.OrderNo != v.OrderNo || d.Status != model.PayOrderCredited || len(d.Notifications) != 1 || d.Notifications[0].Result != model.PayNotifyCredited {
			t.Fatalf("%+v %v", d, err)
		}
		if d.ProviderTradeNo == "" || d.PaidAmountFen != 10000 {
			t.Fatalf("%+v", d)
		}
	})
	t.Run("详情：订单不存在返回 65006", func(t *testing.T) {
		e := newAdminEnv(t)
		_, err := e.admin.GetOrder(ctx, "VC-GHOST")
		wantPayCode(t, err, errcode.ErrPayOrderNotFound.Code)
	})
}
