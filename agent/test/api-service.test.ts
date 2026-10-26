import assert from "node:assert/strict";
import { test } from "node:test";
import { AgentApiService } from "../src/api/service.ts";
import { AgentError } from "../src/engine/errors.ts";
import { codec, withEngine, type Env } from "./engine-helpers.ts";
import type { FakeGoOptions } from "./fake-go.ts";

const code = (c: number) => (e: unknown) => e instanceof AgentError && e.code === c;
const enc = (id: string) => codec.encode(BigInt(id));
const dec = (hex: string) => codec.decodeDecimal(hex)!;

async function withApi(go: FakeGoOptions, fn: (api: AgentApiService, e: Env) => Promise<void>) {
  await withEngine(go, async (e) => fn(new AgentApiService({ store: e.store, engine: e.engine, platform: e.platform, codec, now: () => Date.now() }), e));
}

// ---- 会话 ----

test("新建会话：先问 Go 画布是不是自己的；补默认值；视图里的 id 是编码串、时间是 ISO", () =>
  withApi({ turns: [] }, async (api, e) => {
    const v = await api.createSession("1", "7", { title: "  角色   设计  ", mode: "", model_key: "" });
    assert.deepEqual(Object.keys(v).sort(), ["canvas_id", "created_at", "id", "last_seq", "mode", "model_key", "title", "updated_at"]);
    assert.equal(v.title, "角色 设计", "空白压成一个空格");
    assert.deepEqual([v.mode, v.model_key, v.last_seq], ["all", "", 0]);
    assert.match(v.id, /^[0-9a-f]{32}$/);
    assert.equal(v.canvas_id, enc("7"));
    assert.match(v.created_at, /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d/);
    assert.ok(e.go.paths().includes("POST /canvases/check"));
    assert.equal((await api.createSession("1", "7", {})).title, "新对话");
    assert.equal((await api.createSession("1", "7", { title: "字".repeat(60) })).title.length, 40);
  }));

test("新建会话：别人的画布（Go 说画布不存在）→ 原样的业务错误；每个画布最多 50 个", () =>
  withApi({ turns: [], checkCanvas: (b) => (b.user_id === "2" ? { error: { status: 404, code: 30001, msg: "画布不存在" } } : undefined) }, async (api) => {
    await assert.rejects(api.createSession("2", "7", {}), (err: AgentError) => err.code === 30001 && err.status === 404);
    for (let i = 0; i < 49; i++) await api.createSession("1", "7", {}); // 环境里已经有 1 个，凑满 50 个
    await assert.rejects(api.createSession("1", "7", {}), (err: AgentError) => err.code === 60010 && err.status === 409);
    await api.createSession("1", "8", {}); // 别的画布不受影响
  }));

test("会话列表：最近更新的在前；改名、删除；别人的会话是 60011", () =>
  withApi({ turns: [] }, async (api, e) => {
    const a = await api.createSession("1", "7", { title: "甲" });
    await new Promise((r) => setTimeout(r, 5));
    const b = await api.createSession("1", "7", { title: "乙" });
    assert.deepEqual((await api.listSessions("1", "7")).map((s) => s.title).filter((t) => ["甲", "乙"].includes(t)), ["乙", "甲"]);
    await new Promise((r) => setTimeout(r, 5));
    const renamed = await api.renameSession("1", dec(a.id), "  新名字 ");
    assert.equal(renamed.title, "新名字");
    assert.equal((await api.listSessions("1", "7"))[0].id, a.id, "改名后更新时间最新");
    await assert.rejects(api.renameSession("1", dec(a.id), "   "), (err: AgentError) => err.code === 10001 && /标题不能为空/.test(err.message));
    await assert.rejects(api.renameSession("2", dec(a.id), "x"), code(60011));
    await assert.rejects(api.deleteSession("2", dec(a.id)), code(60011));
    await api.deleteSession("1", dec(a.id));
    assert.ok(!(await api.listSessions("1", "7")).some((s) => s.id === a.id));
    await assert.rejects(api.deleteSession("1", dec(a.id)), code(60011), "已删除的");
    void b; void e;
  }));

