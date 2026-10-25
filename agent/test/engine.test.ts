import assert from "node:assert/strict";
import { test } from "node:test";
import { MemoryStorage } from "@earendil-works/pi-durable";
import { AgentEngine, type PublishedEvent } from "../src/engine/engine.ts";
import { AgentError } from "../src/engine/errors.ts";
import { createIdCodec } from "../src/ids.ts";
import { createPlatform } from "../src/platform-client.ts";
import { openStore, type AgentStore } from "../src/store/store.ts";
import type { Run } from "../src/store/types.ts";
import { decodeRunContext } from "../src/platform-client.ts";
import { fakeGo, type FakeGoOptions } from "./fake-go.ts";

const TOKEN = "service-token-0123456789abcdef0123456789";
const codec = createIdCodec("test-key");

interface Env {
  go: Awaited<ReturnType<typeof fakeGo>>;
  store: AgentStore;
  engine: AgentEngine;
  events: PublishedEvent[];
  sessionId: string;
  start(over?: Partial<Parameters<AgentEngine["startRun"]>[0]>): Promise<Run>;
  types(): string[];
  persisted(type: string): Array<Record<string, any>>;
  statuses(): string[];
  run(id: string): Run;
}

async function withEngine(go: FakeGoOptions, fn: (e: Env) => Promise<void>, opts: { approvalTtlMs?: number; canvasId?: string } = {}) {
  const g = await fakeGo(go);
  const store = openStore(":memory:", { workerId: 1 });
  const events: PublishedEvent[] = [];
  const engine = await AgentEngine.open({
    store, platform: createPlatform({ baseUrl: g.url, serviceToken: TOKEN }), baseUrl: g.url, serviceToken: TOKEN,
    storage: new MemoryStorage(), codec, onEvent: (ev) => events.push(ev), approvalTtlMs: opts.approvalTtlMs,
  });
  const session = store.createSession({ userId: "1", canvasId: opts.canvasId ?? "7", title: "新对话", mode: "all", modelKey: "" });
  const env: Env = {
    go: g, store, engine, events, sessionId: session.id,
    start: (over = {}) => engine.startRun({ userId: "1", sessionId: session.id, message: "拆分镜", modelKey: "claude", ...over }),
    types: () => store.listEvents(session.id, 0, 1000).map((x) => x.type),
    persisted: (type) => store.listEvents(session.id, 0, 1000).filter((x) => x.type === type).map((x) => x.payload as Record<string, any>),
    statuses: () => store.listEvents(session.id, 0, 1000).filter((x) => x.type === "run.status").map((x) => (x.payload as { status: string }).status),
    run: (id) => store.getRun("1", id),
  };
  try {
    await fn(env);
  } finally {
    await engine.close();
    store.close();
    await g.close();
  }
}

