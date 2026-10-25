import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { statusEvent } from "../src/engine/status.ts";
import { codec } from "./engine-helpers.ts";
import { withHttp } from "./http-helpers.ts";

/**
 * 黄金文件对照：agent/test/golden/go-contract.json 是 Go 现在的实现跑一遍完整场景录下来的（backend 里的
 * TestGolden_RESTContract，Go 改了前端能看到的东西会让它失败，必须重新录制）。这里用 TypeScript 的 REST 接口和引擎
 * 跑同样的场景，逐类事件、逐个视图和录下来的比对：字段一个都不能多、不能少、类型不能变。
 */
const golden = JSON.parse(readFileSync(new URL("./golden/go-contract.json", import.meta.url), "utf8"));
const enc = (id: string) => codec.encode(BigInt(id));

const RE_ID = /^[0-9a-f]{32}$/;
const RE_TIME = /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d/;
const RE_NODE = /^n_[0-9a-f]{10}$/;
/** 和 Go 录制时同样的规整：会变的值换成占位符 */
function normalize(v: any): any {
  if (Array.isArray(v)) return v.map(normalize);
  if (v && typeof v === "object") return Object.fromEntries(Object.entries(v).map(([k, x]) => [k, normalize(x)]));
  if (typeof v === "string") return RE_ID.test(v) ? "<id>" : RE_TIME.test(v) ? "<time>" : RE_NODE.test(v) ? "<node>" : v;
  return v;
}

const kind = (v: unknown) => (v === null ? "null" : Array.isArray(v) ? "array" : typeof v);
/** 只看这一层：有哪些字段、各是什么类型（内容不定的字段，比如审批内容，不往里看） */
const flat = (v: any) => Object.fromEntries(Object.entries(v).sort(([a], [b]) => a.localeCompare(b)).map(([k, x]) => [k, kind(x)]));
/** 往里看：每一层的字段和类型；数组看第一项 */
function deep(v: any): any {
  if (Array.isArray(v)) return v.length ? [deep(v[0])] : [];
  if (v && typeof v === "object") return Object.fromEntries(Object.entries(v).sort(([a], [b]) => a.localeCompare(b)).map(([k, x]) => [k, deep(x)]));
  return kind(v);
}

const GOLDEN_APPROVAL_PAYLOAD = golden.events["approval.created"].payload;

