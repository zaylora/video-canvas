package agent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"video-canvas/internal/model"
	. "video-canvas/internal/service/agent"
)

// 提问卡一次列多个问题：提议（参数校验和归一化）、解析（逐题校验回答）、给模型的文字。

// multiAsk 是三种题各一道的多题提问：选项带 follow、多选、选模型、选填的文字题。
func multiAsk() map[string]any {
	return map[string]any{"questions": []any{
		map[string]any{"id": "model", "question": "要接哪个模型？", "kind": "choice", "options": []any{
			map[string]any{"label": "告诉你模型名称", "follow": "如 kling-v3"}, "价格页上的都接",
		}},
		map[string]any{"id": "tags", "question": "要哪些风格？", "kind": "choice", "multi": true, "options": []any{"写实", "动漫", "水墨"}},
		map[string]any{"id": "img", "question": "用哪个图片模型？", "kind": "model", "modelKind": "image"},
		map[string]any{"id": "note", "question": "还有补充吗？", "kind": "text", "required": false, "placeholder": "随便写"},
	}}
}

func proposeAsk(t *testing.T, args any) (*ApprovalProposal, AskPayload) {
	t.Helper()
	e := newApprovalEnv(t, "all")
	p := propose(t, e, platformCtx(900501, "all"), "ask", args)
	var a AskPayload
	if !p.IsError {
		if err := json.Unmarshal(p.Payload, &a); err != nil {
			t.Fatal(err)
		}
	}
	return p, a
}

func TestProposeAsk_MultiQuestionsNormalized(t *testing.T) {
	p, a := proposeAsk(t, multiAsk())
	if p.IsError || !p.Terminate {
		t.Fatalf("%+v", p)
	}
	if len(a.Questions) != 4 {
		t.Fatalf("应有 4 题: %+v", a)
	}
	q0, q1, q2, q3 := a.Questions[0], a.Questions[1], a.Questions[2], a.Questions[3]
	if q0.ID != "model" || !q0.Required || q0.Options[0].Follow != "如 kling-v3" || q0.Options[1].Label != "价格页上的都接" || q0.Options[1].Follow != "" {
		t.Errorf("选项可以是字符串或 {label, follow}，required 默认 true: %+v", q0)
	}
	if !q1.Multi || len(q1.Options) != 3 {
		t.Errorf("多选: %+v", q1)
	}
	if q2.Kind != "model" || len(q2.Models) != 1 || q2.Models[0].Key != "seedream" {
		t.Errorf("选模型题要带已发布的模型列表: %+v", q2)
	}
	if q3.Required || q3.Placeholder != "随便写" {
		t.Errorf("选填文字题: %+v", q3)
	}
	// 顶层为老客户端保留：标题 + 第一题
	if a.Question != "还差 4 件事要你定" || a.Kind != "choice" || len(a.Options) != 2 || a.Options[0] != "告诉你模型名称" {
		t.Errorf("多题时顶层 question 是标题，其余填第一题: %+v", a)
	}
	if !strings.Contains(p.Summary, "4 题") {
		t.Errorf("摘要要带题数: %q", p.Summary)
	}
}

func TestProposeAsk_IDDefaultsToIndex(t *testing.T) {
	_, a := proposeAsk(t, map[string]any{"questions": []any{
		map[string]any{"question": "a?", "kind": "text"},
		map[string]any{"question": "b?", "kind": "number"},
	}})
	if a.Questions[0].ID != "1" || a.Questions[1].ID != "2" {
		t.Errorf("缺省 id 用序号: %+v", a.Questions)
	}
}