test("一轮对话：发起 → 模型回答 → 成功；事件、用量、会话、版本、释放运行都对", () =>
  withEngine({ turns: [{ text: "好的" }] }, async (e) => {
    const r = await e.start();
    assert.equal(r.status, "queued", "发起时返回的是排队状态，和现在一致");
    await e.engine.idle();
    const run = e.run(r.id);
    assert.equal(run.status, "succeeded");
    assert.ok(run.endedAt);
    assert.deepEqual([run.promptVersion, run.toolsHash, run.buildSha, run.harnessKey], [15, "04c13780543e", "abc1234", "harnesskey01"], "版本取自运行准备，之后沿用");
    assert.equal(run.spentCredits, 1);
    assert.equal(run.contextTokens, 100);

    // 事件的类型和顺序：和 Go 现在发的一致（run.usage 在 message.done 之前）
    assert.deepEqual(e.types(), ["message.user", "run.status", "run.status", "run.usage", "message.done", "run.status"]);
    assert.deepEqual(e.statuses(), ["queued", "running", "succeeded"]);
    assert.deepEqual(e.persisted("message.user")[0], { text: "拆分镜", mode: "all", selection: [] });
    assert.deepEqual(e.persisted("run.usage")[0], { spent_credits: 1, context_tokens: 100 });
    assert.deepEqual(e.persisted("message.done")[0], { text: "好的", tool_calls: [], partial: false, credits: 1, estimated: false });
    assert.deepEqual(e.persisted("run.status")[2], { status: "succeeded", error: "" });

    // 会话：标题取首条消息，模式和模型记下，绑定了 Pi 的会话
    const s = e.store.getSession("1", e.sessionId);
    assert.deepEqual([s.title, s.mode, s.modelKey], ["拆分镜", "all", "claude"]);
    assert.ok(s.conversationId);

    // 平台调用：运行准备带消息，模型请求带运行上下文和服务令牌，用量按调用 id 查，最后释放运行
    const paths = e.go.paths();
    assert.equal(paths[0], "POST /runs/prepare");
    assert.deepEqual(e.go.seen[0].body, { message: "拆分镜", max_steps: 40 });
    assert.ok(paths.some((p) => p.startsWith(`GET /runs/${r.id}/usage?call_id=`)));
    assert.equal(paths.at(-1), `DELETE /runs/${r.id}`);
    const req = e.go.modelRequests[0];
    assert.equal(req.headers["x-agent-service-token"], TOKEN);
    assert.deepEqual(decodeRunContext(String(req.headers["x-agent-context"])), { run_id: r.id, user_id: "1", session_id: e.sessionId, canvas_id: "7", mode: "all", model_key: "claude", selection: [] });
    const msgs = req.body.messages as Array<{ role: string; content: any }>;
    assert.match(JSON.stringify(msgs[0]), /你是小镜/);
    assert.match(JSON.stringify(msgs.find((m) => m.role === "user")), /拆分镜[\s\S]*画布目录/);
  }));

test("文字增量只推送不落库（seq 为 0）；完整内容在 message.done 里", () =>
  withEngine({ turns: [{ text: "你好呀，这是一段需要流一会儿的回答", thinking: "想一想", charDelayMs: 25 }] }, async (e) => {
    await e.start();
    await e.engine.idle();
    const deltas = e.events.filter((x) => x.type === "message.delta");
    assert.ok(deltas.length > 0, "收到了增量");
    assert.ok("你好呀，这是一段需要流一会儿的回答".startsWith(deltas.map((x) => (x.payload as { text: string }).text).join("")), "增量按顺序拼起来是回答的前缀（最后一小段在 message.done 里）");
    assert.ok(deltas.every((x) => x.seq === 0), "临时事件不参与回放");
        assert.ok(!e.types().some((t) => t.endsWith(".delta")), "不落库");
    assert.equal(e.persisted("message.done")[0].thinking, "想一想");
  }));

test("工具回合：message.done 带工具调用 → tool.start → tool.end（内容来自 Go）→ 下一轮；计 1 步", () =>
  withEngine({
    turns: [{ tools: [{ id: "call_0", name: "canvas_get_state", args: { nodeIds: ["n1"] } }] }, { text: "读完了" }],
    tools: { canvas_get_state: { content: "1 个节点", tool_end: { id: "call_0", name: "canvas_get_state", is_error: false, summary: "读取 1 个节点", node_ids: ["n1"], label: "读取画布", verb: "读取" } } },
  }, async (e) => {
    const r = await e.start();
    await e.engine.idle();
    assert.deepEqual(e.types().filter((t) => t !== "run.usage"), ["message.user", "run.status", "run.status", "message.done", "tool.start", "tool.end", "message.done", "run.status"]);
    assert.deepEqual(e.persisted("message.done")[0].tool_calls, [{ id: "call_0", name: "canvas_get_state" }]);
    assert.deepEqual(e.persisted("tool.start")[0], { id: "call_0", name: "canvas_get_state", label: "标签-canvas_get_state", verb: "动词-canvas_get_state" });
    assert.deepEqual(e.persisted("tool.end")[0], { id: "call_0", name: "canvas_get_state", is_error: false, summary: "读取 1 个节点", node_ids: ["n1"], label: "读取画布", verb: "读取" });
    assert.equal(e.run(r.id).steps, 1);
    assert.equal(e.run(r.id).status, "succeeded");
    // 第二次模型请求里有工具结果
    assert.match(JSON.stringify(e.go.modelRequests[1].body.messages), /1 个节点/);
  }));