test("黄金文件对照：同样的场景，TypeScript 的 REST 接口和事件和 Go 录下来的一致", () =>
  withHttp({
    models: golden.views.models,
    turns: [
      // 模型回答：思考 + 文字 + 一个工具调用（流得慢一点，才有文字增量）
      { thinking: "先看画布", text: "我先看看画布。", tools: [{ id: "call_1", name: "canvas_get_state", args: {} }], charDelayMs: 60 },
      { tools: [{ id: "call_2", name: "plan_update", args: { steps: [{ title: "拆分镜", status: "doing" }, { title: "出图", status: "todo" }] } }] },
      { tools: [{ id: "call_3", name: "generate_media", args: { items: [{ nodeId: "n1" }] } }] },
      { text: "已开始生成" },
      // 第二个运行：工具里慢一点，好插话和取消
      { tools: [{ id: "slow_1", name: "model_list", args: {} }] },
    ],
    tools: {
      canvas_get_state: { tool_end: { id: "call_1", name: "canvas_get_state", is_error: false, summary: "读取画布目录（1 个节点）", label: "读取画布", verb: "读取" } },
      model_list: async () => { await new Promise((r) => setTimeout(r, 2000)); return { content: "ok" }; },
    },
    propose: { generate: { payload: GOLDEN_APPROVAL_PAYLOAD, quote_credits: 24, summary: "生成 3 个，预估 24 积分", content: "已提交给用户批准，等待决定。" } },
    resolve: () => ({ status: "approved", decision: golden.events["approval.decided"].decision, quote_credits: 16 }),
  }, async (h, e) => {
    const views: Record<string, any> = {};
    const events = (type: string) => e.store.listEvents(e.sessionId, 0, 1000).filter((x) => x.type === type).map((x) => x.payload as Record<string, any>);

    // ---- 会话 ----
    const created = await h.call("POST", `/canvas/${enc("7")}/agent/sessions`, { title: "角色设计", mode: "storyboard", model_key: "claude" });
    views.session = normalize(created.body.data);
    views.models = normalize((await h.call("GET", "/agent/models")).body.data);

    // ---- 一轮完整的运行 ----
    const sid = enc(e.sessionId);
    const started = await h.call("POST", `/agent/sessions/${sid}/runs`, { message: "拆分镜", selection: ["n_script"], budget_credits: 50 });
    assert.equal(started.status, 202);
    views.run_started = normalize(started.body.data);
    const rid = started.body.data.id;
    for (let i = 0; i < 300 && e.run(started.body.data && codec.decodeDecimal(rid)!).status !== "waiting_approval"; i++) await new Promise((r) => setTimeout(r, 20));
    const approval = e.store.listApprovals(codec.decodeDecimal(rid)!)[0];
    const decided = await h.call("POST", `/agent/approvals/${enc(approval.id)}/decision`, {
      decision: "approve", items: [{ index: 0, approve: true, count: 1 }, { index: 1, approve: true }], add_budget: 10,
    });
    views.approval_decided = normalize(decided.body.data);
    await e.engine.idle();
    const undo = await h.call("POST", `/agent/runs/${rid}/undo`);
    views.undo = normalize(undo.body.data);

    // ---- 第二轮：插话、取消 ----
    const second = await h.call("POST", `/agent/sessions/${sid}/runs`, { message: "再来一轮" });
    const rid2 = second.body.data.id;
    for (let i = 0; i < 300 && !e.go.paths().some((p) => p.includes("/tools/model_list")); i++) await new Promise((r) => setTimeout(r, 20));
    assert.equal((await h.call("POST", `/agent/runs/${rid2}/interject`, { message: "再暖一点" })).status, 200);
    const canceled = await h.call("POST", `/agent/runs/${rid2}/cancel`);
    views.run_canceled = normalize(canceled.body.data);
    await e.engine.idle();

    // ---- 读 ----
    const evs = await h.call("GET", `/agent/sessions/${sid}/events?after=0&limit=200`);
    const hist = await h.call("GET", `/agent/sessions/${sid}/history`);

    // ===== 比对 =====
    // 视图：字段和类型一致（运行、会话）；运行视图的值也一致
    assert.deepEqual(flat(views.session), flat(golden.views.session), "会话视图");
    assert.deepEqual(views.session, { ...golden.views.session, last_seq: 0 }, "会话视图的值");
    assert.deepEqual(views.models, golden.views.models, "模型清单");
    assert.deepEqual(views.run_started, golden.views.run_started, "发起运行返回的运行视图");
    assert.deepEqual(flat(views.run_canceled), flat(golden.views.run_canceled), "取消返回的运行视图");
    assert.equal(views.run_canceled.status, golden.views.run_canceled.status);
    assert.deepEqual(flat(views.approval_decided), flat(golden.views.approval_decided), "审批决定返回的视图");
    assert.equal(views.approval_decided.status, golden.views.approval_decided.status);
    assert.equal(views.approval_decided.kind, golden.views.approval_decided.kind);
    assert.deepEqual(flat(views.undo), flat(golden.views.undo), "撤销的结果");

    // 回放和历史：每个元素的字段和类型；HTTP 回放里没有画布 id
    const goldenEv = golden.views.events[0];
    for (const ev of evs.body.data) assert.deepEqual(flat(normalize(ev)), flat(goldenEv), "回放的事件视图 " + ev.type);
    assert.deepEqual(Object.keys(hist.body.data).sort(), Object.keys(golden.views.history).sort(), "历史页");
    assert.equal(typeof hist.body.data.has_more, "boolean");
    assert.ok(golden.views.events.every((ev: any) => ev.canvas_id === null), "Go 的回放里 canvas_id 是 null");
    assert.ok(evs.body.data.every((ev: any) => ev.canvas_id === null));

    // 事件：每一类的内容
    const mine = (type: string) => events(type)[0];
    const want = (type: string) => golden.events[type];
    for (const type of Object.keys(golden.events)) {
      const got = type.endsWith(".delta") ? e.events.find((x) => x.type === type)?.payload : mine(type);
      assert.ok(got, `TypeScript 没有产生 ${type} 事件`);
      assert.deepEqual(deep(normalize(got)), deep(want(type)), `${type} 的字段和类型`);
    }
    // 内容完全一致的几类
    assert.deepEqual(normalize(mine("message.user")), want("message.user"));
    assert.deepEqual(normalize(mine("plan.updated")), want("plan.updated"));
    assert.deepEqual(normalize(mine("run.undone")), want("run.undone"));
    assert.deepEqual(Object.keys(mine("tool.start")).sort(), Object.keys(want("tool.start")).sort());
    assert.deepEqual([mine("tool.start").label, mine("tool.start").verb], [want("tool.start").label, want("tool.start").verb]);
    const done = events("message.done").find((m) => m.thinking)!;
    assert.deepEqual([done.text, done.thinking, done.partial, done.tool_calls[0].name], [want("message.done").text, want("message.done").thinking, want("message.done").partial, want("message.done").tool_calls[0].name]);
    assert.deepEqual(normalize(mine("approval.created")), want("approval.created"));
    assert.deepEqual(flat(normalize(events("approval.decided")[0])), flat(want("approval.decided")));

    // run.status：Go 录到的每一种状态，内容完全一致（标题和提示文字就在里面）
    const seen: Record<string, any> = {};
    for (const p of events("run.status")) seen[p.status] ??= normalize(p);
    for (const [status, payload] of Object.entries(golden.run_statuses)) {
      assert.deepEqual(seen[status], payload, `run.status ${status}`);
    }
    // 其余的状态文字，不靠场景走到，直接对 Go 的表
    for (const [status, t] of Object.entries(golden.status_texts as Record<string, { title: string; hint: string }>)) {
      const ev = statusEvent(status);
      assert.deepEqual([ev.title ?? "", ev.hint ?? ""], [t.title, t.hint], `状态文字 ${status}`);
    }
  }));