func TestProposeAsk_LegacySingleQuestionBecomesOneQuestion(t *testing.T) {
	p, a := proposeAsk(t, map[string]any{"question": "横屏还是竖屏？", "kind": "choice", "options": []string{"横屏", "竖屏"}, "allowCustom": true})
	if p.IsError || len(a.Questions) != 1 {
		t.Fatalf("%+v %+v", p, a)
	}
	q := a.Questions[0]
	if q.ID != "1" || q.Question != "横屏还是竖屏？" || !q.Required || !q.AllowCustom || len(q.Options) != 2 || q.Options[1].Label != "竖屏" {
		t.Errorf("旧参数归一成一题: %+v", q)
	}
	if a.Question != "横屏还是竖屏？" || a.Kind != "choice" || len(a.Options) != 2 || !a.AllowCustom {
		t.Errorf("单题时顶层字段就是这一题: %+v", a)
	}
	_, m := proposeAsk(t, map[string]any{"question": "用哪个？", "kind": "model", "modelKind": "image"})
	if m.Kind != "model" || len(m.Models) != 1 || m.Questions[0].Models[0].Key != "seedream" {
		t.Errorf("旧的选模型题: %+v", m)
	}
}

func TestProposeAsk_RejectsBadInput(t *testing.T) {
	q := func(m map[string]any) any { return map[string]any{"questions": []any{m}} }
	many := make([]any, 6)
	for i := range many {
		many[i] = map[string]any{"question": "q", "kind": "text"}
	}
	long := strings.Repeat("长", 201)
	cases := map[string]struct {
		args any
		want string
	}{
		"什么参数都没有":     {map[string]any{}, "questions"},
		"空 questions": {map[string]any{"questions": []any{}}, "questions"},
		"题太多":         {map[string]any{"questions": many}, "最多 5"},
		"问题为空":        {q(map[string]any{"question": " ", "kind": "text"}), "第 1 题"},
		"问题太长":        {q(map[string]any{"question": long, "kind": "text"}), "200"},
		"kind 不认识":    {q(map[string]any{"question": "q", "kind": "free"}), "kind 只能是"},
		"选项太少":        {q(map[string]any{"question": "q", "kind": "choice", "options": []any{"a"}}), "2 到 4"},
		"选项太多":        {q(map[string]any{"question": "q", "kind": "choice", "options": []any{"a", "b", "c", "d", "e"}}), "2 到 4"},
		"选项为空":        {q(map[string]any{"question": "q", "kind": "choice", "options": []any{"a", " "}}), "不能为空"},
		"选项重复":        {q(map[string]any{"question": "q", "kind": "choice", "options": []any{"a", "a"}}), "重复"},
		"选项类型不对":      {q(map[string]any{"question": "q", "kind": "choice", "options": []any{"a", 3}}), "选项"},
		"模型种类不对":      {q(map[string]any{"question": "q", "kind": "model", "modelKind": "audio"}), "第 1 题"},
		"id 重复": {map[string]any{"questions": []any{
			map[string]any{"id": "x", "question": "a", "kind": "text"}, map[string]any{"id": "x", "question": "b", "kind": "text"}}}, "重复"},
		"显式 id 撞上缺省序号": {map[string]any{"questions": []any{
			map[string]any{"id": "2", "question": "a", "kind": "text"}, map[string]any{"question": "b", "kind": "text"}}}, "重复"},
		"第二题出错要指出题号": {map[string]any{"questions": []any{
			map[string]any{"question": "a", "kind": "text"}, map[string]any{"question": "b", "kind": "nope"}}}, "第 2 题"},
	}
	for name, c := range cases {
		p, _ := proposeAsk(t, c.args)
		if !p.IsError || !strings.Contains(p.Content, c.want) {
			t.Errorf("%s应是含 %q 的工具错误: %+v", name, c.want, p)
		}
	}
}

// askApproval 把提议出来的提问内容原样当作审批内容，用来测解析。
func askPayloadJSON(t *testing.T, args any) []byte {
	t.Helper()
	p, _ := proposeAsk(t, args)
	if p.IsError {
		t.Fatalf("%+v", p)
	}
	return p.Payload
}

