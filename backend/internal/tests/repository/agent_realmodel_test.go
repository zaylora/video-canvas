package repository_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	. "video-canvas/internal/repository"
	agentsvc "video-canvas/internal/service/agent"
)

// 真实模型验证（默认跳过，不进 CI）：全栈环境（真实 Agent 子进程 + Go 平台 API + PostgreSQL）里把上游换成真实的 OpenAI 兼容渠道，
// 看 codemode、审批、提问、压缩、取消、思考内容和计费在真实模型上的表现。密钥只通过环境变量传入，不写进任何文件：
//
//	REAL_LLM_BASE_URL=https://api.vbing.site REAL_LLM_KEY=sk-... REAL_LLM_MODEL=deepseek-v4-pro \
//	  go test -count=1 -run TestRealModel -v ./internal/tests/repository/
//
// REAL_LLM_WINDOW 是登记的上下文窗口（默认 20000，比真实模型小很多，让压缩在几轮之内触发；太小会被 Pi 把 max_tokens 压到 1）。
// 断言只放在和模型无关的不变量上（运行能走到终态、计费和对账、事件完整）；模型这次有没有按期望的方式用工具，只记成「观察」。

type realRegistry struct {
	base, upstream string
	window, output int
}

func (r realRegistry) Snapshot(context.Context, string) (*provider.Snapshot, error) {
	return &provider.Snapshot{
		Model: provider.ModelSnapshot{Key: "claude", Kind: "agent", Label: "真实模型", UpstreamModel: r.upstream,
			Pricing:      modelcfg.Pricing{Billing: modelcfg.BillingToken, Token: &modelcfg.TokenPrice{In: 3, Out: 15}},
			Capabilities: modelcfg.Capabilities{Context: &modelcfg.ContextSpec{Window: r.window, Output: r.output}}},
		Channel: provider.ChannelSnapshot{Key: "real", BaseURL: r.base},
	}, nil
}

func (r realRegistry) ListModels(context.Context, string) ([]provider.ModelInfo, error) {
	return []provider.ModelInfo{{Key: "claude", Kind: "agent", Label: "真实模型"}}, nil
}

type realSecret string

func (s realSecret) Get(context.Context, string) (string, error) { return string(s), nil }

func newRealEnv(t *testing.T) *durableEnv { return newRealEnvWith(t, 4096) }

func newRealEnvWith(t *testing.T, output int) *durableEnv {
	t.Helper()
	base, key, mdl := os.Getenv("REAL_LLM_BASE_URL"), os.Getenv("REAL_LLM_KEY"), os.Getenv("REAL_LLM_MODEL")
	if base == "" || key == "" || mdl == "" {
		t.Skip("没有设置 REAL_LLM_BASE_URL / REAL_LLM_KEY / REAL_LLM_MODEL，跳过真实模型验证")
	}
	window := 20000
	if v, err := strconv.Atoi(os.Getenv("REAL_LLM_WINDOW")); err == nil && v > 0 {
		window = v
	}
	return newDurableEnv(t, nil, &realUpstream{
		registry: realRegistry{base: strings.TrimRight(base, "/"), upstream: mdl, window: window, output: output}, secrets: realSecret(key),
		ipAllowed: func(net.IP) bool { return true }, // 本机开着代理工具时域名会被解析成 198.18.x.x，测试里不限制出站
	})
}

// allEvents 取会话全部落库事件（分页）。
func (e *durableEnv) allEvents(t *testing.T, sid string) []map[string]any {
	t.Helper()
	var out []map[string]any
	after := 0
	for {
		list := e.data(t, "GET", fmt.Sprintf("/agent/sessions/%s/events?after=%d&limit=200", sid, after), "", 200).([]any)
		for _, v := range list {
			ev := v.(map[string]any)
			out = append(out, ev)
			after = int(ev["seq"].(float64))
		}
		if len(list) < 200 {
			return out
		}
	}
}

func runEvents(evs []map[string]any, runID string) []map[string]any {
	var out []map[string]any
	for _, ev := range evs {
		if ev["run_id"] == runID {
			out = append(out, ev)
		}
	}
	return out
}

func dataOf(ev map[string]any) map[string]any { d, _ := ev["data"].(map[string]any); return d }

