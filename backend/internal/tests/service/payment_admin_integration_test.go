package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
)

// 本文件是后台支付管理的真实仓储集成测试：乐观锁、唯一索引、删除检查、排序、联表分页在真实 PostgreSQL 上的行为。
// 需要 TEST_DATABASE_DSN，未设置时跳过。

func TestPaymentRepoIntegration_ChannelOptimisticLock(t *testing.T) {
	ctx := context.Background()
	db := payTestDB(t)
	repo := repository.NewPaymentRepository(db)
	ch := &model.PaymentChannel{Provider: "epay", OrderTimeoutMinutes: 15, Version: 1}

	if err := repo.CreateChannel(ctx, ch); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateChannel(ctx, &model.PaymentChannel{Provider: "epay", Version: 1}); !errors.Is(err, repository.ErrRevisionConflict) {
		t.Fatalf("同一渠道第二次创建应返回乐观锁冲突（并发的首次保存）：%v", err)
	}
	if err := repo.UpdateChannelIf(ctx, "epay", 1, map[string]any{"order_timeout_minutes": 30}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateChannelIf(ctx, "epay", 1, map[string]any{"order_timeout_minutes": 40}); !errors.Is(err, repository.ErrRevisionConflict) {
		t.Fatalf("旧版本更新必须被拒绝：%v", err)
	}
	got, _ := repo.GetChannel(ctx, "epay")
	if got.Version != 2 || got.OrderTimeoutMinutes != 30 {
		t.Fatalf("%+v", got)
	}

	// 校验只在版本没变时生效：校验期间配置被改，不能把新配置标成已校验
	if err := repo.SetChannelValidated(ctx, "epay", 1, time.Now()); !errors.Is(err, repository.ErrRevisionConflict) {
		t.Fatalf("%v", err)
	}
	if err := repo.SetChannelValidated(ctx, "epay", 2, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got, _ = repo.GetChannel(ctx, "epay"); got.ValidatedAt == nil {
		t.Fatal("应记录校验时间")
	}
	// 清空校验：传 nil 要真的写成 NULL
	if err := repo.UpdateChannelIf(ctx, "epay", 2, map[string]any{"enabled": false, "validated_at": nil}); err != nil {
		t.Fatal(err)
	}
	if got, _ = repo.GetChannel(ctx, "epay"); got.ValidatedAt != nil {
		t.Fatalf("validated_at 应被清空：%v", got.ValidatedAt)
	}
	if err := repo.SetChannelEnabled(ctx, "nope", true); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("%v", err)
	}
}

func TestPaymentRepoIntegration_Products(t *testing.T) {
	ctx := context.Background()
	db := payTestDB(t)
	repo := repository.NewPaymentRepository(db)
	var ids []uint64
	for i, name := range []string{"A", "B", "C"} {
		p := &model.RechargeProduct{Name: name, PriceFen: 100, BaseCredits: 100, OnSale: true, Version: 1, Sort: i}
		if err := repo.CreateProduct(ctx, p); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, p.ID)
	}

	// 乐观锁
	if err := repo.UpdateProductIf(ctx, ids[0], 1, map[string]any{"name": "A2"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateProductIf(ctx, ids[0], 1, map[string]any{"name": "A3"}); !errors.Is(err, repository.ErrRevisionConflict) {
		t.Fatalf("%v", err)
	}
	if err := repo.UpdateProductIf(ctx, 99999, 1, map[string]any{"name": "x"}); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("商品不存在应返回 ErrNotFound，而不是冲突：%v", err)
	}

	// 排序：全集才行
	if err := repo.SetProductSort(ctx, []uint64{ids[2], ids[0], ids[1]}); err != nil {
		t.Fatal(err)
	}
	list, _ := repo.ListProducts(ctx, false)
	if list[0].ID != ids[2] || list[1].ID != ids[0] || list[2].ID != ids[1] {
		t.Fatalf("排序结果不对：%+v", list)
	}
	for _, bad := range [][]uint64{{ids[0]}, {ids[0], ids[0], ids[1]}, {ids[0], ids[1], 99999}} {
		if err := repo.SetProductSort(ctx, bad); !errors.Is(err, repository.ErrOrderMismatch) {
			t.Fatalf("%v：%v", bad, err)
		}
	}
	if list, _ = repo.ListProducts(ctx, false); list[0].ID != ids[2] {
		t.Fatal("被拒绝的排序不能改动任何数据")
	}

	// 下架的不出现在用户侧列表
	_ = repo.UpdateProductIf(ctx, ids[1], 1, map[string]any{"on_sale": false})
	if onSale, _ := repo.ListProducts(ctx, true); len(onSale) != 2 {
		t.Fatalf("只应有 2 个上架商品：%d", len(onSale))
	}

	// 删除：有订单的不能删
	db.Create(&model.PaymentOrder{OrderNo: "VC-D1", UserID: 1, IdempotencyKey: "k-d1-0001", ProductID: ids[0], Status: model.PayOrderCredited, Provider: "epay", Method: "wechat", ExpiresAt: time.Now()})
	if err := repo.DeleteProduct(ctx, ids[0]); !errors.Is(err, repository.ErrInUse) {
		t.Fatalf("有订单的商品不能删：%v", err)
	}
	if err := repo.DeleteProduct(ctx, ids[2]); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteProduct(ctx, ids[2]); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("%v", err)
	}
}

