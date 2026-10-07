package agent

import (
	"context"
	"errors"
	"unicode/utf8"

	"video-canvas/internal/llmgateway"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/repository"
)

// maxCallErrorRunes 是调用记录里错误原因的最大字数，与列宽 255 对应。
const maxCallErrorRunes = 250

// AgentBillingRepo 是 Agent 对话计费所需的数据访问。
type AgentBillingRepo interface {
	// AvailableCredits 返回用户的可用积分（余额减冻结），没有账户返回 repository.ErrNotFound。
	AvailableCredits(ctx context.Context, userID uint64) (int, error)
	// CreateModelCall 创建一条待结算的调用记录。
	CreateModelCall(ctx context.Context, c *model.AgentModelCall) error
	// SettleModelCall 按实际用量结算一次调用：幂等，实扣额封顶在可用积分以内。
	SettleModelCall(ctx context.Context, in repository.SettleCallInput) (*model.AgentModelCall, error)
	// AddRunUsage 累加运行的步数和已花积分。
	AddRunUsage(ctx context.Context, id uint64, steps, credits int) error
}

// AgentBilling 是 Agent 每次大模型调用的计费：调用前预检预算和余额，调用后按实际用量扣费。
//
// 不预先冻结，原因见 repository.SettleModelCall：调用结束后一次性扣，并发时可能略微透支，
// 所以实扣额封顶在可用积分以内，差额记在调用上；预检把绝大多数「钱不够」的情况挡在调用之前。
type AgentBilling struct {
	repo AgentBillingRepo
}

// NewAgentBilling 创建计费服务。
func NewAgentBilling(repo AgentBillingRepo) *AgentBilling { return &AgentBilling{repo: repo} }

// CallOutcome 是一次调用的产出，结算按它算用量。
type CallOutcome struct {
	Result     *llmgateway.Result // 网关返回的结果；一个字都没收到时为 nil
	InputChars int                // 这次请求的输入总字数，上游没给用量时按它估输入 Token
	Err        error              // 调用失败、被取消或中断的原因；成功为 nil
}

// SettleResult 是结算结果。
type SettleResult struct {
	Credits   int  // 按用量算出的应收积分
	Charged   int  // 实际扣的积分
	Estimated bool // Token 数是估的
	Shortfall bool // 用户余额不够，实扣小于应收
}

// Preflight 在发起调用前预检：本轮预算和用户可用积分，都要够付这次调用「最坏情况」的费用
// （输入按字数保守估，输出按模型的最大输出打满）。预检不加锁，只是尽早拦住明显不够的情况。
func (b *AgentBilling) Preflight(ctx context.Context, run *model.AgentRun, p modelcfg.Pricing, caps modelcfg.Capabilities, inputChars int) error {
	// 1. Agent 模型只能按 Token 计费，别的计费方式说明配置有问题，按模型不可用处理
	if p.Billing != modelcfg.BillingToken || p.Token == nil {
		return errcode.ErrAgentModelNA
	}
	upper := modelcfg.Quote(p, caps, modelcfg.Spec{PromptChars: inputChars})
	// 2. 本轮预算：已花加上最坏情况不能超过预算
	if upper > run.BudgetCredits-run.SpentCredits {
		return errcode.ErrAgentOverBudget
	}
	// 3. 用户可用积分：没有账户和不够一样处理
	avail, err := b.repo.AvailableCredits(ctx, run.UserID)
	if errors.Is(err, repository.ErrNotFound) || (err == nil && avail < upper) {
		return errcode.ErrInsufficientCredits
	}
	return err
}

// Begin 为即将发起的调用创建一条待结算记录，返回的记录用于后面的 Settle。
func (b *AgentBilling) Begin(ctx context.Context, run *model.AgentRun, modelKey string) (*model.AgentModelCall, error) {
	c := &model.AgentModelCall{RunID: run.ID, UserID: run.UserID, ModelKey: modelKey, Status: model.ModelCallPending}
	if err := b.repo.CreateModelCall(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

// Settle 在调用结束后（成功、失败、取消、中断都一样）按实际用量结算。
// 上游给了用量就用它；没给就按字数估（1 字 = 1 Token，偏保守），并在记录上标出是估的。
// 一个字都没收到时不收费，只记下原因。实扣额记入本轮已花。
func (b *AgentBilling) Settle(ctx context.Context, run *model.AgentRun, call *model.AgentModelCall, p modelcfg.Pricing, out CallOutcome) (*SettleResult, error) {
	in := repository.SettleCallInput{CallID: call.ID, UserID: call.UserID, Error: callError(out.Err)}
	if out.Result != nil {
		r := out.Result
		if r.UsageMissing {
			in.UsageEstimated = true
			in.InputTokens, in.OutputTokens = out.InputChars, outputChars(r)
		} else {
			in.InputTokens, in.OutputTokens, in.CachedTokens = r.Usage.InputTokens, r.Usage.OutputTokens, r.Usage.CachedTokens
		}
		in.Credits = modelcfg.TokenCost(p, in.InputTokens, in.OutputTokens)
	}
	settled, err := b.repo.SettleModelCall(ctx, in)
	if err != nil {
		return nil, err
	}
	if settled.Charged > 0 {
		if err := b.repo.AddRunUsage(ctx, run.ID, 0, settled.Charged); err != nil {
			return nil, err
		}
	}
	return &SettleResult{Credits: settled.Credits, Charged: settled.Charged, Estimated: in.UsageEstimated, Shortfall: settled.Charged < settled.Credits}, nil
}

// outputChars 估输出：正文、思考内容和工具调用参数的字数之和。
func outputChars(r *llmgateway.Result) int {
	n := utf8.RuneCountInString(r.Text) + utf8.RuneCountInString(r.Thinking)
	for _, c := range r.ToolCalls {
		n += utf8.RuneCountInString(c.Arguments)
	}
	return n
}

// callError 把调用错误转成记在记录上的短说明：取消、空闲超时用固定文案，上游错误用它自带的（已脱敏），其余截断。
func callError(err error) string {
	var ue *llmgateway.UpstreamError
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.Canceled):
		return "已取消"
	case errors.Is(err, llmgateway.ErrIdleTimeout):
		return "上游长时间没有响应"
	case errors.Is(err, llmgateway.ErrStreamTruncated):
		return "上游响应中断"
	case errors.As(err, &ue):
		return truncateRunes(ue.Message, maxCallErrorRunes)
	}
	return truncateRunes(err.Error(), maxCallErrorRunes)
}

// truncateRunes 按字数截断。
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
