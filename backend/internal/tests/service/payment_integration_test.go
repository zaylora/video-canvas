package service_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"video-canvas/internal/model"
	"video-canvas/internal/payment"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/repository"
	. "video-canvas/internal/service"
)

// 本文件是支付服务 + 真实仓储的集成测试：验证入账事务、幂等和并发在真实 PostgreSQL 上的行为。
// 需要 TEST_DATABASE_DSN，未设置时跳过。

var paySchemaSeq atomic.Int64

// payTestDB 为本用例创建独立 schema 并迁移支付和积分相关的表，用例结束后整体删除。
func payTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("未设置 TEST_DATABASE_DSN，跳过集成测试")
	}
	quiet := &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)}
	admin, err := gorm.Open(postgres.Open(dsn), quiet)
	if err != nil {
		t.Fatalf("连接测试库失败：%v", err)
	}
	schema := fmt.Sprintf("pay_%d_%d", time.Now().UnixNano()%1_000_000_000, paySchemaSeq.Add(1))
	if err := admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatalf("创建 schema 失败：%v", err)
	}
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), quiet)
	if err != nil {
		t.Fatalf("连接测试 schema 失败：%v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(30)
	t.Cleanup(func() {
		_ = sqlDB.Close()
		_ = admin.Exec("DROP SCHEMA " + schema + " CASCADE").Error
		if a, err := admin.DB(); err == nil {
			_ = a.Close()
		}
	})
	err = db.AutoMigrate(&model.PaymentChannel{}, &model.RechargeProduct{}, &model.PaymentOrder{}, &model.PaymentNotification{}, &model.UserCredit{}, &model.CreditLedger{}, &model.GenerationTask{})
	if err != nil {
		t.Fatalf("迁移失败：%v", err)
	}
	return db
}

// payRealEnv 用真实仓储组装支付服务，渠道适配器仍是 fake（不连外网）。
type payRealEnv struct {
	svc *PaymentService
	db  *gorm.DB
	wx  *fakeProvider
	bc  *recordingBroadcaster
}

func newPayRealEnv(t *testing.T) *payRealEnv {
	t.Helper()
	db := payTestDB(t)
	repo := repository.NewPaymentRepository(db)
	kv := newFakeSettingsRepo()
	kv.kv = map[string]string{
		model.SettingPaymentEnabled: "true", model.SettingPaymentPublicBaseURL: payTestBase,
		model.SettingPaymentMethodWechat: model.ProviderWechatNative,
	}
	wx := newFakeProvider(model.ProviderWechatNative, payment.MethodWechat)
	cfg, _ := json.Marshal(map[string]string{"app_id": "wx"})
	if err := db.Create(&model.PaymentChannel{Provider: model.ProviderWechatNative, Enabled: true, ConfigJSON: cfg, OrderTimeoutMinutes: 15, Version: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.RechargeProduct{ID: 1, Name: "10,000 积分", PriceFen: 10000, BaseCredits: 10000, BonusCredits: 500, OnSale: true, Version: 1}).Error; err != nil {
		t.Fatal(err)
	}
	bc := &recordingBroadcaster{}
	svc := NewPaymentService(PaymentDeps{
		Orders: repo, Channels: repo, Products: repo, Settings: kv, Secrets: &fakeSecrets{kv: map[string]string{}},
		Registry: payment.NewRegistry(wx), Credits: fakeInitialCredits{n: 50}, Broadcaster: bc,
	})
	return &payRealEnv{svc: svc, db: db, wx: wx, bc: bc}
}

func (e *payRealEnv) order(t *testing.T, user uint64, key string) *model.PaymentOrderView {
	t.Helper()
	v, err := e.svc.CreateOrder(context.Background(), user, &model.CreatePaymentOrderReq{ProductID: 1, Method: "wechat", IdempotencyKey: key}, "127.0.0.1")
	if err != nil {
		t.Fatalf("下单失败：%v", err)
	}
	return v
}

func (e *payRealEnv) notifyPaid(no string, fen int, eventKey string) payment.Ack {
	e.wx.mu.Lock()
	e.wx.verifyRes = payment.TradeResult{OrderNo: no, TradeNo: "T-" + no, Identity: payTestIdent, State: payment.StatePaid, PaidFen: fen, EventKey: eventKey}
	e.wx.mu.Unlock()
	return e.svc.HandleNotify(context.Background(), payTestWechat, payment.NotifyRequest{Method: "POST"})
}

func (e *payRealEnv) balance(t *testing.T, user uint64) int {
	t.Helper()
	var c model.UserCredit
	if err := e.db.Where("user_id = ?", user).First(&c).Error; err != nil {
		return -1
	}
	return c.Balance
}

func (e *payRealEnv) ledgerCount(t *testing.T, orderNo string) int64 {
	t.Helper()
	var n int64
	e.db.Raw(`SELECT count(*) FROM credit_ledger l JOIN payment_orders o ON o.id = l.payment_order_id WHERE o.order_no = ? AND l.type = 'recharge'`, orderNo).Scan(&n)
	return n
}

func TestPaymentIntegration_ConcurrentNotifyCreditsOnce(t *testing.T) {
	e := newPayRealEnv(t)
	user := uint64(7001)
	v := e.order(t, user, "idem-0001")

	// 回调、重试、补单会同时到：20 个并发入账，积分只能加一次、流水只能有一条
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.notifyPaid(v.OrderNo, 10000, fmt.Sprintf("evt-%d", i))
		}()
	}
	wg.Wait()

	if got := e.balance(t, user); got != 50+10500 {
		t.Fatalf("余额应为初始 50 + 10500，实际 %d", got)
	}
	if n := e.ledgerCount(t, v.OrderNo); n != 1 {
		t.Fatalf("recharge 流水应恰好 1 条，实际 %d", n)
	}
	var o model.PaymentOrder
	e.db.Where("order_no = ?", v.OrderNo).First(&o)
	if o.Status != model.PayOrderCredited || o.CreditedAt == nil || o.ProviderTradeNo != "T-"+v.OrderNo {
		t.Fatalf("订单状态不对：%+v", o)
	}
	var p model.RechargeProduct
	e.db.First(&p, 1)
	if p.SoldCount != 1 {
		t.Fatalf("商品已售应为 1：%d", p.SoldCount)
	}
	if e.bc.count(ws.TypePaymentUpdated) < 1 {
		t.Fatal("入账后应推送 payment.updated")
	}
}

