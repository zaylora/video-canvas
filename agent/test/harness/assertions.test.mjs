import test from "node:test";
import assert from "node:assert/strict";
import { runAssertions, runAssertion, validateAssertion, ASSERTION_TYPES } from "../../harness/assertions.mjs";
import { deriveRun, isTerminalStatus, RUN_STATUSES } from "../../harness/events.mjs";
import { group, node, edge, canvas, goodStoryboard, ev, approval } from "./helpers.mjs";

// 断言引擎是纯函数：给画布和从事件里推出的运行信息，返回 {type, ok, got, evidence}。
// 每个断言都有正反两个用例；重叠、越界这类要把节点 id 和矩形坐标放进证据。

const ctxOf = (c, extra = {}) => ({ canvas: c, run: { status: "succeeded", steps: 10, spent_credits: 0, tool_errors: 0, error: "" }, approvals: [], taskOutputs: {}, ...extra });
const one = (a, c, extra) => runAssertion(a, ctxOf(c, extra));

test("支持的断言类型和设计文档 6.B.4 一致", () => {
  assert.deepEqual([...ASSERTION_TYPES].sort(), ["asked", "count", "edge_exists", "every_group_has", "max_credits", "max_steps", "max_tool_errors", "members_inside_group", "no_overlap", "not_asked", "nodes_have_output", "nodes_have_prompt", "run_status"].sort());
});

test("每条结果都有 type / ok / got / evidence", () => {
  const r = one({ type: "no_overlap" }, goodStoryboard());
  assert.equal(r.type, "no_overlap");
  assert.equal(typeof r.ok, "boolean");
  assert.equal(typeof r.got, "string");
  assert.ok(Array.isArray(r.evidence));
});

// ---------- no_overlap ----------

test("no_overlap 正例：合格的三镜头分镜没有重叠", () => {
  const r = one({ type: "no_overlap" }, goodStoryboard());
  assert.equal(r.ok, true, r.got + r.evidence.join("\n"));
});

test("no_overlap 正例：边贴边不算相交", () => {
  const c = canvas([group("a", 0, 0, 300, 200), group("b", 300, 0, 300, 200), node("n", "script", 0, 200)]);
  assert.equal(one({ type: "no_overlap" }, c).ok, true);
});

test("no_overlap 反例：组压组（g2 落在 g1 的范围里），证据给出两个矩形坐标", () => {
  const c = canvas([group("g1", 696, 0, 2064, 444), group("g2", 1016, 0, 2064, 444)]);
  const r = one({ type: "no_overlap" }, c);
  assert.equal(r.ok, false);
  const text = r.evidence.join("\n");
  assert.match(text, /g1\(696,0,2064×444\)/);
  assert.match(text, /g2\(1016,0,2064×444\)/);
  assert.match(r.got, /1 处/);
});

test("no_overlap 反例：同组成员互压（两个 9:16 竖图只隔 200，比 324 的宽度小）", () => {
  const c = canvas([
    group("g1", 0, 0, 1000, 700),
    node("a", "image", 48, 72, { parent: "g1", aspect: "9:16" }),
    node("b", "image", 248, 72, { parent: "g1", aspect: "9:16" }),
  ]);
  const r = one({ type: "no_overlap" }, c);
  assert.equal(r.ok, false);
  assert.match(r.evidence.join("\n"), /a\(48,72,324×576\)/);
  assert.match(r.evidence.join("\n"), /b\(248,72,324×576\)/);
});

test("no_overlap 用真实尺寸：同样间距 400，9:16 的竖图不压，16:9 的横图（576 宽）就压了", () => {
  const mk = (aspect) =>
    canvas([group("g", 0, 0, 2000, 800), node("a", "image", 48, 72, { parent: "g", aspect }), node("b", "image", 448, 72, { parent: "g", aspect })]);
  assert.equal(one({ type: "no_overlap" }, mk("9:16")).ok, true);
  assert.equal(one({ type: "no_overlap" }, mk("16:9")).ok, false);
});

