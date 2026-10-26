package agent_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"video-canvas/internal/llmgateway"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
	. "video-canvas/internal/service/agent"
)

// fakeRunUsage 从假计费仓储里算本轮的模型用量：和真实仓储一样，只数该用户、该运行的、已结算的调用。
type fakeRunUsage struct{ bill *fakeBillingRepo }

func (f *fakeRunUsage) charged(in repository.SettleCallInput) int {
	if f.bill.chargeCap > 0 && in.Credits > f.bill.chargeCap {
		return f.bill.chargeCap
	}
	return in.Credits
}

func (f *fakeRunUsage) callByID(id uint64) *model.AgentModelCall {
	for _, c := range f.bill.calls {
		if c.ID == id {
			return c
		}
	}
	return nil
}

func (f *fakeRunUsage) RunModelUsage(_ context.Context, userID, runID uint64) (model.AgentRunModelUsage, error) {
	var u model.AgentRunModelUsage
	for _, in := range f.bill.settled {
		c := f.callByID(in.CallID)
		if c == nil || c.RunID != runID || c.UserID != userID {
			continue
		}
		u.Credits += f.charged(in)
		if in.Purpose == "main" || in.Purpose == "" {
			u.ContextTokens = in.InputTokens
		}
	}
	return u, nil
}

func (f *fakeRunUsage) ModelCall(_ context.Context, userID, runID, callID uint64) (*model.AgentModelCall, error) {
	c := f.callByID(callID)
	if c == nil || c.RunID != runID || c.UserID != userID {
		return nil, repository.ErrNotFound
	}
	out := *c
	out.Status = model.ModelCallPending
	for _, in := range f.bill.settled {
		if in.CallID == callID {
			out.Status, out.Credits, out.Charged, out.InputTokens, out.OutputTokens = model.ModelCallSettled, in.Credits, f.charged(in), in.InputTokens, in.OutputTokens
			out.UsageEstimated, out.Error = in.UsageEstimated, in.Error
		}
	}
	return &out, nil
}

var proxyBody = []byte(`{"model":"x","messages":[{"role":"user","content":"拆分镜"}]}`)

// proxyPlatform 走一次上下文模式的模型代理，返回转出的行和 Go 分配的调用 id。
func proxyPlatform(ctx context.Context, b *bridgeEnv, rc RunContext) (lines []string, callID uint64, err error) {
	err = b.br.ProxyModelPlatform(ctx, rc, proxyBody, func(id uint64) { callID = id }, func(l []byte) { lines = append(lines, string(l)) })
	return lines, callID, err
}

func TestProxyModelPlatform_StreamsAndSettlesWithoutTouchingRunState(t *testing.T) {
	b := newBridgeEnv(t)
	lines, callID, err := proxyPlatform(context.Background(), b, platformCtx(900101, "all"))
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || !strings.Contains(lines[0], "好的") || lines[1] != "data: [DONE]\n\n" {
		t.Errorf("流应原样转出: %v", lines)
	}
	if callID == 0 {
		t.Error("开始流之前要把调用 id 告诉调用方（响应头 X-Agent-Call-Id），它靠这个 id 查这次调用的积分")
	}
	s := b.str
	if s.tg.BaseURL != "https://llm.example" || s.tg.APIKey != "sk-test" || s.tg.UpstreamModel != "claude-up" {
		t.Errorf("目标应来自渠道快照和凭证: %+v", s.tg)
	}
	if len(b.bill.settled) != 1 || b.bill.settled[0].Credits != 5 {
		t.Errorf("应按用量结算: %+v", b.bill.settled)
	}
	if b.bill.calls[0].RunID != 900101 || b.bill.calls[0].UserID != 1 || b.bill.calls[0].ModelKey != "claude" {
		t.Errorf("调用记录要记在这个运行名下: %+v", b.bill.calls[0])
	}
	if b.bill.usageRunID != 0 {
		t.Error("没有运行记录可更新：不能调 AddRunUsage（运行的已花积分由 Go 按调用记录累加）")
	}
}

