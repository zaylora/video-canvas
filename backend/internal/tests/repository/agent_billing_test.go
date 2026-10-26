package repository_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"gorm.io/gorm"

	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
)

func billingDB(t *testing.T) *gorm.DB {
	return isolatedDB(t, &model.UserCredit{}, &model.CreditLedger{}, &model.GenerationTask{}, &model.AgentModelCall{})
}

// openAccount 开一个积分账户并写初始流水，和线上注册流程一致，对账才有基准。
func openAccount(t *testing.T, db *gorm.DB, userID uint64, balance, frozen int) {
	t.Helper()
	if err := db.Create(&model.UserCredit{UserID: userID, Balance: balance, Frozen: frozen}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CreditLedger{UserID: userID, Type: model.LedgerInitial, Amount: balance}).Error; err != nil {
		t.Fatal(err)
	}
}

func newCall(t *testing.T, r *AgentRepository, userID uint64) *model.AgentModelCall {
	t.Helper()
	c := &model.AgentModelCall{RunID: 1, UserID: userID, ModelKey: "claude", Status: model.ModelCallPending}
	if err := r.CreateModelCall(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	return c
}

func account(t *testing.T, db *gorm.DB, userID uint64) model.UserCredit {
	t.Helper()
	var a model.UserCredit
	if err := db.First(&a, "user_id = ?", userID).Error; err != nil {
		t.Fatal(err)
	}
	return a
}

func ledgerOf(t *testing.T, db *gorm.DB, callID uint64) []model.CreditLedger {
	t.Helper()
	var rows []model.CreditLedger
	db.Where("agent_call_id = ?", callID).Order("id").Find(&rows)
	return rows
}

func TestSettleModelCall_ChargesOnceWithPairedLedger(t *testing.T) {
	ctx := context.Background()
	db := billingDB(t)
	r := NewAgentRepository(db)
	openAccount(t, db, 1, 100, 0)
	call := newCall(t, r, 1)

	in := SettleCallInput{CallID: call.ID, UserID: 1, InputTokens: 1200, OutputTokens: 300, CachedTokens: 1000, Credits: 7}
	got, err := r.SettleModelCall(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.ModelCallSettled || got.Charged != 7 || got.Credits != 7 || got.InputTokens != 1200 || got.CachedTokens != 1000 || got.SettledAt == nil {
		t.Fatalf("call=%+v", got)
	}
	if a := account(t, db, 1); a.Balance != 93 || a.Frozen != 0 {
		t.Errorf("余额应 100→93，冻结不变: %+v", a)
	}
	rows := ledgerOf(t, db, call.ID)
	if len(rows) != 2 || rows[0].Type != model.LedgerFreeze || rows[1].Type != model.LedgerSettle || rows[0].Amount != 7 || rows[1].Amount != 7 {
		t.Fatalf("应成对写一条冻结和一条结算流水（金额相同），保持对账等式成立: %+v", rows)
	}
	if rows[0].TaskID != nil || rows[0].AgentCallID == nil || *rows[0].AgentCallID != call.ID {
		t.Errorf("流水应挂在调用上而不是任务上: %+v", rows[0])
	}

	t.Run("重复结算不会再扣", func(t *testing.T) {
		again, err := r.SettleModelCall(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		if again.Charged != 7 || account(t, db, 1).Balance != 93 || len(ledgerOf(t, db, call.ID)) != 2 {
			t.Errorf("幂等失败: %+v 余额=%d", again, account(t, db, 1).Balance)
		}
	})

	t.Run("对账等式仍然成立", func(t *testing.T) {
		rec, err := NewGenerationTaskRepository(db).Reconcile(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		if int64(rec.Balance) != rec.InitialSum+rec.AdminAdjustSum-rec.SettleSum {
			t.Errorf("余额对账不平: %+v", rec)
		}
		if int64(rec.Frozen) != rec.FreezeSum-rec.SettleSum-rec.RefundSum {
			t.Errorf("冻结对账不平: %+v", rec)
		}
		if int64(rec.Frozen) != rec.ActiveCredits {
			t.Errorf("冻结应等于进行中任务的冻结额: %+v", rec)
		}
	})
}

func TestSettleModelCall_CapsAtAvailableCredits(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name            string
		balance, frozen int
		credits         int
		wantCharged     int
	}{
		{"余额不够：只扣能扣的", 5, 0, 9, 5},
		{"冻结中的积分不能动", 100, 98, 9, 2},
		{"全部冻结：一分也扣不了", 50, 50, 9, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := billingDB(t)
			r := NewAgentRepository(db)
			openAccount(t, db, 1, tc.balance, tc.frozen)
			call := newCall(t, r, 1)
			got, err := r.SettleModelCall(ctx, SettleCallInput{CallID: call.ID, UserID: 1, Credits: tc.credits, InputTokens: 10})
			if err != nil {
				t.Fatal(err)
			}
			if got.Credits != tc.credits || got.Charged != tc.wantCharged {
				t.Errorf("应收 %d 实扣 %d，实际 %+v", tc.credits, tc.wantCharged, got)
			}
			a := account(t, db, 1)
			if a.Balance != tc.balance-tc.wantCharged || a.Frozen != tc.frozen {
				t.Errorf("账户=%+v", a)
			}
			if a.Balance < a.Frozen {
				t.Errorf("余额不能低于冻结额: %+v", a)
			}
		})
	}
}

func TestSettleModelCall_NothingToCharge(t *testing.T) {
	ctx := context.Background()

	t.Run("零用量：只标记已结算，不写流水", func(t *testing.T) {
		db := billingDB(t)
		r := NewAgentRepository(db)
		openAccount(t, db, 1, 10, 0)
		call := newCall(t, r, 1)
		got, err := r.SettleModelCall(ctx, SettleCallInput{CallID: call.ID, UserID: 1, Error: "上游连接失败"})
		if err != nil || got.Status != model.ModelCallSettled || got.Charged != 0 || got.Error != "上游连接失败" {
			t.Fatalf("got=%+v err=%v", got, err)
		}
		if len(ledgerOf(t, db, call.ID)) != 0 || account(t, db, 1).Balance != 10 {
			t.Error("没有用量不应动账")
		}
	})

	t.Run("没有积分账户：不报错，扣 0 并记下应收", func(t *testing.T) {
		db := billingDB(t)
		r := NewAgentRepository(db)
		call := newCall(t, r, 9)
		got, err := r.SettleModelCall(ctx, SettleCallInput{CallID: call.ID, UserID: 9, Credits: 3, InputTokens: 5})
		if err != nil || got.Charged != 0 || got.Credits != 3 {
			t.Fatalf("got=%+v err=%v", got, err)
		}
	})

	t.Run("调用不属于该用户：返回不存在，不动账", func(t *testing.T) {
		db := billingDB(t)
		r := NewAgentRepository(db)
		openAccount(t, db, 1, 10, 0)
		openAccount(t, db, 2, 10, 0)
		call := newCall(t, r, 1)
		_, err := r.SettleModelCall(ctx, SettleCallInput{CallID: call.ID, UserID: 2, Credits: 3, InputTokens: 5})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("应 ErrNotFound: %v", err)
		}
		if account(t, db, 2).Balance != 10 || account(t, db, 1).Balance != 10 {
			t.Error("别人的调用不能扣任何人的钱")
		}
	})
}

func TestSettleModelCall_ConcurrentCallsDoNotLoseUpdates(t *testing.T) {
	ctx := context.Background()
	db := billingDB(t)
	r := NewAgentRepository(db)
	openAccount(t, db, 1, 100, 0)

	const n = 12
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		call := newCall(t, r, 1)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := r.SettleModelCall(ctx, SettleCallInput{CallID: call.ID, UserID: 1, Credits: 2, InputTokens: 10}); err != nil {
				t.Errorf("err=%v", err)
			}
		}()
	}
	wg.Wait()
	if a := account(t, db, 1); a.Balance != 100-n*2 {
		t.Errorf("并发扣费不能丢更新：应 %d，实际 %d", 100-n*2, a.Balance)
	}
}

