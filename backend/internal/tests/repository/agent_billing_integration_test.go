package repository_test

import (
	"context"
	"errors"
	"testing"

	"video-canvas/internal/llmgateway"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider/modelcfg"
	. "video-canvas/internal/repository"
	"video-canvas/internal/service"
)

// 编译期检查：真实仓储满足 service 声明的计费依赖接口。
var _ service.AgentBillingRepo = (*AgentRepository)(nil)

// TestAgentBilling_EndToEndOnRealDB 走一遍「预检 → 开始 → 结算 → 本轮已花更新」，
// 并确认预算用到头之后下一次预检会被拦住。
func TestAgentBilling_EndToEndOnRealDB(t *testing.T) {
	ctx := context.Background()
	db := isolatedDB(t, &model.UserCredit{}, &model.CreditLedger{}, &model.GenerationTask{}, &model.AgentModelCall{},
		&model.AgentSession{}, &model.AgentRun{})
	r := NewAgentRepository(db)
	b := service.NewAgentBilling(r)
	openAccount(t, db, 1, 100, 0)

	s := newSession(t, r, 1, 10)
	run := newRun(t, r, s, model.RunRunning) // 预算 50
	pricing := modelcfg.Pricing{Billing: modelcfg.BillingToken, Token: &modelcfg.TokenPrice{In: 3, Out: 15}}
	caps := modelcfg.Capabilities{Context: &modelcfg.ContextSpec{Window: 200000, Output: 8192}}

	if err := b.Preflight(ctx, run, pricing, caps, 4000); err != nil {
		t.Fatalf("预算和余额都够，预检应通过: %v", err)
	}
	call, err := b.Begin(ctx, run, "claude")
	if err != nil {
		t.Fatal(err)
	}
	res := &llmgateway.Result{Text: "好", Usage: llmgateway.Usage{InputTokens: 2_000_000, OutputTokens: 1_000_000}}
	out, err := b.Settle(ctx, run, call, pricing, service.CallOutcome{Result: res})
	if err != nil {
		t.Fatal(err)
	}
	if out.Credits != 21 || out.Charged != 21 { // 2×3 + 1×15
		t.Fatalf("out=%+v", out)
	}
	if a := account(t, db, 1); a.Balance != 79 {
		t.Errorf("余额应 100→79: %d", a.Balance)
	}
	got, _ := r.GetRun(ctx, 1, run.ID)
	if got.SpentCredits != 21 || got.Steps != 0 {
		t.Errorf("本轮已花应是 21、步数不变: %+v", got)
	}

	// 预算 50、已花 21 → 再来一次 40 积分的调用，最坏情况超出剩余预算，预检应拦住。
	pricey := modelcfg.Pricing{Billing: modelcfg.BillingToken, Token: &modelcfg.TokenPrice{In: 1000, Out: 5000}}
	err = b.Preflight(ctx, got, pricey, caps, 40000)
	var ec *errcode.Error
	if !errors.As(err, &ec) || ec.Code != errcode.ErrAgentOverBudget.Code {
		t.Errorf("应返回 60008: %v", err)
	}
}