test("no_overlap 反例：不在组里的散落节点压在组上", () => {
  const c = canvas([group("g1", 0, 0, 800, 500), node("lone", "script", 400, 100)]);
  const r = one({ type: "no_overlap" }, c);
  assert.equal(r.ok, false);
  assert.match(r.evidence.join("\n"), /lone\(400,100,576×324\)/);
});

test("no_overlap：不同组的成员不互相比较（只比顶层和同组）", () => {
  // 两个组上下排开；各自成员的相对坐标相同，但绝对位置不同，不会相交
  const c = canvas([
    group("g1", 0, 0, 700, 500),
    group("g2", 0, 600, 700, 500),
    node("a", "script", 48, 72, { parent: "g1" }),
    node("b", "script", 48, 72, { parent: "g2" }),
  ]);
  assert.equal(one({ type: "no_overlap" }, c).ok, true);
});

// ---------- members_inside_group ----------

test("members_inside_group 正例：组框刚好包住竖图成员", () => {
  const r = one({ type: "members_inside_group" }, goodStoryboard());
  assert.equal(r.ok, true, r.evidence.join("\n"));
});

test("members_inside_group 反例：9:16 竖图伸出组框 204px（后端按 324 高排版的真实 bug）", () => {
  const c = canvas([group("g1", 0, 0, 420, 444), node("g1n2", "image", 48, 72, { parent: "g1", aspect: "9:16" })]);
  const r = one({ type: "members_inside_group" }, c);
  assert.equal(r.ok, false);
  const text = r.evidence.join("\n");
  assert.match(text, /g1n2\(324×576\)/);
  assert.match(text, /底边超出组框 204px/);
});

test("members_inside_group 反例：左边和右边越界都写出来，同时说明越界的量", () => {
  const c = canvas([group("g", 0, 0, 526, 700), node("a", "script", -30, 72, { parent: "g" })]);
  const r = one({ type: "members_inside_group" }, c);
  assert.equal(r.ok, false);
  assert.match(r.evidence.join("\n"), /左边超出组框 30px/);
  assert.match(r.evidence.join("\n"), /右边超出组框 20px/);
});

test("members_inside_group 反例：parentId 指向不存在的组", () => {
  const c = canvas([node("a", "script", 0, 0, { parent: "ghost" })]);
  const r = one({ type: "members_inside_group" }, c);
  assert.equal(r.ok, false);
  assert.match(r.evidence.join("\n"), /a.*ghost/);
});

// ---------- count ----------

test("count 正例：3 个组落在 [3,4]", () => {
  assert.equal(one({ type: "count", of: "group", min: 3, max: 4 }, goodStoryboard()).ok, true);
});

test("count 反例：组太少 / 太多，got 写实际数量", () => {
  const few = one({ type: "count", of: "group", min: 4 }, goodStoryboard());
  assert.equal(few.ok, false);
  assert.match(few.got, /3/);
  const many = one({ type: "count", of: "group", max: 2 }, goodStoryboard());
  assert.equal(many.ok, false);
});

test("count 按种类数节点，node 表示全部普通节点", () => {
  assert.equal(one({ type: "count", of: "image", min: 3, max: 3 }, goodStoryboard()).ok, true);
  assert.equal(one({ type: "count", of: "node", min: 9, max: 9 }, goodStoryboard()).ok, true);
  assert.equal(one({ type: "count", of: "audio", min: 1 }, goodStoryboard()).ok, false);
});

// ---------- every_group_has ----------

test("every_group_has 正例", () => {
  assert.equal(one({ type: "every_group_has", kinds: ["script", "image", "video"] }, goodStoryboard()).ok, true);
});

test("every_group_has 反例：证据写出缺了哪种的组", () => {
  const c = canvas([
    group("g1", 0, 0, 800, 600),
    node("a", "script", 48, 72, { parent: "g1" }),
    node("b", "image", 48, 400, { parent: "g1" }),
    group("g2", 0, 700, 800, 600),
    node("c", "script", 48, 72, { parent: "g2" }),
  ]);
  const r = one({ type: "every_group_has", kinds: ["script", "image"] }, c);
  assert.equal(r.ok, false);
  assert.match(r.evidence.join("\n"), /g2.*缺 image/);
  assert.doesNotMatch(r.evidence.join("\n"), /g1/);
});

