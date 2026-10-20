package tracereport_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/service/agent/tracereport"
)

func ptr[T any](v T) *T { return &v }

func baseRun() tracereport.Run {
	return tracereport.Run{ID: "r1", Status: "succeeded", PromptVersion: 9, Model: "claude", Steps: 18, SpentCredits: 3,
		Duration: 41 * time.Second, UserText: "把剧本拆成分镜", TraceComplete: true}
}

func chat(seq int, in, out int) tracereport.Span {
	return tracereport.Span{Seq: seq, SpanID: "c" + string(rune('0'+seq)), Kind: tracereport.KindChat, Name: "claude", Status: "ok", Summary: "回复", DurationMS: 3100,
		Usage: &tracereport.Usage{Input: in, Output: out}, Purpose: "chat", Truncated: ptr(false)}
}

func tool(seq int, name, result string, isErr bool) tracereport.Span {
	raw, _ := json.Marshal(result)
	st := "ok"
	if isErr {
		st = "error"
	}
	return tracereport.Span{Seq: seq, SpanID: "t" + string(rune('0'+seq)), Kind: tracereport.KindExecuteTool, Name: name, Status: st, Summary: "摘要",
		DurationMS: 850, Result: raw, IsError: ptr(isErr), Truncated: ptr(false)}
}

// 报告开头：标题、概要行（终态 / 版本 / 模型 / 步数 / token / 积分 / 耗时）和用户原话，格式见契约 8.4。
func TestBuild_Header(t *testing.T) {
	in := tracereport.Input{Run: baseRun(), Spans: []tracereport.Span{chat(1, 31000, 4900), chat(2, 400, 0)}}
	out := tracereport.Build(in)
	for _, want := range []string{
		"# Agent 运行报告 · run r1\n",
		"> 终态 succeeded · prompt v9 · 模型 claude · 18 步 · token 入 31.4k / 出 4.9k · 积分 3 · 耗时 41s\n",
		"> 用户原话：把剧本拆成分镜\n",
		"## 可疑信号\n- 无\n",
		"## 时间线\n| # | span | 名称 | 耗时 | 状态 | 摘要 |\n| --- | --- | --- | --- | --- | --- |\n",
		"## 最终画布概要\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("缺少 %q，全文：\n%s", want, out)
		}
	}
	if strings.Contains(out, "⚠") {
		t.Errorf("完整的已结束运行不应有警告：\n%s", out)
	}
}

func TestBuild_UserTextClippedTo200Runes(t *testing.T) {
	r := baseRun()
	r.UserText = strings.Repeat("字", 250) + "\n第二行"
	out := tracereport.Build(tracereport.Input{Run: r})
	line := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "> 用户原话：") {
			line = l
		}
	}
	if got := strings.Count(line, "字"); got != 200 {
		t.Errorf("用户原话应截到 200 字，得到 %d：%s", got, line)
	}
	if !strings.HasSuffix(line, "…") || strings.Contains(out, "第二行") {
		t.Errorf("超长要加省略号且换行折成一行：%s", line)
	}
}

func TestBuild_Warnings(t *testing.T) {
	r := baseRun()
	r.Status, r.Active = "running", true
	out := tracereport.Build(tracereport.Input{Run: r})
	if !strings.Contains(out, "> ⚠ 运行未结束，轨迹不完整\n") {
		t.Errorf("运行未结束要给出契约里的警告：\n%s", out)
	}
	if !strings.Contains(out, "耗时 未结束") {
		t.Errorf("未结束的运行没有耗时：\n%s", out)
	}
	r = baseRun()
	r.TraceComplete = false
	out = tracereport.Build(tracereport.Input{Run: r})
	if !strings.Contains(out, "> ⚠ 轨迹不完整") || strings.Contains(out, "运行未结束") {
		t.Errorf("已结束但轨迹不完整，只提示轨迹不完整：\n%s", out)
	}
	if !strings.Contains(out, "- 轨迹不完整") {
		t.Errorf("可疑信号里也要列出：\n%s", out)
	}
}

func TestBuild_SuspiciousSignals(t *testing.T) {
	spans := []tracereport.Span{
		tool(1, "canvas_arrange", "组 g1 不存在", true),
		tool(2, "canvas_arrange", "组 g1 不存在", true),
		tool(3, "canvas_arrange", "组 g1 不存在", true),
		tool(4, "canvas_apply_ops", "别的错误", true),
		tool(5, "canvas_apply_ops", "别的错误", true), // 只有 2 次，不报
		{Seq: 6, SpanID: "c6", Kind: tracereport.KindChat, Name: "claude", Status: "ok", Purpose: "summary"},
		{Seq: 7, SpanID: "c7", Kind: tracereport.KindChat, Name: "claude", Status: "ok", Purpose: "chat", Truncated: ptr(true)},
	}
	r := baseRun()
	r.Status = "step_limit"
	out := tracereport.Build(tracereport.Input{Run: r, Spans: spans})
	for _, want := range []string{
		"- 同一条工具报错重复 3 次：canvas_arrange「组 g1 不存在」",
		"- 摘要压缩被触发 1 次",
		"- 达到步数上限",
		"- 1 个 span 的原文被截断",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("缺少 %q：\n%s", want, out)
		}
	}
	if strings.Contains(out, "别的错误」") {
		t.Errorf("重复不足 3 次的报错不该出现：\n%s", out)
	}
}