func resolveAskReq(t *testing.T, payload []byte, req model.DecideAgentApprovalReq) (*ApprovalOutcome, error) {
	t.Helper()
	b := newBridgeEnv(t)
	return b.br.ResolveApproval(context.Background(), platformCtx(900502, "all"), model.ApprovalAsk, payload, req)
}

func ans(id string, v any) model.ApprovalAnswer { return model.ApprovalAnswer{ID: id, Value: v} }

func TestResolveAsk_MultiQuestionValidation(t *testing.T) {
	payload := askPayloadJSON(t, multiAsk())
	good := []model.ApprovalAnswer{ans("model", "kling-v3"), ans("tags", []any{"写实", "水墨"}), ans("img", "seedream")}
	approve := func(as ...model.ApprovalAnswer) model.DecideAgentApprovalReq {
		return model.DecideAgentApprovalReq{Decision: "approve", Answers: as}
	}
	out, err := resolveAskReq(t, payload, approve(good...))
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != model.ApprovalApproved {
		t.Errorf("%+v", out)
	}
	if want := "要接哪个模型？：kling-v3；要哪些风格？：写实、水墨；用哪个图片模型？：Seedream（模型 key：seedream）"; out.Decision["answer"] != want {
		t.Errorf("answer 是每题「问题：回答」用分号连起来，选填没答的不出现:\n got %v\nwant %s", out.Decision["answer"], want)
	}
	raw, _ := json.Marshal(out.Decision["answers"])
	var decided []AskDecidedAnswer
	if err := json.Unmarshal(raw, &decided); err != nil || len(decided) != 4 {
		t.Fatalf("decision 要逐题记下回答（含没答的选填题）: %s", raw)
	}
	if decided[1].Answer != "写实、水墨" || decided[3].Answer != "" || decided[2].Question != "用哪个图片模型？" {
		t.Errorf("%+v", decided)
	}
	if v, ok := decided[1].Value.([]any); !ok || len(v) != 2 {
		t.Errorf("多选的原始值是数组: %#v", decided[1].Value)
	}
	if decided[0].Value != "kling-v3" {
		t.Errorf("单选的原始值是字符串: %#v", decided[0].Value)
	}

	bad := map[string]struct {
		req  model.DecideAgentApprovalReq
		want string
	}{
		"缺必填题":      {approve(good[:2]...), "第 3 题"},
		"必填题给空串":    {approve(ans("model", " "), good[1], good[2]), "还没回答"},
		"模型不在列表里":   {approve(good[0], good[1], ans("img", "nope")), "不在可选范围"},
		"多选里有不存在的项": {approve(ans("model", "价格页上的都接"), ans("tags", []any{"写实", "赛博"}), good[2]), "不是可选项"},
		"单选给了多个":    {approve(ans("model", []any{"a", "b"}), good[1], good[2]), "只能回答一项"},
		"不存在的题":     {approve(append([]model.ApprovalAnswer{ans("zzz", "x")}, good...)...), "不存在的题目"},
		"同一题答两次":    {approve(append([]model.ApprovalAnswer{good[0]}, good...)...), "不止一次"},
		"值类型不对":     {approve(ans("model", 3.5), good[1], good[2]), "格式不对"},
	}
	for name, c := range bad {
		if _, err := resolveAskReq(t, payload, c.req); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s应报含 %q 的错误，实际 %v", name, c.want, err)
		}
	}
	// 只传 answer 的旧客户端答不了多题提问
	if _, err := resolveAskReq(t, payload, model.DecideAgentApprovalReq{Decision: "approve", Answer: "随便"}); err == nil {
		t.Error("多题提问不能只传 answer")
	}
}