test("every_group_has 反例：画布里一个组都没有，不能空转通过", () => {
  const r = one({ type: "every_group_has", kinds: ["script"] }, canvas([node("a", "script", 0, 0)]));
  assert.equal(r.ok, false);
  assert.match(r.got, /没有组/);
});

// ---------- edge_exists ----------

test("edge_exists 正例：全局存在 image → video", () => {
  assert.equal(one({ type: "edge_exists", from: "image", to: "video" }, goodStoryboard()).ok, true);
});

test("edge_exists 反例：没有 video → image 的线", () => {
  assert.equal(one({ type: "edge_exists", from: "video", to: "image" }, goodStoryboard()).ok, false);
});

test("edge_exists scope=group 正例：每个组内都有 image → video", () => {
  assert.equal(one({ type: "edge_exists", from: "image", to: "video", scope: "group" }, goodStoryboard()).ok, true);
});

test("edge_exists scope=group 反例：g2 缺线；跨组的线不算数", () => {
  const c = goodStoryboard();
  // 去掉 g2 内的 image→video，换成 g1 的图连 g2 的视频
  c.edges = c.edges.filter((e) => e.id !== "g2e2");
  c.edges.push(edge("cross", "g1i", "g2v"));
  const r = one({ type: "edge_exists", from: "image", to: "video", scope: "group" }, c);
  assert.equal(r.ok, false);
  assert.match(r.evidence.join("\n"), /g2/);
  assert.doesNotMatch(r.evidence.join("\n"), /g1\b/);
});

test("edge_exists 的 from/to 也可以直接写节点 id", () => {
  const c = canvas([node("a", "script", 0, 0), node("b", "image", 0, 400)], [edge("e", "a", "b")]);
  assert.equal(one({ type: "edge_exists", from: "a", to: "b" }, c).ok, true);
  assert.equal(one({ type: "edge_exists", from: "b", to: "a" }, c).ok, false);
});

// ---------- nodes_have_prompt / nodes_have_output ----------

test("nodes_have_prompt 正例：data.prompt 或 params.prompt 非空", () => {
  assert.equal(one({ type: "nodes_have_prompt", kinds: ["image", "video"] }, goodStoryboard()).ok, true);
  const c = canvas([node("a", "image", 0, 0, { data: { params: { prompt: "夜景" } } })]);
  assert.equal(one({ type: "nodes_have_prompt", kinds: ["image"] }, c).ok, true);
});

test("nodes_have_prompt 反例：空白提示词的节点写进证据", () => {
  const c = canvas([node("a", "image", 0, 0, { prompt: "   " }), node("b", "image", 0, 400, { prompt: "好" }), node("c", "video", 0, 800)]);
  const r = one({ type: "nodes_have_prompt", kinds: ["image", "video"] }, c);
  assert.equal(r.ok, false);
  assert.match(r.evidence.join("\n"), /a/);
  assert.match(r.evidence.join("\n"), /c/);
  assert.doesNotMatch(r.evidence.join("\n"), /^b/m);
});

test("nodes_have_prompt 反例：指定的种类一个节点都没有，不能空转通过", () => {
  const r = one({ type: "nodes_have_prompt", kinds: ["video"] }, canvas([node("a", "image", 0, 0, { prompt: "x" })]));
  assert.equal(r.ok, false);
  assert.match(r.got, /没有 video/);
});

test("nodes_have_output 正例：outputs、src 或已完成任务的产物都算", () => {
  const c = canvas([node("a", "image", 0, 0, { outputs: 1 }), node("b", "image", 0, 400, { src: "http://x/y.png" }), node("c", "video", 0, 800)]);
  const r = one({ type: "nodes_have_output", kinds: ["image", "video"] }, c, { taskOutputs: { c: 1 } });
  assert.equal(r.ok, true, r.evidence.join("\n"));
});