// waitSettled 等运行走到不再自己前进的状态（终态、等审批、等回答），返回最后一条 run.status 的状态；超时算失败。
func (e *durableEnv) waitSettled(t *testing.T, sid, runID string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status := ""
		for _, ev := range runEvents(e.allEvents(t, sid), runID) {
			if ev["type"] == "run.status" {
				status, _ = dataOf(ev)["status"].(string)
			}
		}
		switch status {
		case "succeeded", "failed", "interrupted", "canceled", "timeout", "step_limit", "budget_exhausted", "waiting_approval", "waiting_input":
			return status
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("运行 %s 在 %s 内没有走到稳定状态", runID, timeout)
	return ""
}

func clip(s string, n int) string {
	r := []rune(strings.ReplaceAll(s, "\n", "⏎"))
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return string(r)
}

// report 把一次运行的经过记进测试日志，供人读：状态、工具序列（含 codemode 里的嵌套调用）、模型的回复和思考。
func (e *durableEnv) report(t *testing.T, label string, evs []map[string]any, runID string) {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "—— %s（run %s）\n", label, clip(runID, 8))
	for _, ev := range runEvents(evs, runID) {
		d := dataOf(ev)
		switch ev["type"] {
		case "run.status":
			fmt.Fprintf(&b, "  [状态] %v %v\n", d["status"], clip(fmt.Sprint(d["error"]), 120))
		case "tool.start":
			p := ""
			if d["parent_id"] != nil {
				p = "  ↳ 嵌套于 " + fmt.Sprint(d["parent_id"])
			}
			fmt.Fprintf(&b, "  [工具开始] %v%s\n", d["name"], p)
		case "tool.end":
			fmt.Fprintf(&b, "  [工具结束] %v err=%v %s\n", d["name"], d["is_error"], clip(fmt.Sprint(d["summary"]), 100))
		case "message.done":
			th := ""
			if s, _ := d["thinking"].(string); s != "" {
				th = fmt.Sprintf("（带思考 %d 字）", len([]rune(s)))
			}
			fmt.Fprintf(&b, "  [回复]%s %s partial=%v credits=%v\n", th, clip(fmt.Sprint(d["text"]), 240), d["partial"], d["credits"])
		case "approval.created", "approval.decided":
			fmt.Fprintf(&b, "  [%v] kind=%v status=%v\n", ev["type"], d["kind"], d["status"])
		case "run.usage":
			fmt.Fprintf(&b, "  [用量] 已花 %v 积分，上下文 %v token\n", d["spent_credits"], d["context_tokens"])
		}
	}
	t.Log("\n" + b.String())
}