func TestResolveAsk_ChoiceWithoutFreeTextMustMatchOption(t *testing.T) {
	payload := askPayloadJSON(t, map[string]any{"questions": []any{map[string]any{"question": "画幅？", "kind": "choice", "options": []any{"16:9", "9:16"}}}})
	req := func(v string) model.DecideAgentApprovalReq {
		return model.DecideAgentApprovalReq{Decision: "approve", Answers: []model.ApprovalAnswer{ans("1", v)}}
	}
	if _, err := resolveAskReq(t, payload, req("1:1")); err == nil || !strings.Contains(err.Error(), "不是可选项") {
		t.Errorf("既没有 follow 也不允许自定义，回答必须是某个选项: %v", err)
	}
	out, err := resolveAskReq(t, payload, req("9:16"))
	if err != nil || out.Decision["answer"] != "9:16" {
		t.Errorf("单题 answer 只留回答本身: %v %+v", err, out)
	}
	// 允许自定义时，文字就是回答
	custom := askPayloadJSON(t, map[string]any{"questions": []any{map[string]any{"question": "画幅？", "kind": "choice", "options": []any{"16:9", "9:16"}, "allowCustom": true}}})
	if out, err := resolveAskReq(t, custom, req("2.35:1")); err != nil || out.Decision["answer"] != "2.35:1" {
		t.Errorf("allowCustom: %v %+v", err, out)
	}
}

func TestResolveAsk_NumberAndURLQuestions(t *testing.T) {
	payload := askPayloadJSON(t, map[string]any{"questions": []any{
		map[string]any{"id": "n", "question": "几秒？", "kind": "number"},
		map[string]any{"id": "u", "question": "文档链接？", "kind": "url", "required": false},
	}})
	req := func(as ...model.ApprovalAnswer) model.DecideAgentApprovalReq {
		return model.DecideAgentApprovalReq{Decision: "approve", Answers: as}
	}
	for _, v := range []string{"abc", "1e3", "0x10", "Inf", "12 秒", "1.2.3"} {
		if _, err := resolveAskReq(t, payload, req(ans("n", v))); err == nil || !strings.Contains(err.Error(), "数字") {
			t.Errorf("%q 不是数字: %v", v, err)
		}
	}
	for _, v := range []string{"ftp://x.com/a", "javascript:alert(1)", "example.com", "http://", "https:///a"} {
		if _, err := resolveAskReq(t, payload, req(ans("n", "5"), ans("u", v))); err == nil || !strings.Contains(err.Error(), "http") {
			t.Errorf("%q 不是 http(s) 链接: %v", v, err)
		}
	}
	out, err := resolveAskReq(t, payload, req(ans("n", "-3.5"), ans("u", "https://example.com/doc")))
	if err != nil || out.Decision["answer"] != "几秒？：-3.5；文档链接？：https://example.com/doc" {
		t.Errorf("%v %+v", err, out)
	}
	// 选填的链接不答也能提交
	if _, err := resolveAskReq(t, payload, req(ans("n", "5"))); err != nil {
		t.Errorf("选填题可以不答: %v", err)
	}
}

func TestResolveAsk_LegacyClientsKeepWorking(t *testing.T) {
	// 历史里的旧提问：payload 没有 questions，旧客户端只传 answer
	old, _ := json.Marshal(AskPayload{Question: "画幅？", Kind: "choice", Options: []string{"16:9", "9:16"}})
	out, err := resolveAskReq(t, old, model.DecideAgentApprovalReq{Decision: "approve", Answer: "16:9"})
	if err != nil {
		t.Fatal(err)
	}
	if d, _ := json.Marshal(out.Decision); string(d) != `{"answer":"16:9","decision":"approve"}` {
		t.Errorf("旧提问的决定形状不变: %s", d)
	}
	if _, err := resolveAskReq(t, old, model.DecideAgentApprovalReq{Decision: "approve"}); err == nil {
		t.Error("旧提问回答不能为空")
	}
	// 新生成的单题提问，旧客户端只传 answer 也行
	single := askPayloadJSON(t, map[string]any{"question": "画幅？", "kind": "choice", "options": []string{"16:9", "9:16"}})
	out, err = resolveAskReq(t, single, model.DecideAgentApprovalReq{Decision: "approve", Answer: "9:16"})
	if err != nil || out.Decision["answer"] != "9:16" {
		t.Errorf("单题只传 answer: %v %+v", err, out)
	}
}