test("删除会话：这个会话正占着画布（有运行在跑或等用户）时不允许，免得 Agent 往看不见的会话里继续写", () =>
  withApi({ turns: [{ tools: [{ id: "c1", name: "ask_user", args: { question: "几张？" } }] }] }, async (api, e) => {
    const run = await e.start();
    await e.engine.idle();
    assert.equal(e.run(run.id).status, "waiting_input");
    await assert.rejects(api.deleteSession("1", e.sessionId), code(60001));
    await e.engine.cancel("1", run.id);
    await api.deleteSession("1", e.sessionId);
  }));

// ---- 事件和历史 ----

test("回放事件：序号大于 after 的，升序；limit 不合法取 200；视图里 canvas_id 为 null、会话级事件 run_id 为 null", () =>
  withApi({ turns: [] }, async (api, e) => {
    for (let i = 0; i < 5; i++) e.store.appendEvent(e.sessionId, i === 0 ? "0" : e.store.createRun({ sessionId: e.sessionId, canvasId: "7", userId: "1", status: "succeeded", mode: "all" }).id, "message.done", { i });
    const all = await api.events("1", e.sessionId, 0, 0);
    assert.deepEqual(all.map((x) => x.seq), [1, 2, 3, 4, 5]);
    assert.deepEqual(Object.keys(all[0]).sort(), ["canvas_id", "created_at", "data", "run_id", "seq", "session_id", "type"]);
    assert.equal(all[0].run_id, null, "会话级事件");
    assert.equal(all[0].canvas_id, null, "HTTP 回放里没有画布 id（推送才有）");
    assert.match(String(all[1].run_id), /^[0-9a-f]{32}$/);
    assert.deepEqual(all[0].data, { i: 0 });
    assert.deepEqual((await api.events("1", e.sessionId, 3, 2)).map((x) => x.seq), [4, 5]);
    assert.equal((await api.events("1", e.sessionId, 0, 9999)).length, 5);
    await assert.rejects(api.events("2", e.sessionId, 0, 0), code(60011));
  }));

test("聊天记录：从最新往回翻；一页起点对齐到一轮的开头（用户发起的 message.user，插话不算）；has_more", () =>
  withApi({ turns: [] }, async (api, e) => {
    const sid = e.sessionId;
    const add = (type: string, payload: unknown = {}) => e.store.appendEvent(sid, "0", type, payload).seq;
    // 三轮：每轮 message.user + 若干事件；第二轮里有一条插话
    const turns: number[] = [];
    for (let t = 1; t <= 3; t++) {
      turns.push(add("message.user", { text: `第 ${t} 轮` }));
      add("run.status", { status: "running" });
      if (t === 2) add("message.user", { text: "插话", steer: true });
      add("message.done", { text: "好" });
      add("run.status", { status: "succeeded" });
    }
    // 总共 3×4 + 1 = 13 条。limit=5：最新 5 条是第 3 轮的 4 条加上第 2 轮最后一条 → 起点对齐到第 3 轮开头
    const page1 = await api.history("1", sid, 0, 5);
    assert.deepEqual(page1.events.map((x) => x.seq), [turns[2], turns[2] + 1, turns[2] + 2, turns[2] + 3]);
    assert.equal(page1.has_more, true);
    // 往上翻：before = 已有的最小序号
    const page2 = await api.history("1", sid, page1.events[0].seq, 5);
    assert.equal(page2.events[0].seq, turns[1], "起点是第 2 轮的开头，不是插话");
    assert.ok(page2.events.some((x) => (x.data as { steer?: boolean }).steer), "插话留在第 2 轮里");
    assert.equal(page2.has_more, true);
    const page3 = await api.history("1", sid, page2.events[0].seq, 5);
    assert.deepEqual(page3.events.map((x) => x.seq), [turns[0], turns[0] + 1, turns[0] + 2, turns[0] + 3]);
    assert.equal(page3.has_more, false);
    // 默认一页 100 条，装得下全部
    const whole = await api.history("1", sid, 0, 0);
    assert.equal(whole.events.length, 13);
    assert.equal(whole.has_more, false);
    await assert.rejects(api.history("2", sid, 0, 0), code(60011));
  }));