func TestProxyModelPlatform_BudgetUsesSettledCallsPlusExtra(t *testing.T) {
	b := newBridgeEnv(t)
	rc := platformCtx(900102, "all")
	rc.BudgetCredits = 5
	if _, _, err := proxyPlatform(context.Background(), b, rc); err != nil { // 第一次的最坏情况预估约 1 积分，预算 5 够；实际花 5
		t.Fatal(err)
	}
	// 已花 5（由 Go 按调用记录累加，调用方没有告诉它），预算剩 0，再来一次的最坏情况必然超出
	_, _, err := proxyPlatform(context.Background(), b, rc)
	if err == nil {
		t.Fatal("第二次应因本轮预算不够被拒绝")
	}
	wantAgentCode(t, err, errcode.ErrAgentOverBudget)
	if len(b.bill.calls) != 1 || b.str.calls != 1 {
		t.Errorf("被拒绝的调用不能创建调用记录、也不能发往上游: calls=%d upstream=%d", len(b.bill.calls), b.str.calls)
	}
	// 除模型调用外已经花掉的积分（已批准的生成）也算进本轮
	other := platformCtx(900103, "all")
	other.BudgetCredits, other.SpentExtra = 8, 8
	_, _, err = proxyPlatform(context.Background(), b, other)
	wantAgentCode(t, err, errcode.ErrAgentOverBudget)
	// 预算为 0 表示不限制
	free := platformCtx(900104, "all")
	free.SpentExtra = 1000
	if _, _, err := proxyPlatform(context.Background(), b, free); err != nil {
		t.Errorf("预算 0 不限制本轮: %v", err)
	}
}

func TestProxyModelPlatform_AccountAndModelErrors(t *testing.T) {
	t.Run("余额不够", func(t *testing.T) {
		b := newBridgeEnv(t)
		b.bill.available = 0
		_, _, err := proxyPlatform(context.Background(), b, platformCtx(900105, "all"))
		wantAgentCode(t, err, errcode.ErrInsufficientCredits)
		if len(b.bill.calls) != 0 {
			t.Error("不能创建调用记录")
		}
	})
	t.Run("没有给模型", func(t *testing.T) {
		b := newBridgeEnv(t)
		rc := platformCtx(900106, "all")
		rc.ModelKey = ""
		_, _, err := proxyPlatform(context.Background(), b, rc)
		wantAgentCode(t, err, errcode.ErrInvalidParams)
	})
	t.Run("模型不可用或已下线", func(t *testing.T) {
		b := newBridgeEnv(t)
		b.reg.snap, b.reg.snapEr = nil, provider.ErrModelUnavailable
		_, _, err := proxyPlatform(context.Background(), b, platformCtx(900107, "all"))
		wantAgentCode(t, err, errcode.ErrAgentModelNA)
	})
	t.Run("上下文不合法", func(t *testing.T) {
		b := newBridgeEnv(t)
		_, _, err := proxyPlatform(context.Background(), b, RunContext{})
		wantAgentCode(t, err, errcode.ErrInvalidParams)
	})
}

func TestProxyModelPlatform_UpstreamFailureStillSettlesWhatWasProduced(t *testing.T) {
	b := newBridgeEnv(t)
	b.str.script = func(_ context.Context, emit func(llmgateway.Event), raw func([]byte)) (*llmgateway.Result, error) {
		emit(llmgateway.Event{Type: llmgateway.EventText, Text: "半截"})
		raw([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"半截\"}}]}\n\n"))
		return &llmgateway.Result{Text: "半截", Usage: llmgateway.Usage{InputTokens: 1_000_000, OutputTokens: 10}}, errors.New("上游断了")
	}
	_, _, err := proxyPlatform(context.Background(), b, platformCtx(900108, "all"))
	if err == nil {
		t.Fatal("上游出错要原样返回给调用方")
	}
	if len(b.bill.settled) != 1 || b.bill.settled[0].Error == "" {
		t.Errorf("已经产生的部分照样结算，并记下原因: %+v", b.bill.settled)
	}
}

