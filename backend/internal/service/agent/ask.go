package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
)

// 提问（ask_user）的限制。
const (
	maxAskQuestions = 5    // 一张卡最多几个问题
	maxAskIDLen     = 32   // 题目 id 最长几个字
	maxAskOptionLen = 100  // 选项文字、follow 占位文字最长几个字
	maxAskAnswerLen = 2000 // 单题回答最长几个字（和决定请求里 answer 的上限一致）
)

// 提问题目的种类。
const (
	askChoice = "choice"
	askModel  = "model"
	askText   = "text"
	askURL    = "url"
	askNumber = "number"
)

var askKinds = []string{askChoice, askModel, askText, askURL, askNumber}

// askNumberRe 是数字题接受的写法：可带正负号和小数点，不接受科学计数法、十六进制、inf 之类。
var askNumberRe = regexp.MustCompile(`^[+-]?(\d+\.?\d*|\.\d+)$`)

// AskOption 是选择题的一个选项。
type AskOption struct {
	Label  string `json:"label"`            // 选项文字
	Follow string `json:"follow,omitempty"` // 非空表示选中后展开一个输入框，值是输入框的占位文字
}

// AskQuestion 是提问里归一化后的一道题。
type AskQuestion struct {
	ID          string      `json:"id"`                     // 题目标识，题内唯一
	Question    string      `json:"question"`               // 问题
	Kind        string      `json:"kind"`                   // choice / model / text / url / number
	Required    bool        `json:"required"`               // 是否必答
	Options     []AskOption `json:"options,omitempty"`      // kind=choice 的选项
	Multi       bool        `json:"multi,omitempty"`        // kind=choice 是否允许多选
	AllowCustom bool        `json:"allow_custom,omitempty"` // kind=choice 是否多带一个「其他，我来说」输入项
	ModelKind   string      `json:"model_kind,omitempty"`   // kind=model 时要选哪类模型
	Models      []AskModel  `json:"models,omitempty"`       // kind=model 时可选的已发布模型
	Placeholder string      `json:"placeholder,omitempty"`  // text / url / number 输入框的占位文字
}

// AskDecidedAnswer 是写进决定里的逐题回答。
type AskDecidedAnswer struct {
	ID       string `json:"id"`
	Question string `json:"question"`
	Answer   string `json:"answer"` // 人类可读的一句；没回答是空串
	Value    any    `json:"value"`  // 原始回答：字符串，多选是字符串数组；没回答是空串
}

// askOptionArg 是模型给的选项：字符串，或 {label, follow}。
type askOptionArg struct {
	Label  string
	Follow string
}