func TestBuild_Timeline(t *testing.T) {
	spans := []tracereport.Span{
		{Seq: 1, SpanID: "s1", Kind: tracereport.KindSegment, Name: "start", Status: "ok", Summary: "退出码 0", DurationMS: 125000},
		chat(2, 100, 10),
		{Seq: 3, SpanID: "t3", Kind: tracereport.KindExecuteTool, Name: "canvas_apply_ops", Status: "error", Summary: "含|竖线\n和换行" + strings.Repeat("长", 100), DurationMS: 42},
		{Seq: 4, SpanID: "a4", Kind: tracereport.KindApproval, Name: "ask", Status: "pending", Summary: "画幅？"},
	}
	out := tracereport.Build(tracereport.Input{Run: baseRun(), Spans: spans})
	for _, want := range []string{
		"| 1 | segment | start | 2m05s | ok | 退出码 0 |",
		"| 2 | chat | claude | 3.1s | ok | 回复 |",
		"| 3 | execute_tool | canvas_apply_ops | 42ms | error | 含\\|竖线 和换行",
		"| 4 | approval | ask | - | pending | 画幅？ |",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("时间线缺少 %q：\n%s", want, out)
		}
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "| 3 |") && strings.Count(l, "长") > 80 {
			t.Errorf("摘要应截断到 80 字: %s", l)
		}
	}
}

const canvasAfter = `{
  "nodes": [
    {"id":"g1","type":"group","position":{"x":0,"y":0},"width":400,"height":800,"data":{"label":"镜头1"}},
    {"id":"n1","type":"canvas","parentId":"g1","position":{"x":20,"y":72},"data":{"kind":"image","label":"关键帧","params":{"aspect_ratio":"9:16","prompt":"雨夜"}}},
    {"id":"n2","type":"canvas","parentId":"g1","position":{"x":20,"y":700},"data":{"kind":"video","label":"视频"}},
    {"id":"n3","type":"canvas","position":{"x":900,"y":0},"data":{"kind":"script","label":"剧本","text":"一段剧本"}}
  ],
  "edges": [{"id":"e1","source":"n1","target":"n2"}]
}`

// 最终画布概要：组到成员的树，每个节点写种类、尺寸（按前后端共用的规则）和是否有提示词。
func TestBuild_CanvasSummary(t *testing.T) {
	out := tracereport.Build(tracereport.Input{Run: baseRun(), CanvasAfter: json.RawMessage(canvasAfter)})
	for _, want := range []string{
		"- 组 `g1`「镜头1」 400×800 · 2 个成员\n",
		"  - 图片 `n1`「关键帧」 324×576 · 提示词：有\n",
		"  - 视频 `n2`「视频」 576×324 · 提示词：无\n",
		"- 未进组的节点\n",
		"  - 文本 `n3`「剧本」 576×324 · 提示词：有\n",
		"共 4 个节点（1 个组）、1 条连线",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("画布概要缺少 %q：\n%s", want, out)
		}
	}
}

func TestBuild_CanvasSummaryMissingOrBroken(t *testing.T) {
	out := tracereport.Build(tracereport.Input{Run: baseRun()})
	if !strings.Contains(out, "没有画布快照") {
		t.Errorf("没有快照要说明：\n%s", out)
	}
	out = tracereport.Build(tracereport.Input{Run: baseRun(), CanvasAfter: json.RawMessage(`[1,2]`)})
	if !strings.Contains(out, "画布快照无法解析") {
		t.Errorf("快照损坏要说明而不是 panic：\n%s", out)
	}
}

// 输出必须稳定：同样的输入逐字节相同（AI 读报告前后对比时不能有随机顺序）。
func TestBuild_Deterministic(t *testing.T) {
	in := tracereport.Input{Run: baseRun(), Spans: []tracereport.Span{chat(1, 1, 1), tool(2, "a", "x", true)}, CanvasAfter: json.RawMessage(canvasAfter)}
	if tracereport.Build(in) != tracereport.Build(in) {
		t.Fatal("同样的输入应得到同样的输出")
	}
}

// ---- 进程崩溃时把 stderr 里的真正原因摘出来 ----

const nodeCrashStderr = `node:internal/modules/esm/resolve:873
  throw new ERR_MODULE_NOT_FOUND(packageName, fileURLToPath(base), null);
        ^

Error [ERR_MODULE_NOT_FOUND]: Cannot find package '@earendil-works/pi-codemode' imported from /app/agent/src/agent-tools/codemode.mjs
    at packageResolve (node:internal/modules/esm/resolve:873:9)
    at moduleResolve (node:internal/modules/esm/resolve:946:18) {
  code: 'ERR_MODULE_NOT_FOUND'
}

Node.js v20.11.0
`