// ---- 运行 ----

test("发起运行：消息里的引用先问 Go；选中的节点用 Go 去掉不存在后的；模型沿用会话的或取清单第一个；返回运行视图", () =>
  withApi({
    turns: [{ text: "好" }], models: [{ key: "deepseek", name: "DeepSeek", vision: false, context_window: 64000 }, { key: "claude", name: "Claude", vision: true, context_window: 200000 }],
    checkMessage: (b) => ({ selection: (b.selection as string[]).filter((n) => n !== "ghost") }),
  }, async (api, e) => {
    const run = await api.startRun("1", e.sessionId, { message: "拆 @[剧本](node:n1)", selection: ["n1", "ghost"], budget_credits: 30 });
    await e.engine.idle();
    assert.deepEqual(Object.keys(run).sort(), ["budget_credits", "canvas_id", "context_tokens", "created_at", "ended_at", "error_code", "error_message", "id", "max_steps", "mode", "session_id", "spent_credits", "status", "steps"]);
    assert.deepEqual([run.status, run.budget_credits, run.ended_at, run.mode], ["queued", 30, null, "all"]);
    assert.equal(run.session_id, enc(e.sessionId));
    assert.deepEqual(e.go.seen.find((s) => s.path.endsWith("/messages/check"))!.body, { user_id: "1", canvas_id: "7", message: "拆 @[剧本](node:n1)", selection: ["n1", "ghost"] });
    assert.deepEqual(e.persisted("message.user")[0].selection, ["n1"], "去掉了不存在的节点");
    assert.equal(e.store.getSession("1", e.sessionId).modelKey, "deepseek", "没指定模型：取清单第一个");
  }));

test("发起运行：指定模型必须在清单里（60002）；引用不合法 10001 且不创建运行；别人的会话 60011", () =>
  withApi({
    turns: [], checkMessage: (b) => (String(b.message).includes("幽灵") ? { error: { status: 400, code: 10001, msg: "引用的节点「幽灵」不可用" } } : { selection: [] }),
  }, async (api, e) => {
    const sid = e.sessionId;
    await assert.rejects(api.startRun("1", sid, { message: "你好", agent_model_key: "不存在" }), code(60002));
    await assert.rejects(api.startRun("1", sid, { message: "拆 @[幽灵](node:ghost)" }), (err: AgentError) => err.code === 10001 && /幽灵/.test(err.message));
    await assert.rejects(api.startRun("2", sid, { message: "x" }), code(60011));
    await assert.rejects(api.startRun("1", sid, { message: "   " }), (err: AgentError) => err.code === 10001);
    assert.equal(e.store.activeRun("7"), undefined, "校验失败不留下占着画布的运行");
  }));

test("发起运行：清单是空的 → 60002", () =>
  withApi({ turns: [], models: [] }, async (api, e) => {
    await assert.rejects(api.startRun("1", e.sessionId, { message: "你好" }), code(60002));
  }));