test("nodes_have_output 反例：还没有产物的节点写进证据", () => {
  const c = canvas([node("a", "image", 0, 0, { outputs: 1 }), node("b", "image", 0, 400)]);
  const r = one({ type: "nodes_have_output", kinds: ["image"] }, c);
  assert.equal(r.ok, false);
  assert.match(r.evidence.join("\n"), /b/);
});

// ---------- asked / not_asked ----------

const askedApprovals = [
  { id: "a1", kind: "ask", ask_kind: "choice", model_kind: "", question: "画幅用哪个？", text: "画幅用哪个？" },
  { id: "a2", kind: "generate", ask_kind: "", model_kind: "", question: "", text: "出图" },
];

test("asked 正例：出现过画幅的提问", () => {
  const r = one({ type: "asked", kind: "ask", contains: "画幅" }, canvas([]), { approvals: askedApprovals });
  assert.equal(r.ok, true);
  assert.match(r.evidence.join("\n"), /画幅用哪个/);
});

test("asked 反例：没出现过选模型的提问", () => {
  assert.equal(one({ type: "asked", kind: "ask", ask_kind: "model" }, canvas([]), { approvals: askedApprovals }).ok, false);
});

test("not_asked 正例：没问过删除审批", () => {
  assert.equal(one({ type: "not_asked", kind: "delete" }, canvas([]), { approvals: askedApprovals }).ok, true);
});

test("not_asked 反例：问了不该问的，证据是提问原文", () => {
  const r = one({ type: "not_asked", kind: "ask" }, canvas([]), { approvals: askedApprovals });
  assert.equal(r.ok, false);
  assert.match(r.evidence.join("\n"), /画幅用哪个/);
});

// ---------- 过程指标 ----------

test("max_tool_errors 正反例", () => {
  assert.equal(one({ type: "max_tool_errors", max: 2 }, canvas([]), { run: { status: "succeeded", steps: 1, spent_credits: 0, tool_errors: 2 } }).ok, true);
  const bad = one({ type: "max_tool_errors", max: 2 }, canvas([]), { run: { status: "succeeded", steps: 1, spent_credits: 0, tool_errors: 3 } });
  assert.equal(bad.ok, false);
  assert.match(bad.got, /3/);
});

test("max_steps 正反例", () => {
  assert.equal(one({ type: "max_steps", max: 40 }, canvas([]), { run: { status: "succeeded", steps: 40, spent_credits: 0, tool_errors: 0 } }).ok, true);
  assert.equal(one({ type: "max_steps", max: 40 }, canvas([]), { run: { status: "succeeded", steps: 41, spent_credits: 0, tool_errors: 0 } }).ok, false);
});

test("max_credits 正反例", () => {
  assert.equal(one({ type: "max_credits", max: 10 }, canvas([]), { run: { status: "succeeded", steps: 1, spent_credits: 8, tool_errors: 0 } }).ok, true);
  assert.equal(one({ type: "max_credits", max: 10 }, canvas([]), { run: { status: "succeeded", steps: 1, spent_credits: 12, tool_errors: 0 } }).ok, false);
});

// ---------- run_status ----------

test("run_status 正反例：in 里写真实的运行状态常量", () => {
  assert.equal(one({ type: "run_status", in: ["succeeded"] }, canvas([])).ok, true);
  assert.equal(one({ type: "run_status", in: ["succeeded", "step_limit"] }, canvas([]), { run: { status: "step_limit", steps: 40, spent_credits: 0, tool_errors: 0 } }).ok, true);
  const bad = one({ type: "run_status", in: ["succeeded"] }, canvas([]), { run: { status: "failed", steps: 1, spent_credits: 0, tool_errors: 0, error: "模型超时" } });
  assert.equal(bad.ok, false);
  assert.match(bad.got, /failed/);
  assert.match(bad.evidence.join("\n"), /模型超时/);
});

test("run_status 校验：done / error 不是真实状态，提示改成 succeeded / failed", () => {
  const e1 = validateAssertion({ type: "run_status", in: ["done"] }).join("\n");
  assert.match(e1, /done/);
  assert.match(e1, /succeeded/);
  assert.match(validateAssertion({ type: "run_status", in: ["error"] }).join("\n"), /failed/);
  assert.deepEqual(validateAssertion({ type: "run_status", in: ["succeeded", "failed", "canceled", "budget_exhausted", "step_limit", "timeout", "interrupted", "expired"] }), []);
});