func TestPaymentRepoIntegration_OrderListing(t *testing.T) {
	ctx := context.Background()
	db := payTestDB(t)
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewPaymentRepository(db)
	db.Create(&model.User{ID: 11, Username: "alice_pay", Password: "x"})
	db.Create(&model.User{ID: 12, Username: "bob", Password: "x"})
	base := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	mk := func(no string, user uint64, status, method string, at time.Time) {
		db.Create(&model.PaymentOrder{
			OrderNo: no, UserID: user, IdempotencyKey: "k-" + no, ProductID: 1, Status: status, Method: method, Provider: "epay",
			ExpiresAt: at.Add(time.Hour), CreatedAt: at, ProductSnapshot: []byte(`{"name":"X"}`),
		})
	}
	mk("VC-L1", 11, model.PayOrderPending, "wechat", base)
	mk("VC-L2", 11, model.PayOrderCredited, "alipay", base.Add(time.Hour))
	mk("VC-L3", 12, model.PayOrderCredited, "wechat", base.Add(2*time.Hour))

	rows, total, err := repo.ListOrders(ctx, repository.PaymentOrderFilter{Limit: 10})
	if err != nil || total != 3 || len(rows) != 3 || rows[0].OrderNo != "VC-L3" || rows[0].Username != "bob" {
		t.Fatalf("按 id 倒序并带出用户名：%+v %d %v", rows, total, err)
	}
	cases := map[string]struct {
		f    repository.PaymentOrderFilter
		want int64
	}{
		"按订单号精确":      {repository.PaymentOrderFilter{Q: "VC-L2", Limit: 10}, 1},
		"按用户名模糊（含 _）": {repository.PaymentOrderFilter{Q: "alice_", Limit: 10}, 2},
		"下划线不是通配符":    {repository.PaymentOrderFilter{Q: "alice_x", Limit: 10}, 0},
		"按状态":         {repository.PaymentOrderFilter{Status: model.PayOrderCredited, Limit: 10}, 2},
		"按支付方式":       {repository.PaymentOrderFilter{Method: "wechat", Limit: 10}, 2},
		"按时间段":        {repository.PaymentOrderFilter{From: ptr(base.Add(30 * time.Minute)), To: ptr(base.Add(90 * time.Minute)), Limit: 10}, 1},
	}
	for name, c := range cases {
		if _, total, err := repo.ListOrders(ctx, c.f); err != nil || total != c.want {
			t.Errorf("%s：期望 %d 条，实际 %d %v", name, c.want, total, err)
		}
	}
	if rows, total, _ = repo.ListOrders(ctx, repository.PaymentOrderFilter{Limit: 2, Offset: 2}); total != 3 || len(rows) != 1 {
		t.Fatalf("分页：总数不受 limit 影响：%d %d", total, len(rows))
	}
	one, err := repo.FindOrderWithUser(ctx, "VC-L1")
	if err != nil || one.Username != "alice_pay" {
		t.Fatalf("%+v %v", one, err)
	}
	if _, err := repo.FindOrderWithUser(ctx, "VC-NONE"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("%v", err)
	}
}

func TestPaymentRepoIntegration_OpenOrderCounts(t *testing.T) {
	ctx := context.Background()
	db := payTestDB(t)
	repo := repository.NewPaymentRepository(db)
	mk := func(no string, user uint64, provider, status string) {
		db.Create(&model.PaymentOrder{OrderNo: no, UserID: user, IdempotencyKey: "k-" + no, ProductID: 1, Status: status, Method: "wechat", Provider: provider, ExpiresAt: time.Now()})
	}
	mk("VC-O1", 1, "epay", model.PayOrderPending)
	mk("VC-O2", 1, "epay", model.PayOrderCreating)
	mk("VC-O3", 1, "epay", model.PayOrderClosed)
	mk("VC-O4", 2, "xunhupay", model.PayOrderPending)
	if n, _ := repo.CountOpenOrders(ctx, 1); n != 2 {
		t.Fatalf("用户 1 进行中的订单应为 2（creating + pending，closed 不算）：%d", n)
	}
	if n, _ := repo.CountOpenOrdersByProvider(ctx, "epay"); n != 2 {
		t.Fatalf("%d", n)
	}
	if o, err := repo.LatestOpenOrder(ctx, 1); err != nil || o.OrderNo != "VC-O2" {
		t.Fatalf("最近一笔进行中的订单：%+v %v", o, err)
	}
	if _, err := repo.LatestOpenOrder(ctx, 99); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("%v", err)
	}
}
