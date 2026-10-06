package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"video-canvas/internal/llmgateway"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/repository"
	. "video-canvas/internal/service"
)

// fakeBillingRepo 是内存版 AgentBillingRepo：记下结算入参，按预设返回实扣额。
type fakeBillingRepo struct {
	available   int
	noAccount   bool
	calls       []*model.AgentModelCall
	settled     []repository.SettleCallInput
	chargeCap   int // >0 时实扣额封顶，模拟余额不够
	usageRunID  uint64
	usageSteps  int
	usageCredit int
}

func (f *fakeBillingRepo) AvailableCredits(context.Context, uint64) (int, error) {
	if f.noAccount {
		return 0, repository.ErrNotFound
	}
	return f.available, nil
}

func (f *fakeBillingRepo) CreateModelCall(_ context.Context, c *model.AgentModelCall) error {
	c.ID = uint64(len(f.calls) + 1)
	f.calls = append(f.calls, c)
	return nil
}

func (f *fakeBillingRepo) SettleModelCall(_ context.Context, in repository.SettleCallInput) (*model.AgentModelCall, error) {
	f.settled = append(f.settled, in)
	charged := in.Credits
	if f.chargeCap > 0 && charged > f.chargeCap {
		charged = f.chargeCap
	}
	return &model.AgentModelCall{ID: in.CallID, Status: model.ModelCallSettled, Credits: in.Credits, Charged: charged}, nil
}

func (f *fakeBillingRepo) AddRunUsage(_ context.Context, runID uint64, steps, credits int) error {
	f.usageRunID, f.usageSteps, f.usageCredit = runID, steps, credits
	return nil
}

var billingPricing = modelcfg.Pricing{Billing: modelcfg.BillingToken, Token: &modelcfg.TokenPrice{In: 3, Out: 15}}
var billingCaps = modelcfg.Capabilities{Context: &modelcfg.ContextSpec{Window: 200000, Output: 8192}}

func billingRun(spent int) *model.AgentRun {
	return &model.AgentRun{ID: 5, UserID: 1, BudgetCredits: 50, SpentCredits: spent}
}