test("模型出错（上游 502）：运行失败，错误信息给用户看，事件里带 error", () =>
  withEngine({ turns: [{ error: { status: 502, message: "上游调用失败，请稍后重试", type: "upstream_error" } }] }, async (e) => {
    const r = await e.start();
    await e.engine.idle();
    const run = e.run(r.id);
    assert.equal(run.status, "failed");
    assert.equal(run.errorCode, "agent_error");
    assert.match(run.errorMessage, /上游调用失败/);
    const last = e.persisted("run.status").at(-1)!;
    assert.deepEqual([last.status, last.title, last.hint], ["failed", "运行失败", "可以换个说法重试"]);
    assert.match(last.error, /上游调用失败/);
    assert.equal(e.go.modelRequests.length, 1, "Go 负责重试，这里不再重试");
  }));

test("本轮额度用完（业务错误码 60008）：运行进入 budget_exhausted 而不是失败，可以追加额度后继续", () =>
  withEngine({ turns: [{ error: { status: 402, message: "超出本轮积分预算", errorCode: 60008 } }] }, async (e) => {
    const r = await e.start({ budgetCredits: 5 });
    await e.engine.idle();
    assert.equal(e.run(r.id).status, "budget_exhausted");
    const last = e.persisted("run.status").at(-1)!;
    assert.deepEqual([last.status, last.title, last.hint], ["budget_exhausted", "本轮额度用完了", "追加额度后可以继续"]);
    assert.equal(e.run(r.id).budgetCredits, 5);
    // 画布空出来了
    assert.equal(e.store.activeRun("7"), undefined);
  }));

test("余额不足（402，别的业务错误码）是失败，不是额度用完", () =>
  withEngine({ turns: [{ error: { status: 402, message: "积分不足", errorCode: 40001 } }] }, async (e) => {
    const r = await e.start();
    await e.engine.idle();
    assert.equal(e.run(r.id).status, "failed");
    assert.match(e.run(r.id).errorMessage, /积分不足/);
  }));

test("步数用尽：下一次工具调用不执行，运行暂停为 step_limit，本轮停下", () =>
  withEngine({ turns: [{ tools: [{ id: "c1", name: "canvas_get_state" }] }, { tools: [{ id: "c2", name: "canvas_get_state" }] }, { text: "不该到这里" }] }, async (e) => {
    const r = await e.start({ maxSteps: 1 });
    await e.engine.idle();
    assert.equal(e.run(r.id).status, "step_limit");
    assert.equal(e.go.seen.filter((s) => s.path.includes("/tools/")).length, 1, "第二次没有到平台");
    const last = e.persisted("run.status").at(-1)!;
    assert.deepEqual([last.status, last.title], ["step_limit", "达到本轮步数上限"]);
    assert.equal(e.go.modelRequests.length, 2, "停下之后不再请求模型");
    assert.equal(e.run(r.id).maxSteps, 1);
  }));