func TestProxyModelPlatform_CancelledCallerIsStillCharged(t *testing.T) {
	b := newBridgeEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	b.str.script = func(c context.Context, _ func(llmgateway.Event), raw func([]byte)) (*llmgateway.Result, error) {
		raw([]byte("data: {}\n\n"))
		cancel() // 用户停止：调用方断开
		<-c.Done()
		return &llmgateway.Result{Text: "半", Usage: llmgateway.Usage{InputTokens: 1_000_000, OutputTokens: 10}}, c.Err()
	}
	_, _, _ = proxyPlatform(ctx, b, platformCtx(900109, "all"))
	if len(b.bill.settled) != 1 || b.bill.settled[0].Credits == 0 {
		t.Errorf("调用方断开后，已经产生的用量仍要结算（结算用不会被取消的上下文）: %+v", b.bill.settled)
	}
}

func TestProxyModelPlatform_TitlePurposeIsRecordedAndDoesNotCountAsContext(t *testing.T) {
	b := newBridgeEnv(t)
	rc := platformCtx(900111, "all")
	if _, _, err := proxyPlatform(WithModelPurpose(context.Background(), ModelPurposeTitle), b, rc); err != nil {
		t.Fatal(err)
	}
	if b.bill.settled[0].Purpose != "title" {
		t.Errorf("会话命名调用要记成 title: %q", b.bill.settled[0].Purpose)
	}
	u, err := b.br.PlatformRunUsage(context.Background(), rc, 0)
	if err != nil {
		t.Fatal(err)
	}
	if u.SpentCredits == 0 || u.ContextTokens != 0 {
		t.Errorf("命名调用照常计费，但不算对话的上下文大小: %+v", u)
	}
}

func TestProxyModelPlatform_SummaryPurposeIsRecordedAndDoesNotCountAsContext(t *testing.T) {
	b := newBridgeEnv(t)
	rc := platformCtx(900110, "all")
	if _, _, err := proxyPlatform(WithModelPurpose(context.Background(), ModelPurposeSummary), b, rc); err != nil {
		t.Fatal(err)
	}
	if b.bill.settled[0].Purpose != "summary" {
		t.Errorf("摘要调用要记成 summary: %q", b.bill.settled[0].Purpose)
	}
	u, err := b.br.PlatformRunUsage(context.Background(), rc, 0)
	if err != nil {
		t.Fatal(err)
	}
	if u.SpentCredits == 0 || u.ContextTokens != 0 {
		t.Errorf("摘要调用要计费，但不代表对话现在多大: %+v", u)
	}
}

func TestPlatformRunUsage_SumsCallsAndFindsOneCall(t *testing.T) {
	b := newBridgeEnv(t)
	rc := platformCtx(900111, "all")
	_, first, err := proxyPlatform(context.Background(), b, rc)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := proxyPlatform(context.Background(), b, rc); err != nil {
		t.Fatal(err)
	}
	u, err := b.br.PlatformRunUsage(context.Background(), rc, first)
	if err != nil {
		t.Fatal(err)
	}
	if u.SpentCredits != 10 || u.ContextTokens != 1_000_000 {
		t.Errorf("两次调用各 5 积分；上下文大小是最近一次对话调用的输入 token: %+v", u)
	}
	if u.Call == nil || u.Call.ID != first || u.Call.Status != model.ModelCallSettled || u.Call.Charged != 5 || u.Call.Credits != 5 {
		t.Errorf("带了调用 id 要给出这次调用的积分（message.done 要用）: %+v", u.Call)
	}
	// 不带调用 id 就没有 call
	if u2, _ := b.br.PlatformRunUsage(context.Background(), rc, 0); u2.Call != nil {
		t.Errorf("没带调用 id 不该有 call: %+v", u2.Call)
	}
	// 别的运行、别的用户查不到这次调用
	for name, other := range map[string]RunContext{"别的运行": platformCtx(900112, "all"), "别的用户": func() RunContext { r := rc; r.UserID = 2; return r }()} {
		if _, err := b.br.PlatformRunUsage(context.Background(), other, first); err == nil {
			t.Errorf("%s不能查到这次调用", name)
		}
	}
}

func TestPlatformRunUsage_NewRunHasZeroUsage(t *testing.T) {
	b := newBridgeEnv(t)
	u, err := b.br.PlatformRunUsage(context.Background(), platformCtx(900113, "all"), 0)
	if err != nil || u.SpentCredits != 0 || u.ContextTokens != 0 || u.Call != nil {
		t.Errorf("没有任何调用的运行用量为零，不是错误: %+v %v", u, err)
	}
}