// ---------- 引擎本身 ----------

test("runAssertions 按顺序返回全部结果，未知类型判失败而不是抛错", () => {
  const rs = runAssertions([{ type: "no_overlap" }, { type: "nonsense" }], ctxOf(goodStoryboard()));
  assert.equal(rs.length, 2);
  assert.equal(rs[0].ok, true);
  assert.equal(rs[1].ok, false);
  assert.match(rs[1].got, /未知断言类型/);
});

test("validateAssertion：缺参数、类型不对都能提前发现", () => {
  assert.deepEqual(validateAssertion({ type: "no_overlap" }), []);
  assert.ok(validateAssertion({ type: "count" }).length > 0);
  assert.ok(validateAssertion({ type: "count", of: "group" }).length > 0, "min 和 max 至少一个");
  assert.ok(validateAssertion({ type: "every_group_has", kinds: "image" }).length > 0);
  assert.ok(validateAssertion({ type: "edge_exists", from: "image" }).length > 0);
  assert.ok(validateAssertion({ type: "max_steps" }).length > 0);
  assert.ok(validateAssertion({ type: "run_status", in: [] }).length > 0);
  assert.ok(validateAssertion({ type: "???" }).length > 0);
  assert.ok(validateAssertion({ type: "edge_exists", from: "a", to: "b", scope: "galaxy" }).length > 0);
});

// ---------- 从事件推运行信息 ----------

test("deriveRun：状态、步数（不含嵌套调用）、工具报错、积分、审批", () => {
  const events = [
    ev(1, "message.user", { text: "hi" }),
    ev(2, "run.status", { status: "running" }),
    ev(3, "tool.start", { id: "t1", name: "canvas_get_state" }),
    ev(4, "tool.end", { id: "t1", name: "canvas_get_state", is_error: false, summary: "1 个节点" }),
    ev(5, "tool.end", { id: "t2", name: "canvas_apply_ops", is_error: true, summary: "boom" }),
    ev(6, "tool.end", { id: "t3", name: "canvas_apply_ops", is_error: true, parent_id: "t2", summary: "嵌套" }),
    ev(7, "approval.created", approval("a1", "ask", { question: "画幅？", kind: "choice", options: ["16:9", "9:16"] })),
    ev(8, "run.status", { status: "waiting_input" }),
    ev(9, "run.usage", { spent_credits: 4, budget_credits: 50 }),
    ev(10, "run.status", { status: "failed", error: "渠道报错" }),
    ev(11, "tool.end", { id: "x", name: "other_run", is_error: true }, "r2"), // 别的运行的事件不计
  ];
  const run = deriveRun(events, "r1");
  assert.equal(run.status, "failed");
  assert.equal(run.error, "渠道报错");
  assert.equal(run.steps, 2);
  assert.equal(run.tool_errors, 2);
  assert.equal(run.spent_credits, 4);
  assert.equal(run.approvals.length, 1);
  assert.deepEqual({ id: run.approvals[0].id, kind: run.approvals[0].kind, ask_kind: run.approvals[0].ask_kind, question: run.approvals[0].question }, { id: "a1", kind: "ask", ask_kind: "choice", question: "画幅？" });
});

test("deriveRun：没有任何 run.status 时是 queued", () => {
  assert.equal(deriveRun([], "r1").status, "queued");
});

test("终态 = 不在活跃状态（queued / running / waiting_approval / waiting_input）里；12 个真实状态常量都认识", () => {
  assert.equal(RUN_STATUSES.length, 12);
  for (const s of ["queued", "running", "waiting_approval", "waiting_input"]) assert.equal(isTerminalStatus(s), false, s);
  for (const s of ["succeeded", "failed", "canceled", "budget_exhausted", "step_limit", "timeout", "interrupted", "expired"]) assert.equal(isTerminalStatus(s), true, s);
});