func TestPaymentIntegration_LedgerUniqueIndexIsLastDefence(t *testing.T) {
	e := newPayRealEnv(t)
	user := uint64(7002)
	v := e.order(t, user, "idem-0001")
	e.notifyPaid(v.OrderNo, 10000, "evt-1")
	var o model.PaymentOrder
	e.db.Where("order_no = ?", v.OrderNo).First(&o)

	// 绕过 service 的状态判断，直接对同一订单再写一条 recharge 流水：唯一索引必须拦住
	repo := repository.NewPaymentRepository(e.db)
	inserted, err := repo.InsertLedger(context.Background(), &model.CreditLedger{UserID: user, Type: model.LedgerRecharge, Amount: 10500, PaymentOrderID: &o.ID})
	if err != nil || inserted {
		t.Fatalf("同一订单第二条 recharge 流水必须被唯一索引拦住：inserted=%v err=%v", inserted, err)
	}
}

func TestPaymentIntegration_AmountMismatchKeepsMoneyOut(t *testing.T) {
	e := newPayRealEnv(t)
	user := uint64(7003)
	v := e.order(t, user, "idem-0001")
	e.notifyPaid(v.OrderNo, 9999, "evt-1")

	var o model.PaymentOrder
	e.db.Where("order_no = ?", v.OrderNo).First(&o)
	if o.Status != model.PayOrderAbnormal || o.AbnormalReason != model.PayAbnormalAmount {
		t.Fatalf("%+v", o)
	}
	if e.balance(t, user) != -1 || e.ledgerCount(t, v.OrderNo) != 0 {
		t.Fatal("金额不符不能产生任何入账")
	}
}

