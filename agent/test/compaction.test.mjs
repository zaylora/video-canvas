import assert from "node:assert/strict";
import { test } from "node:test";
import {
  SUMMARY_PREFIX, compactionLimits, createCompactor, findCutPoint, isSummaryMessage, serializeMessages,
} from "../src/compaction.mjs";
import { runAgent } from "../src/run.mjs";
import { baseInput, fakeBridge } from "./helpers.mjs";

const withBridge = async (opts, fn) => { const b = await fakeBridge(opts); try { await fn(b); } finally { await b.close(); } };
const ZERO = { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, totalTokens: 0, cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 } };
const text = (m) => (typeof m.content === "string" ? m.content : (m.content ?? []).map((p) => p.text ?? p.name ?? "").join(""));
/** 带标记的定长文本：标记在最前，断言时看某条消息有没有进摘要 */
const body = (mark, chars) => mark + "字".repeat(Math.max(0, chars - mark.length));

/** 十条交替的消息，每条 360 字：总量 3600 刚过窗口 4000 的触发线 3400；留 A7 U8 A9，其余进摘要 */
const tenMessages = () => Array.from({ length: 10 }, (_, i) => (i % 2 ? asst(body("A" + i, 360)) : user(body("U" + i, 360))));

const user = (t) => ({ role: "user", content: [{ type: "text", text: t }], timestamp: 1 });
const asst = (t, calls = []) => ({
  role: "assistant", content: [{ type: "text", text: t }, ...calls.map((id) => ({ type: "toolCall", id, name: "canvas_get_state", arguments: { nodeIds: ["a"] } }))],
  api: "openai-completions", provider: "bridge", model: "agent", usage: ZERO, stopReason: calls.length ? "toolUse" : "stop", timestamp: 1,
});
const toolResult = (id, t) => ({ role: "toolResult", toolCallId: id, toolName: "canvas_get_state", content: [{ type: "text", text: t }], isError: false, timestamp: 1 });

test("切点：从最新往回留够 keepRecent，落在用户或助手消息前", () => {
  const msgs = [user(body("U0", 1000)), asst(body("A1", 1000)), user(body("U2", 1000)), asst(body("A3", 1000))];
  assert.equal(findCutPoint(msgs, 1500), 2, "最近两条共 2000 ≥ 1500，从 U2 开始留");
  assert.equal(findCutPoint(msgs, 100000), 0, "全部都在保留范围内，没有可切的");
});

test("切点：永远不落在工具结果上，工具调用和它的结果成对留下", () => {
  const msgs = [user("u"), asst("a", ["c1"]), toolResult("c1", body("R", 3000)), asst("done"), user("再来")];
  // 往回累计在工具结果处越线：不能从结果切，向新的方向找到下一条助手消息
  const cut = findCutPoint(msgs, 1000);
  assert.notEqual(msgs[cut]?.role, "toolResult");
  assert.equal(cut, 3);
  // 越线的是最后的工具结果、后面没有可切的位置：退回它前面的助手消息，调用和结果一起留
  const tail = [user("u"), asst("a", ["c1"]), toolResult("c1", body("R", 5000))];
  assert.equal(findCutPoint(tail, 1000), 1);
});

test("序列化：用户、助手、工具调用、工具结果（过长截断）、图片占位", () => {
  const out = serializeMessages([
    user("拆分镜"),
    asst("先读画布", ["c1"]),
    toolResult("c1", "画布".repeat(100)),
    { role: "toolResult", toolCallId: "c2", toolName: "canvas_inspect_image", content: [{ type: "image", data: "x", mimeType: "image/png" }], isError: false, timestamp: 1 },
  ], { maxResultChars: 20 });
  assert.match(out, /\[用户\]\s*拆分镜/);
  assert.match(out, /\[助手\]\s*先读画布/);
  assert.match(out, /canvas_get_state/);
  assert.match(out, /nodeIds/);
  assert.match(out, /已截断/);
  assert.ok(!out.includes("画布".repeat(30)), "过长的工具结果被截断");
  assert.match(out, /\[图片\]/);
});

test("摘要消息：带固定前缀，能识别出来", () => {
  const c = createCompactor({ summarize: async () => "目标：拆分镜", window: 1000 });
  assert.ok(isSummaryMessage(user(SUMMARY_PREFIX + "x")));
  assert.ok(!isSummaryMessage(user("普通消息")));
  assert.ok(!isSummaryMessage(asst(SUMMARY_PREFIX + "x")));
  assert.ok(c);
});

test("触发线：窗口越小留的越少，默认 reserve 16384 / keepRecent 20000 封顶", () => {
  assert.deepEqual(compactionLimits(4000), { reserve: 600, keepRecent: 1000 });
  assert.deepEqual(compactionLimits(200000), { reserve: 16384, keepRecent: 20000 });
});

test("压缩：没超线不动，也不调用模型", async () => {
  let calls = 0;
  const c = createCompactor({ summarize: async () => (calls++, "x"), window: 10000 });
  assert.equal(await c.compact([user("你好"), asst("好")]), null);
  assert.equal(calls, 0);
});

test("压缩：超线时旧消息换成一条摘要，最近的原样留下", async () => {
  const prompts = [];
  const c = createCompactor({ summarize: async (p) => (prompts.push(p), "目标：拆分镜；已建节点 n1"), window: 4000 });
  const msgs = [...tenMessages(), user("拆分镜")];
  const out = await c.compact(msgs);
  assert.equal(prompts.length, 1);
  for (const gone of ["U0", "A1", "U2", "A3", "U4", "A5", "U6"]) assert.ok(prompts[0].includes(gone), `${gone} 进摘要`);
  for (const kept of ["A7", "U8", "A9"]) assert.ok(!prompts[0].includes(kept), `${kept} 留在原处，不进摘要`);
  assert.ok(isSummaryMessage(out[0]));
  assert.match(text(out[0]), /目标：拆分镜；已建节点 n1/);
  assert.deepEqual(out.slice(1), msgs.slice(7), "最近的消息原样留下");
});