test("插话、取消、继续、审批决定：id 是编码串；插话也要校验引用；返回运行 / 审批视图", () =>
  withApi({
    turns: [{ tools: [{ id: "c1", name: "canvas_get_state" }] }, { text: "好" }],
    tools: { canvas_get_state: async () => { await new Promise((r) => setTimeout(r, 200)); return { content: "ok" }; } },
    checkMessage: (b) => (String(b.message).includes("幽灵") ? { error: { status: 400, code: 10001, msg: "引用的节点「幽灵」不可用" } } : { selection: [] }),
  }, async (api, e) => {
    const started = await api.startRun("1", e.sessionId, { message: "拆分镜" });
    for (let i = 0; i < 100 && !e.go.paths().some((p) => p.includes("/tools/")); i++) await new Promise((r) => setTimeout(r, 20));
    await assert.rejects(api.interject("1", dec(started.id), "看 @[幽灵](node:ghost)"), (err: AgentError) => err.code === 10001);
    await api.interject("1", dec(started.id), "再暖一点");
    const canceled = await api.cancel("1", dec(started.id));
    assert.deepEqual([canceled.status, canceled.id], ["canceled", started.id]);
    await assert.rejects(api.cancel("2", dec(started.id)), code(60012));
    await assert.rejects(api.interject("1", dec(started.id), "晚了"), code(60003));
    await assert.rejects(api.resume("1", dec(started.id), 0), code(60003), "已停止的不能继续");
  }));

// ---- 撤销 ----

test("撤销：运行得是自己的、已经结束；经平台撤销；真的写了（revision 非零）才发 run.undone", () =>
  withApi({ turns: [{ text: "好" }], undo: () => ({ reverted: 2, skipped: [{ id: "n1", reason: "用户改过" }], revision: 9 }) }, async (api, e) => {
    const run = await e.start();
    await e.engine.idle();
    const out = await api.undo("1", run.id);
    assert.deepEqual(out, { reverted: 2, skipped: [{ id: "n1", reason: "用户改过" }], revision: 9 });
    const undone = e.persisted("run.undone");
    assert.deepEqual(undone, [{ reverted: 2, skipped: 1 }], "skipped 是条数");
    const hit = e.go.seen.find((s) => s.path.endsWith("/undo"))!;
    assert.match(String(hit.headers["x-agent-context"]), /./);
    await assert.rejects(api.undo("2", run.id), code(60012));
  }));

test("撤销：运行还在跑或在等用户 → 60003；没改过画布（revision 为 0）不发事件；重复撤销的业务错误原样返回", async () => {
  await withApi({ turns: [{ tools: [{ id: "c1", name: "ask_user" }] }] }, async (api, e) => {
    const run = await e.start();
    await e.engine.idle();
    await assert.rejects(api.undo("1", run.id), code(60003));
  });
  await withApi({ turns: [{ text: "好" }], undo: () => ({ reverted: 0, skipped: [], revision: 0 }) }, async (api, e) => {
    const run = await e.start();
    await e.engine.idle();
    assert.equal((await api.undo("1", run.id)).revision, 0);
    assert.equal(e.persisted("run.undone").length, 0);
  });
  await withApi({ turns: [{ text: "好" }], undo: () => ({ error: { status: 409, code: 60007, msg: "本轮已撤销" } }) }, async (api, e) => {
    const run = await e.start();
    await e.engine.idle();
    await assert.rejects(api.undo("1", run.id), (err: AgentError) => err.code === 60007 && err.status === 409);
  });
});

test("模型清单：原样给前端；空清单是 []", async () => {
  await withApi({ turns: [] }, async (api) => assert.deepEqual(await api.models(), [{ key: "claude", name: "Claude", vision: true, context_window: 200000 }]));
  await withApi({ turns: [], models: [] }, async (api) => assert.deepEqual(await api.models(), []));
});

// ---- 评价 ----

