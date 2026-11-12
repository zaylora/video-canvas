package stepkit_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"video-canvas/internal/stepkit"
)

// 五个协议块必须按「职责 → 输入 → 方法 → 输出格式 → 校验规则」排列，结构要求最后出现。
func TestBuildPromptSectionOrder(t *testing.T) {
	step := newTestStep()
	method := stepkit.BuiltinMethod(step)
	prompt, err := stepkit.BuildPrompt(step, method, stepkit.Input{Vars: map[string]any{"product": "晨雾香水"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	last := -1
	for _, h := range []string{"## 本步职责", "## 输入", "## 方法", "## 输出格式", "## 校验规则"} {
		i := strings.Index(prompt, h)
		if i < 0 {
			t.Fatalf("缺少 %s：\n%s", h, prompt)
		}
		if i <= last {
			t.Fatalf("%s 的位置不对：\n%s", h, prompt)
		}
		last = i
	}
	for _, want := range []string{"把商品信息写成镜头列表。", "晨雾香水", "内置方法：先想卖点，再写镜头。", "标题不能含禁用词", "标题不宜夸张"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("提示词缺少 %q", want)
		}
	}
	// 内置方法不加「写作方法」包裹
	if strings.Contains(prompt, "【写作方法") {
		t.Fatal("内置方法不应有技能包裹")
	}
}

// 输出格式块由 Schema 生成字段说明，并附 Schema 原文。
func TestBuildPromptOutputFormatFromSchema(t *testing.T) {
	step := newTestStep()
	prompt, err := stepkit.BuildPrompt(step, stepkit.BuiltinMethod(step), stepkit.Input{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	format := prompt[strings.Index(prompt, "## 输出格式"):strings.Index(prompt, "## 校验规则")]
	for _, want := range []string{"title", "shots[].text", "镜头描述", "必填", `"minItems": 1`} {
		if !strings.Contains(format, want) {
			t.Fatalf("输出格式缺少 %q：\n%s", want, format)
		}
	}
}

// 技能方法用「写作方法」块包裹：标题、版本、冲突时以结构为准的声明和 <skill> 标签。
func TestBuildPromptSkillWrapper(t *testing.T) {
	step := newTestStep()
	method := stepkit.Method{Source: stepkit.SourceSkill, SkillName: "tvc", SkillTitle: "TVC 导演", Version: 3, Body: "先讲冲突，再讲反转。"}
	prompt, err := stepkit.BuildPrompt(step, method, stepkit.Input{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"【写作方法：TVC 导演 v3】", "方法与结构冲突时以结构为准", "<skill>\n先讲冲突，再讲反转。\n</skill>"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("缺少 %q：\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, step.Builtin) {
		t.Fatal("绑定技能后不应再出现内置正文")
	}
}

// 技能正文按环节的 MethodMax 截断（按字符，不是字节）。
func TestBuildPromptClipsSkillBody(t *testing.T) {
	step := newTestStep()
	step.MethodMax = 20
	body := strings.Repeat("长", 100)
	prompt, err := stepkit.BuildPrompt(step, stepkit.Method{Source: stepkit.SourceSkill, SkillTitle: "长技能", Version: 1, Body: body}, stepkit.Input{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(prompt, "<skill>\n") + len("<skill>\n")
	end := strings.Index(prompt, "\n</skill>")
	if got := utf8.RuneCountInString(prompt[start:end]); got != 20 {
		t.Fatalf("技能正文应截到 20 字，实际 %d", got)
	}
	// 默认上限是 16000
	step.MethodMax = 0
	prompt, err = stepkit.BuildPrompt(step, stepkit.Method{Source: stepkit.SourceSkill, SkillTitle: "长技能", Version: 1, Body: strings.Repeat("长", 20000)}, stepkit.Input{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	start = strings.Index(prompt, "<skill>\n") + len("<skill>\n")
	end = strings.Index(prompt, "\n</skill>")
	if got := utf8.RuneCountInString(prompt[start:end]); got != stepkit.DefaultMethodMax {
		t.Fatalf("默认上限应为 %d，实际 %d", stepkit.DefaultMethodMax, got)
	}
}

// 重写时把上次的硬错误放进输入块，方法块之前，五块顺序不变。
func TestBuildPromptFeedbackStaysInInput(t *testing.T) {
	step := newTestStep()
	prompt, err := stepkit.BuildPrompt(step, stepkit.BuiltinMethod(step), stepkit.Input{}, []string{"标题含禁用词"})
	if err != nil {
		t.Fatal(err)
	}
	fb := strings.Index(prompt, "标题含禁用词")
	if fb < strings.Index(prompt, "## 输入") || fb > strings.Index(prompt, "## 方法") {
		t.Fatalf("反馈应在输入块内：\n%s", prompt)
	}
}