test("压缩：已经有摘要时增量更新，旧摘要作为输入", async () => {
  const prompts = [];
  const c = createCompactor({ summarize: async (p) => (prompts.push(p), "新摘要"), window: 4000 });
  const old = user(SUMMARY_PREFIX + "旧摘要：用户要暖色");
  const msgs = [old, ...tenMessages(), user("继续")];
  const out = await c.compact(msgs);
  assert.match(prompts[0], /旧摘要：用户要暖色/);
  assert.match(prompts[0], /之前的摘要/);
  assert.equal(out.filter(isSummaryMessage).length, 1, "只留一条摘要，不会越叠越多");
  assert.match(text(out[0]), /新摘要/);
});

test("压缩：中文按一字一个 token 算，不会被按英文估的低估骗过去", async () => {
  let calls = 0;
  const c = createCompactor({ summarize: async () => (calls++, "x"), window: 1000 });
  // pi 的估算（3.5 字符一个 token）只有 300 左右，但中文实际接近一字一个 token，已经超过 1000 - 150
  const msgs = [user(body("U0", 450)), asst(body("A1", 450)), user(body("U2", 450)), asst(body("A3", 100)), user("好")];
  assert.ok(await c.compact(msgs));
  assert.equal(calls, 1);
});

test("压缩：摘要失败就退回不压缩，并且这个运行片段里不再重试", async () => {
  const errs = [];
  let calls = 0;
  const c = createCompactor({ summarize: async () => { calls++; throw new Error("上游 500"); }, window: 4000, onError: (e) => errs.push(String(e.message)) });
  const msgs = tenMessages();
  assert.equal(await c.compact(msgs), null);
  assert.equal(await c.compact(msgs), null);
  assert.equal(calls, 1);
  assert.deepEqual(errs, ["上游 500"]);
});

test("压缩：摘要为空也算失败；没有可切的位置（一条超长消息）不压缩", async () => {
  const empty = createCompactor({ summarize: async () => "  ", window: 4000 });
  const msgs = tenMessages();
  assert.equal(await empty.compact(msgs), null);
  const one = createCompactor({ summarize: async () => "x", window: 1000 });
  assert.equal(await one.compact([user(body("U", 5000))]), null);
});

test("压缩：摘要请求带上调用方的中止信号", async () => {
  let got;
  const c = createCompactor({ summarize: async (_p, signal) => ((got = signal), "x"), window: 4000 });
  const ac = new AbortController();
  const msgs = tenMessages();
  await c.compact(msgs, ac.signal);
  assert.equal(got, ac.signal);
});

// ---- 接入运行 ----

const history = tenMessages;
const smallWindow = { name: "C", context_window: 4000, max_tokens: 512, vision: false };

test("运行：历史太长时先压缩再请求模型；摘要请求走桥、带内部标记、不带工具；保存的历史是压缩后的", async () => {
  await withBridge({ models: [{ text: "好的" }], summaries: [{ text: "目标：拆分镜" }] }, async (b) => {
    const r = await runAgent(baseInput(b.url, { messages: history(), model: smallWindow }));
    assert.equal(r.status, "done");
    assert.equal(b.rec.summaries.length, 1);
    const purposes = b.rec.headers.map((h) => h["x-agent-purpose"]);
    assert.deepEqual(purposes.filter(Boolean), ["summary"], "只有摘要请求带内部标记，对话请求不带");
    assert.ok(!b.rec.summaries[0].tools?.length, "摘要请求不带工具");
    assert.match(JSON.stringify(b.rec.summaries[0].messages), /U0/);
    const sent = b.rec.chats[0].messages.filter((m) => m.role !== "system");
    assert.match(text(sent[0]), /目标：拆分镜/);
    assert.ok(!JSON.stringify(sent).includes("U0"), "旧消息不再发给模型");
    assert.match(JSON.stringify(sent), /U8/);
    const saved = b.rec.states.at(-1);
    assert.ok(isSummaryMessage(saved[0]), "摘要写进保存的历史，下次不用重新生成");
    assert.ok(!JSON.stringify(saved).includes("U0"));
  });
});

test("运行：压缩过一次，后面的回合不再重复生成摘要", async () => {
  await withBridge({
    models: [{ tools: [{ id: "c1", name: "canvas_get_state", args: {} }] }, { text: "读完了" }],
    summaries: [{ text: "摘要一" }, { text: "不该出现的摘要二" }],
  }, async (b) => {
    await runAgent(baseInput(b.url, { messages: history(), model: smallWindow }));
    assert.equal(b.rec.chats.length, 2);
    assert.equal(b.rec.summaries.length, 1);
  });
});

test("运行：没超线不压缩，没有摘要请求", async () => {
  await withBridge({ models: [{ text: "好" }] }, async (b) => {
    await runAgent(baseInput(b.url));
    assert.equal(b.rec.summaries.length, 0);
  });
});

test("运行：摘要失败（桥返回 500）：运行照常，用原来的历史请求模型", async () => {
  await withBridge({ models: [{ text: "好" }], summaries: [{ status: 500, msg: "上游挂了" }] }, async (b) => {
    const r = await runAgent(baseInput(b.url, { messages: history(), model: smallWindow }));
    assert.equal(r.status, "done");
    assert.equal(b.rec.chats.length, 1);
    assert.match(JSON.stringify(b.rec.chats[0].messages), /U0/);
  });
});