// checkModelCalls 记下所有模型调用，并检查不变量：每次调用都结算了、没有上游错误；对账等式成立。
func (e *durableEnv) checkModelCalls(t *testing.T) []model.AgentModelCall {
	t.Helper()
	var calls []model.AgentModelCall
	e.db.Order("id").Find(&calls)
	var lines []string
	for _, c := range calls {
		lines = append(lines, fmt.Sprintf("    #%d %-7s %s 输入 %d 输出 %d 缓存 %d 实扣 %d 估算=%v %dms 首字 %dms 停止=%s 错误=%q",
			c.ID%1000, c.Purpose, c.Status, c.InputTokens, c.OutputTokens, c.CachedTokens, c.Charged, c.UsageEstimated, c.LatencyMs, c.TTFTMs, c.StopReason, clip(c.Error, 80)))
		if c.Status != model.ModelCallSettled {
			t.Errorf("模型调用 #%d 状态是 %s，应是已结算", c.ID, c.Status)
		}
	}
	t.Logf("模型调用 %d 次：\n%s", len(calls), strings.Join(lines, "\n"))
	var bodies []model.AgentModelCallBody
	e.db.Omit("body").Order("call_id").Find(&bodies)
	for _, b := range bodies {
		t.Logf("    调用 #%d 的请求参数（不含 messages 和 tools）：%s；messages %d 条，tools %d 个，%d 字节", b.CallID%1000, clip(string(b.Params), 300), b.MessageCount, b.ToolCount, b.RawBytes)
	}
	rec, err := NewGenerationTaskRepository(e.db).Reconcile(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if int64(rec.Balance) != rec.InitialSum+rec.AdminAdjustSum-rec.SettleSum || int64(rec.Frozen) != rec.FreezeSum-rec.SettleSum-rec.RefundSum {
		t.Errorf("对账等式不成立：%+v", rec)
	}
	return calls
}

func (e *durableEnv) say(t *testing.T, sid, msg string, timeout time.Duration) (runID, status string) {
	t.Helper()
	runID = e.startRun(t, sid, msg)
	return runID, e.waitSettled(t, sid, runID, timeout)
}

const realRunTimeout = 4 * time.Minute

func TestRealModel_BuildCanvas(t *testing.T) {
	e := newRealEnv(t)
	sid := e.newSession(t)
	runID, status := e.say(t, sid, "在剧本节点后面建一个叫“镜头 01”的组，组里放两个图片节点：关键帧 A（提示词：雨夜便利店门口，霓虹灯反光）、关键帧 B（提示词：便利店收银台，店员在打瞌睡），再把剧本节点连到这两个图片节点。", realRunTimeout)
	evs := e.allEvents(t, sid)
	e.report(t, "建组和节点", evs, runID)
	if status != "succeeded" {
		t.Errorf("运行应成功：%s", status)
	}
	g := e.graph(t)
	t.Logf("[观察] 画布现在有 %d 个节点、%d 条连线", len(g.Nodes), len(g.Edges))
	if len(g.Nodes) <= 2 {
		t.Errorf("[观察] 模型没有改动画布")
	}
	e.checkModelCalls(t)
}

func TestRealModel_Codemode(t *testing.T) {
	e := newRealEnv(t)
	sid := e.newSession(t)
	runID, status := e.say(t, sid, "请一次性在画布上创建 6 个图片节点，标题依次是 镜头01 到 镜头06，提示词各不相同（围绕雨夜便利店的故事一步步推进），并把它们按顺序依次连线（01→02→…→06）。如果有批量脚本类的工具，就用脚本一次做完。", realRunTimeout)
	evs := e.allEvents(t, sid)
	e.report(t, "批量创建（看是否走 codemode）", evs, runID)
	if status != "succeeded" {
		t.Errorf("运行应成功：%s", status)
	}
	usedCode, nested := false, 0
	for _, ev := range runEvents(evs, runID) {
		if ev["type"] == "tool.start" {
			if dataOf(ev)["name"] == "codemode" || strings.Contains(fmt.Sprint(dataOf(ev)["name"]), "code") {
				usedCode = true
			}
			if dataOf(ev)["parent_id"] != nil {
				nested++
			}
		}
	}
	g := e.graph(t)
	t.Logf("[观察] 用了 codemode=%v，脚本里的嵌套调用 %d 次；画布 %d 个节点、%d 条连线", usedCode, nested, len(g.Nodes), len(g.Edges))
	var recorded int64
	e.db.Model(&model.AgentToolCall{}).Where("parent_id <> ''").Count(&recorded)
	t.Logf("[观察] Go 库里记录的嵌套工具调用 %d 条", recorded)
	if usedCode && int64(nested) != recorded {
		t.Errorf("嵌套调用在事件里是 %d 次、Go 的工具调用记录里是 %d 条，应一致", nested, recorded)
	}
	e.checkModelCalls(t)
}

func TestRealModel_DeleteApprovalAndAsk(t *testing.T) {
	e := newRealEnv(t)
	sid := e.newSession(t)
	runID, status := e.say(t, sid, "把画布上那个“废稿”节点删掉。", realRunTimeout)
	e.report(t, "删除要审批", e.allEvents(t, sid), runID)
	if status != "waiting_approval" {
		t.Fatalf("[观察] 应停在等审批：%s", status)
	}
	ap := e.waitEvent(t, sid, "审批", func(ev map[string]any) bool { return ev["type"] == "approval.created" })
	e.data(t, "POST", "/agent/approvals/"+ap["data"].(map[string]any)["id"].(string)+"/decision", `{"decision":"approve"}`, 200)
	status = e.waitSettled(t, sid, runID, realRunTimeout)
	e.report(t, "批准之后续跑", e.allEvents(t, sid), runID)
	if status != "succeeded" || e.graph(t).Node("n_old") != nil {
		t.Errorf("批准后应成功且节点被删：%s，节点还在=%v", status, e.graph(t).Node("n_old") != nil)
	}

	runID, status = e.say(t, sid, "我想做个分镜，但还没想好要几个镜头。请先用提问的方式问我要几个镜头，等我回答之后再动手建一个“分镜组”，组里的图片节点数量等于我的回答。", realRunTimeout)
	e.report(t, "先提问", e.allEvents(t, sid), runID)
	if status != "waiting_input" {
		t.Fatalf("[观察] 应停在等回答：%s", status)
	}
	ap = e.waitEvent(t, sid, "提问", func(ev map[string]any) bool {
		return ev["type"] == "approval.created" && dataOf(ev)["kind"] == "ask"
	})
	e.data(t, "POST", "/agent/approvals/"+ap["data"].(map[string]any)["id"].(string)+"/decision", `{"decision":"approve","answer":"3 个镜头"}`, 200)
	status = e.waitSettled(t, sid, runID, realRunTimeout)
	e.report(t, "回答之后续跑", e.allEvents(t, sid), runID)
	if status != "succeeded" {
		t.Errorf("回答后应成功：%s", status)
	}
	t.Logf("[观察] 回答“3 个镜头”之后画布有 %d 个节点（原来 1 个剧本）", len(e.graph(t).Nodes))
	e.checkModelCalls(t)
}

func TestRealModel_CancelMidStream(t *testing.T) {
	e := newRealEnv(t)
	sid := e.newSession(t)
	runID := e.startRun(t, sid, "请不要改画布，直接给我写一篇不少于 1500 字的雨夜便利店短篇小说。")
	time.Sleep(6 * time.Second)
	e.data(t, "POST", "/agent/runs/"+runID+"/cancel", `{}`, 200)
	status := e.waitSettled(t, sid, runID, time.Minute)
	e.report(t, "流式中途取消", e.allEvents(t, sid), runID)
	if status != "canceled" {
		t.Errorf("应是 canceled：%s", status)
	}
	calls := e.checkModelCalls(t)
	if len(calls) == 0 {
		t.Error("被取消的调用也应有一条记录（已产生的部分照样计费）")
	}
}

// 压缩：窗口故意设得很小，在同一个会话里连续发起几轮，直到出现摘要调用；然后问一个只有看得到摘要才答得出的问题。
func TestRealModel_Compaction(t *testing.T) {
	e := newRealEnv(t)
	sid := e.newSession(t)
	// 前两轮建节点（摘要要保住节点 id）；之后每轮贴一大段剧本草稿、只要求回复「收到」：输出很短，输入很快涨过触发线，
	// 又不会因为 Pi 按「窗口 − 估算输入」压低 max_tokens 而把回答截断
	script := func(part int) string {
		var b strings.Builder
		for i := 1; i <= 90; i++ {
			fmt.Fprintf(&b, "第%d段第%d场：深夜十一点，雨还在下，便利店的霓虹灯在积水里碎成一片，店员小周第%d次看向门口，货架上的热饮在保温柜里发出嗡嗡声，监控屏幕里闪过一个撑黑伞的人影。\n", part, i, i)
		}
		return b.String()
	}
	prompts := []string{
		"先在剧本节点后面建一个叫“开场”的图片节点，提示词：雨夜便利店门口。回复里把新节点的 id 告诉我。",
		"再建一个“收银台”图片节点（提示词：便利店收银台，店员打瞌睡），把“开场”连到它。回复里告诉我两个节点的 id。",
	}
	for part := 1; part <= 8; part++ {
		prompts = append(prompts, "这是剧本草稿第 "+strconv.Itoa(part)+" 部分，请不要改画布，读完只回复“收到”两个字：\n"+script(part))
	}
	summarized := func() int64 {
		var n int64
		e.db.Model(&model.AgentModelCall{}).Where("purpose = ?", "summary").Count(&n)
		return n
	}
	var runs []string
	for i, p := range prompts {
		runID, status := e.say(t, sid, p, realRunTimeout)
		runs = append(runs, runID)
		if status != "succeeded" {
			t.Errorf("第 %d 轮应成功：%s", i+1, status)
		}
		var last model.AgentModelCall
		e.db.Order("id desc").Where("purpose = ?", "main").First(&last)
		t.Logf("第 %d 轮结束：最近一次对话调用输入 %d token；摘要调用累计 %d 次", i+1, last.InputTokens, summarized())
		if summarized() > 0 && i >= 3 {
			break
		}
	}
	runID, status := e.say(t, sid, "请不要改画布，也不要重新读取画布：根据我们之前的对话，回答——故事的主题是什么？我们已经建过哪些节点（写 id）？", realRunTimeout)
	evs := e.allEvents(t, sid)
	e.report(t, "压缩之后的回顾提问", evs, runID)
	if status != "succeeded" {
		t.Errorf("回顾提问应成功：%s", status)
	}
	if summarized() == 0 {
		t.Fatalf("[观察] 窗口 %s 下 %d 轮之后仍没有触发压缩", os.Getenv("REAL_LLM_WINDOW"), len(runs)+1)
	}
	// 压缩之后发出的请求里能找到摘要消息；把摘要正文记下来，看画布相关的信息（节点 id、要求）有没有保住
	id, _ := idcodec.Decode(runID)
	reqs, err := e.observe.RunRequests(context.Background(), agentsvc.ObserveActor{UserID: 1}, id)
	if err != nil {
		t.Fatal(err)
	}
	// 压缩之后发出的请求：把消息的开头逐条记下来，看摘要是怎么进上下文的；摘要的正文也记下来，看节点 id、要求有没有保住
	if len(reqs.Items) == 0 {
		t.Fatalf("取不到请求体")
	}
	last := reqs.Items[len(reqs.Items)-1]
	var raw map[string]any
	if err := json.Unmarshal(last.Body, &raw); err != nil {
		t.Fatalf("请求体解析失败（kept=%v，%d 字节）：%v", last.Kept, last.Bytes, err)
	}
	msgs, _ := raw["messages"].([]any)
	summaryAt := -1
	for i, m := range msgs {
		mm, _ := m.(map[string]any)
		text := messageText(mm["content"])
		t.Logf("    消息 %d [%v] %d 字：%s", i, mm["role"], len([]rune(text)), clip(text, 90))
		if summaryAt < 0 && (strings.Contains(text, "摘要") || strings.Contains(text, "summary")) && i > 0 {
			summaryAt = i
		}
	}
	if summaryAt < 0 {
		t.Errorf("压缩之后的请求里找不到摘要消息")
	} else {
		mm, _ := msgs[summaryAt].(map[string]any)
		t.Logf("[观察] 摘要消息（第 %d 条）正文：\n%s", summaryAt, clip(messageText(mm["content"]), 2500))
	}
	final := ""
	for _, ev := range runEvents(evs, runID) {
		if ev["type"] == "message.done" {
			final, _ = dataOf(ev)["text"].(string)
		}
	}
	t.Logf("[观察] 回顾提问的回答是否提到主题“雨夜便利店”=%v、是否提到节点 id（n_ 开头）=%v", strings.Contains(final, "便利店"), strings.Contains(final, "n_"))
	e.checkModelCalls(t)
}

func messageText(content any) string {
	switch c := content.(type) {
	case string:
		return c
	case []any:
		var b strings.Builder
		for _, part := range c {
			pm, _ := part.(map[string]any)
			if s, _ := pm["text"].(string); s != "" {
				b.WriteString(s)
			}
		}
		return b.String()
	}
	return ""
}

// 输出上限很小（思考内容也占额度）：以前运行会静默「成功」而回答是空的或半截的，现在要失败并说清楚。
func TestRealModel_OutputTruncated(t *testing.T) {
	e := newRealEnvWith(t, 300)
	sid := e.newSession(t)
	runID, status := e.say(t, sid, "请不要改画布，直接写一篇不少于 800 字的雨夜便利店短篇小说。", realRunTimeout)
	evs := e.allEvents(t, sid)
	e.report(t, "输出上限 300 token", evs, runID)
	if status != "failed" {
		t.Errorf("输出被截断应失败：%s", status)
	}
	for _, ev := range runEvents(evs, runID) {
		if ev["type"] == "run.status" && dataOf(ev)["status"] == "failed" && !strings.Contains(fmt.Sprint(dataOf(ev)["error"]), "截断") {
			t.Errorf("失败原因应说清楚是输出被截断：%v", dataOf(ev)["error"])
		}
	}
	e.checkModelCalls(t)
}