func TestAgentBilling_Preflight(t *testing.T) {
	ctx := context.Background()

	t.Run("预算和余额都够：通过", func(t *testing.T) {
		b := NewAgentBilling(&fakeBillingRepo{available: 100})
		if err := b.Preflight(ctx, billingRun(0), billingPricing, billingCaps, 4000); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("本轮预算剩得比这次最坏情况还少：60008", func(t *testing.T) {
		b := NewAgentBilling(&fakeBillingRepo{available: 100})
		// 单价调高，最坏情况（输出打满 8192 Token）要好几积分；预算只剩 1。
		pricey := modelcfg.Pricing{Billing: modelcfg.BillingToken, Token: &modelcfg.TokenPrice{In: 3000, Out: 15000}}
		err := b.Preflight(ctx, billingRun(49), pricey, billingCaps, 4000)
		wantAgentCode(t, err, errcode.ErrAgentOverBudget)
	})

	t.Run("可用积分不够：40001", func(t *testing.T) {
		b := NewAgentBilling(&fakeBillingRepo{available: 0})
		wantAgentCode(t, b.Preflight(ctx, billingRun(0), billingPricing, billingCaps, 4000), errcode.ErrInsufficientCredits)
	})

	t.Run("没有积分账户：40001", func(t *testing.T) {
		b := NewAgentBilling(&fakeBillingRepo{noAccount: true})
		wantAgentCode(t, b.Preflight(ctx, billingRun(0), billingPricing, billingCaps, 4000), errcode.ErrInsufficientCredits)
	})

	t.Run("模型不是按 Token 计费：Agent 模型不可用", func(t *testing.T) {
		b := NewAgentBilling(&fakeBillingRepo{available: 100})
		perCall := modelcfg.Pricing{Billing: modelcfg.BillingPerCall, Unit: 5}
		wantAgentCode(t, b.Preflight(ctx, billingRun(0), perCall, billingCaps, 10), errcode.ErrAgentModelNA)
	})
}

func TestAgentBilling_Settle(t *testing.T) {
	ctx := context.Background()
	newBilling := func() (*AgentBilling, *fakeBillingRepo, *model.AgentModelCall) {
		repo := &fakeBillingRepo{available: 100}
		b := NewAgentBilling(repo)
		call, err := b.Begin(ctx, billingRun(0), "claude")
		if err != nil {
			t.Fatal(err)
		}
		return b, repo, call
	}

	t.Run("Begin 创建待结算记录，带运行、用户、模型", func(t *testing.T) {
		_, repo, call := newBilling()
		if call.Status != model.ModelCallPending || call.RunID != 5 || call.UserID != 1 || call.ModelKey != "claude" || len(repo.calls) != 1 {
			t.Errorf("call=%+v", call)
		}
	})

	t.Run("上游给了用量：按它算，记入本轮已花", func(t *testing.T) {
		b, repo, call := newBilling()
		res := &llmgateway.Result{Text: "好", Usage: llmgateway.Usage{InputTokens: 1_000_000, OutputTokens: 100_000, CachedTokens: 500_000}}
		out, err := b.Settle(ctx, billingRun(0), call, billingPricing, CallOutcome{Result: res, InputChars: 10})
		if err != nil {
			t.Fatal(err)
		}
		in := repo.settled[0]
		if in.Credits != 5 || in.InputTokens != 1_000_000 || in.CachedTokens != 500_000 || in.UsageEstimated { // 3 + 1.5 = 4.5 → 5
			t.Errorf("settle 入参=%+v", in)
		}
		if out.Credits != 5 || out.Charged != 5 || out.Estimated || out.Shortfall {
			t.Errorf("out=%+v", out)
		}
		if repo.usageRunID != 5 || repo.usageCredit != 5 || repo.usageSteps != 0 {
			t.Errorf("应把实扣额记入本轮已花（不算工具步数）: %+v", repo)
		}
	})

	t.Run("上游没给用量：按字数估，并标出是估的", func(t *testing.T) {
		b, repo, call := newBilling()
		res := &llmgateway.Result{Text: strings.Repeat("字", 1000), ToolCalls: []llmgateway.ToolCall{{Arguments: strings.Repeat("a", 500)}}, UsageMissing: true}
		out, err := b.Settle(ctx, billingRun(0), call, billingPricing, CallOutcome{Result: res, InputChars: 4000})
		if err != nil {
			t.Fatal(err)
		}
		in := repo.settled[0]
		if !in.UsageEstimated || in.InputTokens != 4000 || in.OutputTokens != 1500 {
			t.Errorf("估算按 1 字 = 1 Token（输入取整段提示字数，输出取正文加工具参数）: %+v", in)
		}
		if !out.Estimated || in.Credits != 1 {
			t.Errorf("out=%+v credits=%d", out, in.Credits)
		}
	})

	t.Run("被取消：已产生的部分照样结算，并记下原因", func(t *testing.T) {
		b, repo, call := newBilling()
		res := &llmgateway.Result{Text: "说到一半", UsageMissing: true}
		if _, err := b.Settle(ctx, billingRun(0), call, billingPricing, CallOutcome{Result: res, InputChars: 100, Err: context.Canceled}); err != nil {
			t.Fatal(err)
		}
		if repo.settled[0].Credits == 0 || repo.settled[0].Error == "" || !strings.Contains(repo.settled[0].Error, "取消") {
			t.Errorf("入参=%+v", repo.settled[0])
		}
	})

	t.Run("一个字都没收到（连不上上游）：不收费，只记原因", func(t *testing.T) {
		b, repo, call := newBilling()
		upErr := &llmgateway.UpstreamError{Status: 502, Message: "上游服务暂时不可用（HTTP 502）"}
		out, err := b.Settle(ctx, billingRun(0), call, billingPricing, CallOutcome{Result: nil, InputChars: 100, Err: upErr})
		if err != nil {
			t.Fatal(err)
		}
		if repo.settled[0].Credits != 0 || out.Credits != 0 || repo.usageCredit != 0 {
			t.Errorf("没有任何产出不该收费: %+v %+v", repo.settled[0], out)
		}
		if !strings.Contains(repo.settled[0].Error, "上游") {
			t.Errorf("应记下上游的说明: %q", repo.settled[0].Error)
		}
	})

	t.Run("余额不够实扣不足：标出差额", func(t *testing.T) {
		b, repo, call := newBilling()
		repo.chargeCap = 2
		res := &llmgateway.Result{Usage: llmgateway.Usage{InputTokens: 2_000_000, OutputTokens: 1_000_000}}
		out, err := b.Settle(ctx, billingRun(0), call, billingPricing, CallOutcome{Result: res})
		if err != nil {
			t.Fatal(err)
		}
		if out.Credits != 21 || out.Charged != 2 || !out.Shortfall {
			t.Errorf("out=%+v", out)
		}
		if repo.usageCredit != 2 {
			t.Errorf("本轮已花应按实扣额记: %d", repo.usageCredit)
		}
	})

	t.Run("错误原因脱敏并截断", func(t *testing.T) {
		b, repo, call := newBilling()
		long := errors.New(strings.Repeat("长", 1000))
		if _, err := b.Settle(ctx, billingRun(0), call, billingPricing, CallOutcome{Err: long}); err != nil {
			t.Fatal(err)
		}
		if n := len([]rune(repo.settled[0].Error)); n > 255 {
			t.Errorf("错误原因要能存进 255 字的列: %d", n)
		}
	})
}