func TestExecuteApproval_AskOutcomeTexts(t *testing.T) {
	e := newExecEnv(t)
	rc := platformCtx(900503, "all")
	payload := askPayloadJSON(t, multiAsk())
	b := newBridgeEnv(t)
	resolved, err := b.br.ResolveApproval(context.Background(), rc, model.ApprovalAsk, payload, model.DecideAgentApprovalReq{Decision: "approve",
		Answers: []model.ApprovalAnswer{ans("model", "kling-v3"), ans("tags", []any{"写实"}), ans("img", "seedream")}})
	if err != nil {
		t.Fatal(err)
	}
	// Agent 服务把决定存成 JSON 再交回来，数字和列表的类型会变，文字要照样出
	raw, _ := json.Marshal(resolved)
	var back ApprovalOutcome
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	res := execApproval(t, e, rc, model.ApprovalAsk, "c1", back)
	want := "用户回答了你的提问：\n1. 要接哪个模型？ → kling-v3\n2. 要哪些风格？ → 写实\n3. 用哪个图片模型？ → Seedream（模型 key：seedream）\n4. 还有补充吗？ → 未回答"
	if res.Status != "none" || res.OutcomeText != want {
		t.Errorf("逐题列出，未答的选填题写「未回答」:\n got %q\nwant %q", res.OutcomeText, want)
	}
	// 单题照旧
	one := askPayloadJSON(t, map[string]any{"question": "画幅？", "kind": "choice", "options": []string{"16:9", "9:16"}})
	r1, err := b.br.ResolveApproval(context.Background(), rc, model.ApprovalAsk, one, model.DecideAgentApprovalReq{Decision: "approve", Answers: []model.ApprovalAnswer{ans("1", "16:9")}})
	if err != nil {
		t.Fatal(err)
	}
	if got := execApproval(t, e, rc, model.ApprovalAsk, "c2", *r1).OutcomeText; got != "用户回答：16:9" {
		t.Errorf("单题: %q", got)
	}
}

func TestResolveAndExecute_SkippedAskText(t *testing.T) {
	e := newExecEnv(t)
	rc := platformCtx(900504, "all")
	b := newBridgeEnv(t)
	const multi = "用户没有回答这些问题，改发了新消息；请以用户的新消息为准，确有必要再问。"
	const single = "用户没有回答这个问题，改发了新消息；请以用户的新消息为准，确有必要再问。"
	for name, c := range map[string]struct {
		payload []byte
		want    string
	}{
		"多题": {askPayloadJSON(t, multiAsk()), multi},
		"单题": {askPayloadJSON(t, map[string]any{"question": "q?", "kind": "text"}), single},
		"历史里的旧提问": {func() []byte {
			j, _ := json.Marshal(AskPayload{Question: "画幅？", Kind: "choice", Options: []string{"a", "b"}})
			return j
		}(), single},
	} {
		out, err := b.br.ResolveApproval(context.Background(), rc, model.ApprovalAsk, c.payload, model.DecideAgentApprovalReq{Decision: "reject"})
		if err != nil || out.Status != model.ApprovalRejected {
			t.Fatalf("%s: %v %+v", name, err, out)
		}
		raw, _ := json.Marshal(out)
		var back ApprovalOutcome
		_ = json.Unmarshal(raw, &back) // 经 JSON 往返，和 Agent 服务交回来的一样
		if got := execApproval(t, e, rc, model.ApprovalAsk, "c", back).OutcomeText; got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", name, got, c.want)
		}
		if got := execApproval(t, e, rc, model.ApprovalAsk, "c", *out).OutcomeText; got != c.want {
			t.Errorf("%s（不经 JSON）:\n got %q\nwant %q", name, got, c.want)
		}
	}
}