func segment(seq int, status, stderr string) tracereport.Span {
	return tracereport.Span{Seq: seq, SpanID: "s" + string(rune('0'+seq)), Kind: tracereport.KindSegment, Name: "start", Status: status,
		Summary: "start", DurationMS: 900, ExitCode: ptr(1), StderrTail: stderr}
}

func signalsOf(out string) string {
	a := strings.Index(out, "## 可疑信号\n")
	b := strings.Index(out, "## 时间线\n")
	return out[a:b]
}

func TestBuild_SegmentCrashSurfacesStderrError(t *testing.T) {
	const want = "Cannot find package '@earendil-works/pi-codemode'"

	t.Run("缺 npm 包崩溃：可疑信号和时间线都直接写出原因", func(t *testing.T) {
		r := baseRun()
		r.Status = "interrupted"
		out := tracereport.Build(tracereport.Input{Run: r, Spans: []tracereport.Span{segment(1, "aborted", nodeCrashStderr)}})
		sig := signalsOf(out)
		if !strings.Contains(sig, want) || !strings.Contains(sig, "退出码 1") || strings.Contains(sig, "- 无") {
			t.Errorf("可疑信号里应有崩溃原因：\n%s", sig)
		}
		timeline := out[strings.Index(out, "## 时间线\n"):]
		if !strings.Contains(timeline, want) {
			t.Errorf("时间线摘要里也应带上这一句：\n%s", timeline)
		}
		if strings.Contains(sig, "Node.js v20") || strings.Contains(sig, "at packageResolve") {
			t.Errorf("只摘错误那一行，不要带堆栈和版本号：\n%s", sig)
		}
	})

	t.Run("状态是 error 也一样", func(t *testing.T) {
		out := tracereport.Build(tracereport.Input{Run: baseRun(), Spans: []tracereport.Span{segment(1, "error", nodeCrashStderr)}})
		if !strings.Contains(signalsOf(out), want) {
			t.Errorf("error 状态的片段也要摘出原因：\n%s", out)
		}
	})

	t.Run("正常结束的片段、stderr 为空或只有空白：不报", func(t *testing.T) {
		for name, sp := range map[string]tracereport.Span{
			"ok 带 stderr": segment(1, "ok", nodeCrashStderr),
			"stderr 为空":   segment(1, "aborted", ""),
			"stderr 空白":   segment(1, "aborted", " \n\t\n"),
		} {
			out := tracereport.Build(tracereport.Input{Run: baseRun(), Spans: []tracereport.Span{sp}})
			if !strings.Contains(signalsOf(out), "- 无\n") {
				t.Errorf("%s：不该有可疑信号：\n%s", name, signalsOf(out))
			}
		}
	})

	t.Run("摘取规则：优先最后一条 Error 开头的行，其次含 ERR_ 的最后一行，再不行取最后一个非空行", func(t *testing.T) {
		cases := map[string]struct{ stderr, want string }{
			"Error 开头":  {"warn: x\nError: boom one\n  at a\nError: boom two\n  at b\n", "Error: boom two"},
			"TypeError": {"TypeError: x is not a function\n    at y\n", "TypeError: x is not a function"},
			"只有 ERR_":   {"info\nfailed with ERR_OUT_OF_MEMORY here\nbye\n", "failed with ERR_OUT_OF_MEMORY here"},
			"取最后一行":     {"starting\n\nkilled by signal\n\n", "killed by signal"},
		}
		for name, c := range cases {
			out := tracereport.Build(tracereport.Input{Run: baseRun(), Spans: []tracereport.Span{segment(1, "aborted", c.stderr)}})
			if !strings.Contains(signalsOf(out), c.want) {
				t.Errorf("%s：应摘出 %q：\n%s", name, c.want, signalsOf(out))
			}
		}
	})

	t.Run("超长截到 200 字；含竖线不撑破时间线表格", func(t *testing.T) {
		long := "Error: " + strings.Repeat("长", 300) + "|尾巴"
		out := tracereport.Build(tracereport.Input{Run: baseRun(), Spans: []tracereport.Span{segment(1, "aborted", long)}})
		if got := strings.Count(signalsOf(out), "长"); got != 200-len([]rune("Error: ")) {
			t.Errorf("整句应截到 200 字（含 Error: 前缀），实际“长”有 %d 个", got)
		}
		if strings.Contains(out, "|尾巴") {
			t.Error("超过 200 字的部分不应出现")
		}
		short := tracereport.Build(tracereport.Input{Run: baseRun(), Spans: []tracereport.Span{segment(1, "aborted", "Error: a|b")}})
		if !strings.Contains(short, `Error: a\|b`) {
			t.Errorf("时间线里的竖线要转义：\n%s", short)
		}
	})

	t.Run("多个片段各出一条", func(t *testing.T) {
		out := tracereport.Build(tracereport.Input{Run: baseRun(), Spans: []tracereport.Span{segment(1, "aborted", "Error: one\n"), segment(2, "aborted", "Error: two\n")}})
		sig := signalsOf(out)
		if !strings.Contains(sig, "Error: one") || !strings.Contains(sig, "Error: two") {
			t.Errorf("两个片段都要有：\n%s", sig)
		}
	})
}