test("审批类工具：预估通过后创建审批，运行进入 waiting_approval，本轮停下；审批 24 小时有效", () =>
  withEngine({
    turns: [{ tools: [{ id: "call_g", name: "generate_media", args: { items: [{ nodeId: "n1" }] } }] }, { text: "不该到这里" }],
    propose: { generate: { payload: { items: [{ node_id: "n1", price: 5 }] }, quote_credits: 5, summary: "生成 1 个，预估 5 积分", content: "已提交给用户批准，等待决定。" } },
  }, async (e) => {
    const r = await e.start();
    await e.engine.idle();
    assert.equal(e.run(r.id).status, "waiting_approval");
    const [a] = e.store.listApprovals(r.id);
    assert.deepEqual([a.kind, a.status, a.toolCallId, a.quoteCredits], ["generate", "pending", "call_g", 5]);
    assert.deepEqual(a.payload, { items: [{ node_id: "n1", price: 5 }] });
    assert.equal(a.expiresAt - a.createdAt, 24 * 3600 * 1000);
    assert.deepEqual(e.types().filter((t) => ["approval.created", "tool.start", "tool.end"].includes(t)), ["tool.start", "approval.created", "tool.end"], "顺序：开始 → 创建审批 → 结束");
    assert.equal(e.persisted("approval.created")[0].id, codec.encode(BigInt(a.id)), "事件里的审批 id 是对外的编码串");
    assert.equal(e.persisted("approval.created")[0].run_id, codec.encode(BigInt(r.id)));
    assert.deepEqual(e.persisted("run.status").slice(-1)[0], { status: "waiting_approval" });
    assert.equal(e.go.modelRequests.length, 1, "等用户决定期间不请求模型");
    assert.equal(e.store.activeRun("7")?.id, r.id, "等审批的运行仍占着画布");
  }));

test("提问进入 waiting_input", () =>
  withEngine({ turns: [{ tools: [{ id: "c1", name: "ask_user", args: { question: "要几张？" } }] }] }, async (e) => {
    const r = await e.start();
    await e.engine.idle();
    assert.equal(e.run(r.id).status, "waiting_input");
    assert.equal(e.store.listApprovals(r.id)[0].kind, "ask");
  }));

test("审批按调用 id 幂等：崩溃后重放同一个工具调用不会出现两个审批", () =>
  withEngine({
    turns: [{ tools: [{ id: "slow", name: "canvas_get_state" }] }, { text: "不该到这里" }],
    tools: { canvas_get_state: async () => { await new Promise((r) => setTimeout(r, 300)); return { content: "ok" }; } },
  }, async (e) => {
    const r = await e.start();
    for (let i = 0; i < 100 && !e.go.paths().some((p) => p.includes("/tools/")); i++) await new Promise((x) => setTimeout(x, 20));
    const rc = decodeRunContext(String(e.go.modelRequests[0].headers["x-agent-context"]));
    const make = () => e.engine.host.approval({ id: r.id, rc }, { kind: "generate", toolCallId: "call_g", payload: { a: 1 }, quoteCredits: 0, summary: "x" });
    await make();
    await make(); // 重放：同一个调用 id
    await e.engine.idle();
    assert.equal(e.store.listApprovals(r.id).length, 1);
    assert.equal(e.types().filter((t) => t === "approval.created").length, 1);
    assert.equal(e.run(r.id).status, "waiting_approval");
  }));

test("取消：运行进入 canceled，等待的审批失效，进行中的模型调用被中止；之后不会被覆盖成别的状态", () =>
  withEngine({ turns: [{ text: "我先想", hang: true }] }, async (e) => {
    const r = await e.start();
    for (let i = 0; i < 100 && e.go.modelRequests.length === 0; i++) await new Promise((x) => setTimeout(x, 20));
    const out = await e.engine.cancel("1", r.id);
    assert.equal(out.status, "canceled");
    await e.engine.idle();
    assert.equal(e.run(r.id).status, "canceled");
    assert.equal(e.statuses().at(-1), "canceled");
    assert.equal(e.statuses().filter((s) => s === "canceled").length, 1);
    assert.equal(e.store.activeRun("7"), undefined);
    assert.ok(e.go.paths().includes(`DELETE /runs/${r.id}`), "释放运行");
    // 已经结束的运行不能再取消
    await assert.rejects(e.engine.cancel("1", r.id), (err: AgentError) => err.code === 60003);
    // 别人的运行不存在
    await assert.rejects(e.engine.cancel("2", r.id), (err: AgentError) => err.code === 60012);
  }));

