import test from "node:test";
import assert from "node:assert/strict";
import { buildTraceFromEvents, buildFallbackReport, renderAssertionBlock, applyAssertionBlock, summarizeResults, canvasOutline } from "../../harness/report.mjs";
import { deriveRun } from "../../harness/events.mjs";
import { ev, approval, goodStoryboard, group, node, canvas } from "./helpers.mjs";

const events = [
  ev(1, "message.user", { text: "把剧本拆成 3 个镜头" }),
  ev(2, "run.status", { status: "running" }),
  ev(3, "tool.start", { id: "t1", name: "canvas_get_state" }),
  ev(4, "tool.end", { id: "t1", name: "canvas_get_state", is_error: false, summary: "1 个节点" }),
  ev(5, "tool.start", { id: "t2", name: "canvas_arrange" }),
  ev(6, "tool.end", { id: "t2", name: "canvas_arrange", is_error: true, summary: "节点 g1 不存在或是组" }),
  ev(7, "tool.end", { id: "t3", name: "canvas_arrange", is_error: true, summary: "节点 g1 不存在或是组" }),
  ev(8, "tool.end", { id: "t4", name: "canvas_arrange", is_error: true, summary: "节点 g1 不存在或是组" }),
  ev(9, "message.done", { text: "好的", tool_calls: [{ id: "t2", name: "canvas_arrange" }], credits: 1 }),
  ev(10, "approval.created", approval("a1", "ask", { question: "画幅用哪个？", kind: "choice", options: ["16:9", "9:16"] })),
  ev(11, "approval.decided", approval("a1", "ask", { question: "画幅用哪个？", kind: "choice" }, { status: "approved", decision: { decision: "approve", answer: "9:16" } })),
  ev(12, "run.status", { status: "succeeded" }),
];

test("无轨迹时用事件拼简版 trace.jsonl：第一行 system，之后一行一个 span，按 seq 升序", () => {
  const { spans, jsonl } = buildTraceFromEvents(events, { runId: "r1" });
  const lines = jsonl.trim().split("\n").map((l) => JSON.parse(l));
  assert.equal(lines[0].kind, "system");
  assert.equal(lines[0].seq, 0);
  assert.match(lines[0].note, /无轨迹/);
  assert.equal(lines.length, spans.length + 1);
  assert.deepEqual(lines.slice(1).map((s) => s.seq), lines.slice(1).map((_, i) => i + 1));
  const kinds = new Set(lines.slice(1).map((s) => s.kind));
  assert.ok(kinds.has("execute_tool") && kinds.has("approval") && kinds.has("chat"));
  for (const s of lines.slice(1)) {
    for (const k of ["seq", "span_id", "kind", "name", "status", "summary"]) assert.ok(k in s, `span 缺 ${k}`);
    assert.ok(["ok", "error", "denied", "aborted", "pending"].includes(s.status));
    assert.ok(!("request" in s) && !("response" in s), "简版没有请求和响应原文");
  }
});

test("简版 span：工具报错标 error，审批带 approval_kind / ask_kind / question / decision", () => {
  const { spans } = buildTraceFromEvents(events, { runId: "r1" });
  const tools = spans.filter((s) => s.kind === "execute_tool");
  assert.equal(tools.length, 4);
  assert.equal(tools.filter((s) => s.status === "error").length, 3);
  const ap = spans.find((s) => s.kind === "approval");
  assert.equal(ap.approval_kind, "ask");
  assert.equal(ap.ask_kind, "choice");
  assert.equal(ap.question, "画幅用哪个？");
  assert.equal(ap.status, "ok");
  assert.equal(ap.decision, "approve");
});

test("被拒绝的审批在 span 里是 denied", () => {
  const evs = [
    ev(1, "approval.created", approval("g1", "generate", { reason: "出图" })),
    ev(2, "approval.decided", approval("g1", "generate", {}, { status: "rejected", decision: { decision: "reject" } })),
  ];
  const { spans } = buildTraceFromEvents(evs, { runId: "r1" });
  assert.equal(spans[0].status, "denied");
});