func TestPaymentIntegration_TradeNoUniqueAcrossOrders(t *testing.T) {
	e := newPayRealEnv(t)
	a := e.order(t, 7004, "idem-000a")
	b := e.order(t, 7005, "idem-000b")
	e.notifyPaid(a.OrderNo, 10000, "evt-a")

	// 第二个订单拿同一个渠道交易号来入账：被拦下并转 abnormal
	e.wx.mu.Lock()
	e.wx.verifyRes = payment.TradeResult{OrderNo: b.OrderNo, TradeNo: "T-" + a.OrderNo, Identity: payTestIdent, State: payment.StatePaid, PaidFen: 10000, EventKey: "evt-b"}
	e.wx.mu.Unlock()
	e.svc.HandleNotify(context.Background(), payTestWechat, payment.NotifyRequest{})

	var ob model.PaymentOrder
	e.db.Where("order_no = ?", b.OrderNo).First(&ob)
	if ob.Status != model.PayOrderAbnormal || ob.AbnormalReason != model.PayAbnormalTradeNo || e.balance(t, 7005) != -1 {
		t.Fatalf("%+v", ob)
	}
}

func TestPaymentIntegration_IdempotentCreateUnderConcurrency(t *testing.T) {
	e := newPayRealEnv(t)
	var wg sync.WaitGroup
	nos := make(chan string, 10)
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := e.svc.CreateOrder(context.Background(), 7006, &model.CreatePaymentOrderReq{ProductID: 1, Method: "wechat", IdempotencyKey: "idem-same"}, "")
			if err == nil {
				nos <- v.OrderNo
			}
		}()
	}
	wg.Wait()
	close(nos)
	seen := map[string]bool{}
	for no := range nos {
		seen[no] = true
	}
	var n int64
	e.db.Model(&model.PaymentOrder{}).Where("user_id = ?", 7006).Count(&n)
	if n != 1 || len(seen) != 1 {
		t.Fatalf("同一幂等键并发 10 次只能产生 1 笔订单：库里 %d 笔，返回了 %d 个不同订单号", n, len(seen))
	}
}

func TestPaymentIntegration_RechargeLedgerCoexistsWithTaskLedger(t *testing.T) {
	e := newPayRealEnv(t)
	user := uint64(7007)
	// 老用户已有账户：充值直接累加；流水的 task_id 为空，不受 (task_id,type) 索引约束
	e.db.Create(&model.UserCredit{UserID: user, Balance: 1000})
	v := e.order(t, user, "idem-0001")
	e.notifyPaid(v.OrderNo, 10000, "evt-1")
	if got := e.balance(t, user); got != 1000+10500 {
		t.Fatalf("余额 %d", got)
	}
	var led model.CreditLedger
	e.db.Where("user_id = ? AND type = ?", user, model.LedgerRecharge).First(&led)
	if led.TaskID != nil || led.PaymentOrderID == nil || led.Amount != 10500 || led.Note == "" {
		t.Fatalf("%+v", led)
	}
}

func TestPaymentIntegration_ReconcileIncludesRecharge(t *testing.T) {
	e := newPayRealEnv(t)
	user := uint64(7008)
	v := e.order(t, user, "idem-0001")
	e.notifyPaid(v.OrderNo, 10000, "evt-1")

	// 对账公式：余额 = Σinitial + Σadmin_adjust + Σrecharge − Σsettle。充值入账后必须仍然对得上，
	// 否则运营一跑对账就会看到每个充过值的用户都「余额与流水不符」。
	rec, err := repository.NewGenerationTaskRepository(e.db).Reconcile(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}
	if rec.RechargeSum != 10500 || rec.InitialSum != 50 || rec.BalanceDiff() != 0 {
		t.Fatalf("对账不平：%+v diff=%d", rec, rec.BalanceDiff())
	}
}