test("评价：点踩记下评分、去重后的原因和说明，发 feedback.set；点赞忽略原因；改选覆盖", () =>
  withApi({ turns: [{ text: "好" }] }, async (api, e) => {
    const run = await e.start();
    await e.engine.idle();
    const down = await api.setFeedback("1", run.id, { rating: -1, reasons: ["misread", "slow", "misread"], comment: "  我要 6 个，它给了 12 个  " });
    assert.deepEqual(down, { rating: -1, reasons: ["misread", "slow"], comment: "我要 6 个，它给了 12 个" });
    assert.deepEqual(e.persisted("feedback.set"), [down], "事件载荷里不放运行 id（事件自带）");
    const up = await api.setFeedback("1", run.id, { rating: 1, reasons: ["slow"], comment: "" });
    assert.deepEqual(up, { rating: 1, reasons: [], comment: "" });
    assert.equal(e.store.getFeedback(run.id, "1")!.rating, 1, "覆盖，不是新增");
    assert.equal(e.persisted("feedback.set").length, 2);
  }));

test("评价的限制：评分只能是 1 或 -1；原因必须是已知标签、最多 5 个；说明最多 500 字；运行得是自己的、已经结束", () =>
  withApi({ turns: [{ tools: [{ id: "c1", name: "ask_user", args: { question: "几张？" } }] }] }, async (api, e) => {
    const run = await e.start();
    await e.engine.idle(); // 停在 waiting_input：还没结束
    await assert.rejects(api.setFeedback("1", run.id, { rating: 1, reasons: [], comment: "" }), (err: AgentError) => err.code === 60014 && err.status === 409);
    await e.engine.cancel("1", run.id);
    const bad = (rating: number, reasons: string[], comment: string, re: RegExp) => assert.rejects(api.setFeedback("1", run.id, { rating, reasons, comment }), (err: AgentError) => err.code === 10001 && re.test(err.message));
    await bad(0, [], "", /评价只能是 1 或 -1/);
    await bad(2, [], "", /评价只能是 1 或 -1/);
    await bad(-1, ["瞎填"], "", /未知的原因标签：瞎填/);
    await bad(-1, Array(6).fill("slow"), "", /原因标签太多/);
    await bad(-1, [], "字".repeat(501), /补充说明最多 500 字/);
    await api.setFeedback("1", run.id, { rating: -1, reasons: [], comment: "字".repeat(500) }); // 正好 500 字
    await assert.rejects(api.setFeedback("2", run.id, { rating: 1, reasons: [], comment: "" }), code(60012));
    assert.equal(e.store.getFeedback(run.id, "2"), undefined);
  }));

test("取消评价：删掉并发 feedback.cleared；本来就没有是幂等的，不发事件；别人的运行 60012", () =>
  withApi({ turns: [{ text: "好" }] }, async (api, e) => {
    const run = await e.start();
    await e.engine.idle();
    await api.clearFeedback("1", run.id);
    assert.equal(e.persisted("feedback.cleared").length, 0);
    await api.setFeedback("1", run.id, { rating: 1, reasons: [], comment: "" });
    await api.clearFeedback("1", run.id);
    assert.deepEqual(e.persisted("feedback.cleared"), [{}]);
    assert.equal(e.store.getFeedback(run.id, "1"), undefined);
    await assert.rejects(api.clearFeedback("2", run.id), code(60012));
  }));

test("标题来源：创建时没给标题是草稿，给了就是用户起的；手动改名算用户起的，之后模型不会再覆盖", () =>
  withApi({ turns: [] }, async (api, e) => {
    const draft = await api.createSession("1", "7", {});
    const named = await api.createSession("1", "7", { title: "角色设计" });
    assert.equal(e.store.getSession("1", dec(draft.id)).titleSource, "draft");
    assert.equal(e.store.getSession("1", dec(named.id)).titleSource, "user");

    await api.renameSession("1", dec(draft.id), "我的名字");
    assert.equal(e.store.getSession("1", dec(draft.id)).titleSource, "user");
    assert.equal(e.store.setLlmTitle("1", dec(draft.id), "模型的名字"), false);
    assert.deepEqual(Object.keys(await api.renameSession("1", dec(draft.id), "再改")).sort(),
      ["canvas_id", "created_at", "id", "last_seq", "mode", "model_key", "title", "updated_at"], "接口返回的字段不变，不暴露标题来源");
  }));