test("取消时等待中的审批一并失效", () =>
  withEngine({ turns: [{ tools: [{ id: "c1", name: "generate_media" }] }] }, async (e) => {
    const r = await e.start();
    await e.engine.idle();
    await e.engine.cancel("1", r.id);
    assert.equal(e.store.listApprovals(r.id)[0].status, "expired");
    assert.equal(e.run(r.id).status, "canceled");
  }));

test("插话：运行中补充要求，记 message.user（steer），下一次模型请求里能看到；不在运行中不能插话", () =>
  withEngine({
    turns: [{ tools: [{ id: "c1", name: "canvas_get_state" }] }, { text: "收到" }],
    tools: { canvas_get_state: async () => { await new Promise((r) => setTimeout(r, 300)); return { content: "ok" }; } },
  }, async (e) => {
    const r = await e.start();
    for (let i = 0; i < 100 && !e.go.paths().some((p) => p.includes("/tools/")); i++) await new Promise((x) => setTimeout(x, 20));
    await e.engine.interject("1", r.id, "再暖一点");
    await e.engine.idle();
    assert.deepEqual(e.persisted("message.user")[1], { text: "再暖一点", steer: true });
    assert.match(JSON.stringify(e.go.modelRequests[1].body.messages), /再暖一点/, "工具跑完后的下一次请求带上插话");
    assert.equal(e.go.modelRequests.length, 2, "不打断正在执行的工具，只多一次请求");
    assert.equal(e.run(r.id).status, "succeeded");
    await assert.rejects(e.engine.interject("1", r.id, "晚了"), (err: AgentError) => err.code === 60003);
    await assert.rejects(e.engine.interject("1", r.id, "  "), (err: AgentError) => err.code === 10001);
  }));

test("同一画布同时只能有一个运行；不同画布的运行并发，各带各的运行上下文", () =>
  withEngine({ turns: [{ text: "甲", delayMs: 100 }, { text: "乙", delayMs: 100 }] }, async (e) => {
    const s2 = e.store.createSession({ userId: "1", canvasId: "8", title: "新对话", mode: "all", modelKey: "" });
    const a = await e.start();
    await assert.rejects(e.start(), (err: AgentError) => err.code === 60001);
    const b = await e.engine.startRun({ userId: "1", sessionId: s2.id, message: "另一个", modelKey: "claude" });
    await e.engine.idle();
    assert.equal(e.run(a.id).status, "succeeded");
    assert.equal(e.run(b.id).status, "succeeded");
    const ctxs = e.go.modelRequests.map((q) => decodeRunContext(String(q.headers["x-agent-context"])));
    assert.deepEqual(ctxs.map((c) => c.run_id).sort(), [a.id, b.id].sort());
    assert.deepEqual(ctxs.find((c) => c.run_id === b.id)!.canvas_id, "8");
    // 事件各记在各自的会话里
    assert.equal(e.store.listEvents(s2.id, 0, 100).filter((x) => x.type === "message.user").length, 1);
  }));

test("校验：别人的会话、空消息、没有模型、平台准备失败", async () => {
  await withEngine({ turns: [] }, async (e) => {
    await assert.rejects(e.engine.startRun({ userId: "2", sessionId: e.sessionId, message: "x", modelKey: "claude" }), (err: AgentError) => err.code === 60011);
    await assert.rejects(e.start({ message: "  \n " }), (err: AgentError) => err.code === 10001 && /消息不能为空/.test(err.message));
    await assert.rejects(e.start({ modelKey: "" }), (err: AgentError) => err.code === 60002);
    assert.equal(e.store.activeRun("7"), undefined, "校验失败不能留下占着画布的运行");
  });
  await withEngine({ turns: [], prepareError: { status: 400, code: 60002, msg: "Agent 模型不可用" } }, async (e) => {
    await assert.rejects(e.start(), (err: AgentError) => err.code === 60005, "启动失败对用户说 Agent 暂不可用");
    const [run] = [e.store.activeRun("7")];
    assert.equal(run, undefined, "画布立刻空出来");
    assert.equal(e.statuses().at(-1), "failed");
  });
});