func (o *askOptionArg) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		o.Label = s
		return nil
	}
	var m struct {
		Label  string `json:"label"`
		Follow string `json:"follow"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return errors.New("选项要么是字符串，要么是 {label, follow} 对象")
	}
	o.Label, o.Follow = m.Label, m.Follow
	return nil
}

// askQuestionArg 是模型给的一道题（也是旧的单题参数的形状）。
type askQuestionArg struct {
	ID          string         `json:"id"`
	Question    string         `json:"question"`
	Kind        string         `json:"kind"`
	Required    *bool          `json:"required"`
	Options     []askOptionArg `json:"options"`
	Multi       bool           `json:"multi"`
	AllowCustom bool           `json:"allowCustom"`
	ModelKind   string         `json:"modelKind"`
	Placeholder string         `json:"placeholder"`
}

// toolAsk 向用户提问：一张卡可以列 1 到 5 个问题，创建提问并让本轮停下等回答。
// 旧的单题参数（顶层 question + kind）等同于只有一题。
func (b *AgentBridge) toolAsk(ctx context.Context, tc *toolCall) (*toolOut, error) {
	var a struct {
		askQuestionArg
		Questions []askQuestionArg `json:"questions"`
	}
	if err := json.Unmarshal(tc.args, &a); err != nil {
		return nil, fail("参数格式不对：%v", err)
	}
	args := a.Questions
	if len(args) == 0 {
		if strings.TrimSpace(a.Question) == "" && a.Kind == "" {
			return nil, fail("参数里需要 questions 数组，1 到 %d 题", maxAskQuestions)
		}
		args = []askQuestionArg{a.askQuestionArg}
	}
	p, err := b.buildAskPayload(ctx, args)
	if err != nil {
		return nil, err
	}
	if err := b.requestApproval(ctx, tc, model.ApprovalAsk, p, 0); err != nil {
		return nil, err
	}
	summary := "向用户提问：" + truncateRunes(p.Questions[0].Question, 40)
	if len(p.Questions) > 1 {
		summary = fmt.Sprintf("向用户提问（%d 题）：%s", len(p.Questions), truncateRunes(p.Questions[0].Question, 30))
	}
	return &toolOut{content: "已向用户提问，等待回答。用户回答后你会收到结果。", terminate: true, summary: summary}, nil
}

// buildAskPayload 校验并归一化每一题，组出提问内容。顶层的 question / kind / options / models 保留，
// 让只认识旧字段的老客户端和历史记录照常显示：单题时就是这一题，多题时 question 是标题，其余填第一题。
func (b *AgentBridge) buildAskPayload(ctx context.Context, args []askQuestionArg) (AskPayload, error) {
	if len(args) > maxAskQuestions {
		return AskPayload{}, fail("一张卡最多 %d 个问题，现在有 %d 个；挑最关键的问，其余等用户回答后再问", maxAskQuestions, len(args))
	}
	qs := make([]AskQuestion, 0, len(args))
	seen := map[string]bool{}
	for i, arg := range args {
		q, err := b.buildAskQuestion(ctx, i, arg)
		if err != nil {
			return AskPayload{}, err
		}
		if seen[q.ID] {
			return AskPayload{}, fail("第 %d 题：题目 id %q 重复，id 在同一张卡里不能重复", i+1, q.ID)
		}
		seen[q.ID] = true
		qs = append(qs, q)
	}
	first := qs[0]
	p := AskPayload{Question: first.Question, Kind: first.Kind, ModelKind: first.ModelKind, Models: first.Models, AllowCustom: first.AllowCustom, Questions: qs}
	if len(qs) > 1 {
		p.Question = fmt.Sprintf("还差 %d 件事要你定", len(qs))
	}
	for _, o := range first.Options {
		p.Options = append(p.Options, o.Label)
	}
	return p, nil
}

// buildAskQuestion 校验并归一化一道题。错误信息带题号，让模型知道改哪一题。
func (b *AgentBridge) buildAskQuestion(ctx context.Context, i int, arg askQuestionArg) (AskQuestion, error) {
	at := fmt.Sprintf("第 %d 题：", i+1)
	text := strings.TrimSpace(arg.Question)
	if text == "" || utf8.RuneCountInString(text) > maxQuestionLen {
		return AskQuestion{}, fail("%s问题不能为空，且不超过 %d 个字", at, maxQuestionLen)
	}
	id := strings.TrimSpace(arg.ID)
	if id == "" {
		id = strconv.Itoa(i + 1)
	}
	if utf8.RuneCountInString(id) > maxAskIDLen {
		return AskQuestion{}, fail("%sid 不超过 %d 个字", at, maxAskIDLen)
	}
	q := AskQuestion{ID: id, Question: text, Kind: arg.Kind, Required: arg.Required == nil || *arg.Required}
	switch arg.Kind {
	case askChoice:
		opts, err := askOptions(at, arg.Options)
		if err != nil {
			return AskQuestion{}, err
		}
		q.Options, q.Multi, q.AllowCustom = opts, arg.Multi, arg.AllowCustom
	case askModel:
		models, err := b.askModels(ctx, arg.ModelKind)
		if err != nil {
			var tf *toolFailure
			if errors.As(err, &tf) {
				return AskQuestion{}, fail("%s%s", at, tf.msg)
			}
			return AskQuestion{}, err
		}
		q.ModelKind, q.Models = arg.ModelKind, models
	case askText, askURL, askNumber:
		ph := strings.TrimSpace(arg.Placeholder)
		if utf8.RuneCountInString(ph) > maxAskOptionLen {
			return AskQuestion{}, fail("%splaceholder 不超过 %d 个字", at, maxAskOptionLen)
		}
		q.Placeholder = ph
	default:
		return AskQuestion{}, fail("%skind 只能是 %s", at, strings.Join(askKinds, "、"))
	}
	return q, nil
}

// askOptions 校验选择题的选项：2 到 4 个，文字非空、不重复、不超长。
func askOptions(at string, args []askOptionArg) ([]AskOption, error) {
	if len(args) < 2 || len(args) > 4 {
		return nil, fail("%s选项题需要 2 到 4 个选项", at)
	}
	out := make([]AskOption, 0, len(args))
	for _, o := range args {
		label, follow := strings.TrimSpace(o.Label), strings.TrimSpace(o.Follow)
		if label == "" || utf8.RuneCountInString(label) > maxAskOptionLen {
			return nil, fail("%s每个选项的文字不能为空，且不超过 %d 个字", at, maxAskOptionLen)
		}
		if utf8.RuneCountInString(follow) > maxAskOptionLen {
			return nil, fail("%s选项 %q 的 follow 不超过 %d 个字", at, label, maxAskOptionLen)
		}
		if slices.ContainsFunc(out, func(x AskOption) bool { return x.Label == label }) {
			return nil, fail("%s选项 %q 重复", at, label)
		}
		out = append(out, AskOption{Label: label, Follow: follow})
	}
	return out, nil
}

// askQuestions 取提问的各题。旧的单题提问（没有 questions）按顶层字段还原成一题。
func (p *AskPayload) askQuestions() []AskQuestion {
	if len(p.Questions) > 0 {
		return p.Questions
	}
	q := AskQuestion{ID: "1", Question: p.Question, Kind: p.Kind, Required: true, ModelKind: p.ModelKind, Models: p.Models, AllowCustom: p.AllowCustom}
	for _, o := range p.Options {
		q.Options = append(q.Options, AskOption{Label: o})
	}
	return []AskQuestion{q}
}

// resolveAsk 回答提问：必填题都要有值，每题的值要符合题目的种类。
// 旧客户端只传 answer：单题提问仍然可用，决定的形状和以前完全一样。
func resolveAsk(a *model.AgentApproval, req *model.DecideAgentApprovalReq) (*outcome, error) {
	var p AskPayload
	if len(a.PayloadJSON) > 0 {
		if err := json.Unmarshal(a.PayloadJSON, &p); err != nil {
			return nil, err
		}
	}
	if len(p.Questions) == 0 && len(req.Answers) == 0 { // 旧提问、旧客户端
		if len([]rune(req.Answer)) == 0 {
			return nil, errcode.ErrInvalidParams.WithMsg("请先回答")
		}
		return &outcome{status: model.ApprovalApproved, decision: map[string]any{"decision": "approve", "answer": req.Answer}}, nil
	}
	qs := p.askQuestions()
	given, err := givenAnswers(qs, req)
	if err != nil {
		return nil, err
	}
	decided := make([]AskDecidedAnswer, 0, len(qs))
	var sentences []string
	for i, q := range qs {
		d, err := decideAskQuestion(i, q, given[q.ID])
		if err != nil {
			return nil, err
		}
		decided = append(decided, d)
		if d.Answer != "" {
			sentences = append(sentences, q.Question+"："+d.Answer)
		}
	}
	answer := strings.Join(sentences, "；")
	if len(qs) == 1 {
		answer = decided[0].Answer // 单题只留回答本身，和旧展示一致
	}
	return &outcome{status: model.ApprovalApproved, decision: map[string]any{"decision": "approve", "answer": answer, "answers": decided}}, nil
}

// givenAnswers 把请求里的回答按题目 id 对齐。没有 answers 时，单题把 answer 当它的回答。
func givenAnswers(qs []AskQuestion, req *model.DecideAgentApprovalReq) (map[string]any, error) {
	given := map[string]any{}
	if len(req.Answers) == 0 {
		if len(qs) == 1 && strings.TrimSpace(req.Answer) != "" {
			given[qs[0].ID] = req.Answer
		}
		return given, nil
	}
	for _, an := range req.Answers {
		if !slices.ContainsFunc(qs, func(q AskQuestion) bool { return q.ID == an.ID }) {
			return nil, errcode.ErrInvalidParams.WithMsg(fmt.Sprintf("回答里有不存在的题目：%s", an.ID))
		}
		if _, dup := given[an.ID]; dup {
			return nil, errcode.ErrInvalidParams.WithMsg(fmt.Sprintf("题目 %s 回答了不止一次", an.ID))
		}
		given[an.ID] = an.Value
	}
	return given, nil
}

// decideAskQuestion 校验第 i 题的回答并记成决定；没答的选填题记空回答。
func decideAskQuestion(i int, q AskQuestion, raw any) (AskDecidedAnswer, error) {
	d := AskDecidedAnswer{ID: q.ID, Question: q.Question, Value: ""}
	vals, list, err := askValues(raw)
	if err != nil {
		return d, askInvalid(i, q, err.Error())
	}
	if len(vals) == 0 {
		if q.Required {
			return d, askInvalid(i, q, "还没回答")
		}
		return d, nil
	}
	readable, err := checkAskValues(q, vals)
	if err != nil {
		return d, askInvalid(i, q, err.Error())
	}
	d.Answer, d.Value = readable, vals[0]
	if list {
		d.Value = vals
	}
	return d, nil
}

func askInvalid(i int, q AskQuestion, why string) error {
	return errcode.ErrInvalidParams.WithMsg(fmt.Sprintf("第 %d 题「%s」%s", i+1, truncateRunes(q.Question, 20), why))
}

// askValues 把回答的值整理成字符串列表：字符串是一个值，数组是多个值；空白的值丢掉。list 表示原本是数组。
func askValues(v any) (vals []string, list bool, err error) {
	add := func(s string) {
		if s = strings.TrimSpace(s); s != "" {
			vals = append(vals, s)
		}
	}
	switch x := v.(type) {
	case nil:
	case string:
		add(x)
	case []string:
		list = true
		for _, s := range x {
			add(s)
		}
	case []any:
		list = true
		for _, e := range x {
			s, ok := e.(string)
			if !ok {
				return nil, false, errors.New("的回答格式不对，应是文字或文字数组")
			}
			add(s)
		}
	default:
		return nil, false, errors.New("的回答格式不对，应是文字或文字数组")
	}
	return vals, list, nil
}

// checkAskValues 按题目种类校验回答，返回人类可读的一句。错误文字接在「第 N 题「…」」后面。
func checkAskValues(q AskQuestion, vals []string) (string, error) {
	if len(vals) > 1 && (q.Kind != askChoice || !q.Multi) {
		return "", errors.New("只能回答一项")
	}
	for _, v := range vals {
		if utf8.RuneCountInString(v) > maxAskAnswerLen {
			return "", fmt.Errorf("的回答太长，不超过 %d 个字", maxAskAnswerLen)
		}
	}
	switch q.Kind {
	case askChoice:
		return checkAskChoice(q, vals)
	case askModel:
		return checkAskModel(q, vals[0])
	case askNumber:
		if !askNumberRe.MatchString(vals[0]) {
			return "", errors.New("需要填数字")
		}
	case askURL:
		u, err := url.Parse(vals[0])
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return "", errors.New("需要填 http 或 https 开头的链接")
		}
	}
	return vals[0], nil
}

// checkAskChoice 选项题：值要是某个选项；选了带 follow 的选项、或「其他，我来说」时，回答是用户输入的文字，不是选项原文。
func checkAskChoice(q AskQuestion, vals []string) (string, error) {
	free := q.AllowCustom || slices.ContainsFunc(q.Options, func(o AskOption) bool { return o.Follow != "" })
	for _, v := range vals {
		if !free && !slices.ContainsFunc(q.Options, func(o AskOption) bool { return o.Label == v }) {
			return "", fmt.Errorf("的回答 %q 不是可选项", truncateRunes(v, 20))
		}
	}
	return strings.Join(vals, "、"), nil
}

// checkAskModel 选模型题：值要是提问时列出的某个模型 key。
func checkAskModel(q AskQuestion, key string) (string, error) {
	for _, m := range q.Models {
		if m.Key == key {
			return fmt.Sprintf("%s（模型 key：%s）", firstNonEmpty(m.Name, m.Key), m.Key), nil // 模型要拿 key 去建节点
		}
	}
	return "", errors.New("选的模型不在可选范围内")
}

// askOutcomeText 是提问被回答后交给模型的文字：逐题列出「问题 → 回答」，未答的选填题写「未回答」。
func askOutcomeText(out *outcome) string {
	var decided []AskDecidedAnswer
	if raw, err := json.Marshal(out.decision["answers"]); err == nil {
		_ = json.Unmarshal(raw, &decided) // 旧决定没有 answers，解析不出来就是空，走下面的老文字
	}
	if len(decided) == 0 {
		return "用户回答：" + fmt.Sprint(out.decision["answer"])
	}
	if len(decided) == 1 && decided[0].Answer != "" {
		return "用户回答：" + decided[0].Answer
	}
	lines := make([]string, len(decided))
	for i, d := range decided {
		answer := d.Answer
		if answer == "" {
			answer = "未回答"
		}
		lines[i] = fmt.Sprintf("%d. %s → %s", i+1, d.Question, answer)
	}
	return "用户回答了你的提问：\n" + strings.Join(lines, "\n")
}

// askSkippedText 是提问被跳过（用户没答，改发了新消息）后交给模型的文字。
func askSkippedText(out *outcome) string {
	subject := "这些问题"
	if n, ok := out.decision["question_count"].(int); ok && n == 1 {
		subject = "这个问题"
	} else if f, ok := out.decision["question_count"].(float64); ok && f == 1 { // 经 JSON 往返回来的数字
		subject = "这个问题"
	}
	return "用户没有回答" + subject + "，改发了新消息；请以用户的新消息为准，确有必要再问。"
}