func TestAvailableCredits(t *testing.T) {
	ctx := context.Background()
	db := billingDB(t)
	r := NewAgentRepository(db)
	openAccount(t, db, 1, 100, 30)
	if got, err := r.AvailableCredits(ctx, 1); err != nil || got != 70 {
		t.Errorf("可用 = 余额 - 冻结: %d %v", got, err)
	}
	if _, err := r.AvailableCredits(ctx, 99); !errors.Is(err, ErrNotFound) {
		t.Errorf("没有账户应 ErrNotFound: %v", err)
	}
}

// ---- 上下文模式要用的汇总：运行记录不在 Go 里，本轮已花的积分按调用记录算 ----

func settledCall(t *testing.T, r *AgentRepository, runID, userID uint64, purpose string, inputTokens, credits int) *model.AgentModelCall {
	t.Helper()
	c := &model.AgentModelCall{RunID: runID, UserID: userID, ModelKey: "claude", Status: model.ModelCallPending}
	if err := r.CreateModelCall(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if _, err := r.SettleModelCall(context.Background(), SettleCallInput{CallID: c.ID, UserID: userID, InputTokens: inputTokens, OutputTokens: 10, Credits: credits, Purpose: purpose}); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestRunModelUsage_SumsChargedAndTracksContextOfTheLatestMainCall(t *testing.T) {
	ctx := context.Background()
	db := billingDB(t)
	r := NewAgentRepository(db)
	openAccount(t, db, 1, 1000, 0)
	openAccount(t, db, 2, 1000, 0)

	const run uint64 = 1152921504606846977 // 超过 2^53：bigint 列里不能丢精度
	settledCall(t, r, run, 1, "main", 1000, 5)
	settledCall(t, r, run, 1, "main", 3000, 7)
	settledCall(t, r, run, 1, "summary", 90000, 2) // 摘要调用要计费，但它的输入是被压缩的那段历史，不代表对话现在多大
	settledCall(t, r, run+1, 1, "main", 500, 100)  // 相邻的另一个运行
	settledCall(t, r, run, 2, "main", 800, 50)     // 别的用户（运行 id 相同也不能混）
	pending := &model.AgentModelCall{RunID: run, UserID: 1, ModelKey: "claude", Status: model.ModelCallPending}
	if err := r.CreateModelCall(ctx, pending); err != nil { // 还没结算的不算
		t.Fatal(err)
	}

	u, err := r.RunModelUsage(ctx, 1, run)
	if err != nil {
		t.Fatal(err)
	}
	if u.Credits != 14 || u.ContextTokens != 3000 {
		t.Errorf("实扣 5+7+2；上下文是最近一次对话调用的输入 3000，不是摘要的 90000: %+v", u)
	}
	if other, _ := r.RunModelUsage(ctx, 2, run); other.Credits != 50 || other.ContextTokens != 800 {
		t.Errorf("别的用户只看到自己的: %+v", other)
	}
	if adj, _ := r.RunModelUsage(ctx, 1, run+1); adj.Credits != 100 {
		t.Errorf("相邻的两个大 ID 不能混成一个: %+v", adj)
	}
	if none, err := r.RunModelUsage(ctx, 1, 424242); err != nil || none.Credits != 0 || none.ContextTokens != 0 {
		t.Errorf("没有调用的运行全为 0，不是错误: %+v %v", none, err)
	}
}

func TestRunModelUsage_UsesChargedNotCreditsWhenBalanceIsShort(t *testing.T) {
	ctx := context.Background()
	db := billingDB(t)
	r := NewAgentRepository(db)
	openAccount(t, db, 1, 3, 0) // 只有 3 积分，应收 10 实扣 3
	settledCall(t, r, 9, 1, "main", 100, 10)
	if u, _ := r.RunModelUsage(ctx, 1, 9); u.Credits != 3 {
		t.Errorf("本轮已花按实扣算，不是应收: %+v", u)
	}
}

func TestModelCall_ScopedByUserAndRun(t *testing.T) {
	ctx := context.Background()
	db := billingDB(t)
	r := NewAgentRepository(db)
	openAccount(t, db, 1, 100, 0)
	c := settledCall(t, r, 9, 1, "main", 100, 4)

	got, err := r.ModelCall(ctx, 1, 9, c.ID)
	if err != nil || got.Charged != 4 || got.Status != model.ModelCallSettled {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	for name, args := range map[string][3]uint64{"别的用户": {2, 9, c.ID}, "别的运行": {1, 10, c.ID}, "不存在": {1, 9, 99999}} {
		if _, err := r.ModelCall(ctx, args[0], args[1], args[2]); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s应返回 ErrNotFound: %v", name, err)
		}
	}
}