test("简版报告：标题、概要、无轨迹提示、可疑信号、时间线、画布概要齐全，且不含断言", () => {
  const run = deriveRun(events, "r1");
  const { spans } = buildTraceFromEvents(events, { runId: "r1" });
  const md = buildFallbackReport({ runId: "r1", message: "把剧本拆成 3 个镜头", run, spans, canvas: goodStoryboard(), noTraceReason: "缺管理员凭据" });
  assert.match(md, /^# Agent 运行报告 · run r1/);
  assert.match(md, /终态 succeeded/);
  assert.match(md, /用户原话：把剧本拆成 3 个镜头/);
  assert.match(md, /无轨迹（缺管理员凭据）/);
  assert.match(md, /## 可疑信号/);
  assert.match(md, /同一条工具报错重复 3 次：canvas_arrange「节点 g1 不存在或是组」/);
  assert.match(md, /## 时间线\n\| # \| span \| 名称 \| 耗时 \| 状态 \| 摘要 \|/);
  assert.match(md, /## 最终画布概要/);
  assert.doesNotMatch(md, /断言结论/);
});

test("简版报告的可疑信号：未预写的提问、步数上限、无报错时写「无」", () => {
  const md = buildFallbackReport({ runId: "r2", message: "m", run: { status: "step_limit", steps: 40, spent_credits: 0, tool_errors: 0 }, spans: [], canvas: canvas([]), noTraceReason: "x", unscriptedQuestions: ["画幅用哪个？"] });
  assert.match(md, /达到步数上限/);
  assert.match(md, /未预写的提问：画幅用哪个？/);
  const clean = buildFallbackReport({ runId: "r3", message: "m", run: { status: "succeeded", steps: 1, spent_credits: 0, tool_errors: 0 }, spans: [], canvas: canvas([]), noTraceReason: "" });
  assert.match(clean, /## 可疑信号\n- 无\n/);
  assert.doesNotMatch(clean, /无轨迹/, "有轨迹时不出现无轨迹提示");
});

test("画布概要：组 → 成员树，每个节点写种类、尺寸、是否有提示词", () => {
  const text = canvasOutline(canvas([group("g1", 0, 0, 420, 696), node("a", "image", 48, 72, { parent: "g1", aspect: "9:16", prompt: "夜景" }), node("b", "script", 0, 900)]));
  assert.match(text, /g1.*组.*420×696/);
  assert.match(text, /a.*image.*324×576.*有提示词/);
  assert.match(text, /b.*script.*576×324.*无提示词/);
});

// ---------- 断言段 ----------

const passed = { type: "no_overlap", ok: true, expected: "没有重叠", got: "0 处相交", evidence: [] };
const failed = { type: "members_inside_group", ok: false, expected: "成员都在组框内", got: "1 个成员越界", evidence: ["g1n2(324×576) 底边超出组框 204px"] };

test("summarizeResults 数通过的条数", () => {
  assert.deepEqual(summarizeResults([passed, failed]), { passed: 1, total: 2, ok: false });
  assert.deepEqual(summarizeResults([passed]), { passed: 1, total: 1, ok: true });
});

test("断言段：结论行 + 失败断言（期望、实际、证据）", () => {
  const block = renderAssertionBlock({ results: [passed, failed], notes: [] });
  assert.match(block, /## 断言结论\n\n失败（1\/2 条断言通过）/);
  assert.match(block, /## 失败断言/);
  assert.match(block, /members_inside_group/);
  assert.match(block, /期望：成员都在组框内/);
  assert.match(block, /实际：1 个成员越界/);
  assert.match(block, /g1n2\(324×576\) 底边超出组框 204px/);
  assert.doesNotMatch(block, /### no_overlap/, "通过的不进失败段");
});

test("全部通过时失败段写「无」", () => {
  const block = renderAssertionBlock({ results: [passed], notes: [] });
  assert.match(block, /通过（1\/1 条断言通过）/);
  assert.match(block, /## 失败断言\n\n无/);
});

test("运行备注（画布没删掉、无轨迹）写进断言段", () => {
  const block = renderAssertionBlock({ results: [passed], notes: ["测试画布删除失败，请手动清理：canvas=4821"] });
  assert.match(block, /## 运行备注/);
  assert.match(block, /canvas=4821/);
});

test("未预写的提问：失败断言里带建议补的 answers 条目", () => {
  const f = { type: "harness.unscripted_question", ok: false, expected: "所有审批和提问都有预写的应答", got: "未预写的提问：画幅用哪个？", evidence: ["建议在用例 answers 里补："], suggestion: { match: { kind: "ask", contains: "画幅" }, answer: { answer: "16:9" } } };
  const block = renderAssertionBlock({ results: [f], notes: [] });
  assert.match(block, /未预写的提问：画幅用哪个？/);
  assert.match(block, /"contains": "画幅"/);
});

test("插入位置：标题和紧跟标题的引用概要之后，其余内容之前", () => {
  const md = "# Agent 运行报告 · run 9\n> 终态 succeeded\n> 用户原话：hi\n\n## 可疑信号\n- 无\n";
  const out = applyAssertionBlock(md, renderAssertionBlock({ results: [passed], notes: [] }));
  const lines = out.split("\n");
  assert.equal(lines[0], "# Agent 运行报告 · run 9");
  assert.equal(lines[1], "> 终态 succeeded");
  assert.equal(lines[2], "> 用户原话：hi");
  assert.ok(out.indexOf("## 断言结论") < out.indexOf("## 可疑信号"));
  assert.ok(out.indexOf("## 断言结论") > out.indexOf("用户原话"));
});

test("重复插入是幂等的：旧的断言段被替换，不叠加", () => {
  const md = "# Agent 运行报告 · run 9\n> 终态 succeeded\n\n## 时间线\n";
  const once = applyAssertionBlock(md, renderAssertionBlock({ results: [failed], notes: [] }));
  const twice = applyAssertionBlock(once, renderAssertionBlock({ results: [passed], notes: [] }));
  assert.equal((twice.match(/## 断言结论/g) ?? []).length, 1);
  assert.match(twice, /通过（1\/1/);
  assert.doesNotMatch(twice, /1\/2/);
  assert.ok(twice.endsWith("## 时间线\n"));
});

test("没有标题行的报告也能插（放在最前面）", () => {
  const out = applyAssertionBlock("随便什么内容\n", renderAssertionBlock({ results: [passed], notes: [] }));
  assert.ok(out.startsWith("<!-- harness:assertions:start -->") || out.includes("## 断言结论"));
  assert.match(out, /随便什么内容/);
});
