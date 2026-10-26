import assert from "node:assert/strict";
import { test } from "node:test";
import { decodeRunContext } from "../src/platform-client.ts";
import { withEngine, type Env } from "./engine-helpers.ts";

const MARK = "【节点详情】";
const BIG = MARK + "画布节点详情".repeat(700); // 约 4200 字，约 1200 个 Token；开头有标记，好数有几份
const SMALL_WINDOW = { model: { name: "Claude", context_window: 3000, max_tokens: 500, vision: true } };
const toolTurn = (id: string) => ({ tools: [{ id, name: "canvas_get_state" }] });
/** 每次调用实扣的积分随调用 id 变化，才看得出积分有没有对到各自的调用上 */
const chargedOf = (callId: string) => Number(BigInt(callId) % 7n) + 1;
const usage = (_runId: string, callId: string | undefined, calls: number) => ({
  spent_credits: calls * 3, context_tokens: calls * 100,
  ...(callId ? { call: { id: callId, status: "settled" as const, credits: chargedOf(callId), charged: chargedOf(callId), estimated: false, input_tokens: 100, output_tokens: 20 } } : {}),
});

const text = (v: unknown) => JSON.stringify(v);

test("历史超过触发线：用画布专用的提示词写摘要，请求走平台（带运行上下文和摘要用途），摘要换掉较早的消息", () =>
  withEngine({
    setup: SMALL_WINDOW, turns: [toolTurn("c1"), toolTurn("c2"), toolTurn("c3"), { text: "做完了" }],
    tools: { canvas_get_state: { content: BIG } }, summary: "## 目标\n把剧本拆成分镜\n## 不能丢的数据\nn_script",
  }, async (e) => {
    const run = await e.start();
    await e.engine.idle();
    assert.equal(e.run(run.id).status, "succeeded");

    assert.ok(e.go.summaryRequests.length >= 1, "触发了压缩");
    const sr = e.go.summaryRequests[0];
    assert.equal(sr.headers["x-agent-purpose"], "summary");
    assert.equal(sr.headers["x-agent-service-token"] !== undefined, true);
    assert.equal(decodeRunContext(String(sr.headers["x-agent-context"])).run_id, run.id, "摘要请求带运行上下文，Go 才能计费和检查额度");
    const msgs = sr.body.messages as Array<{ role: string; content: any }>;
    assert.match(text(msgs[0]), /上下文摘要助手/, "系统提示词是画布专用的摘要提示词");
    assert.match(text(msgs.at(-1)), /<对话>[\s\S]*\[用户\][\s\S]*拆分镜/, "把较早的对话序列化给摘要模型");
    assert.match(text(msgs.at(-1)), /## 目标[\s\S]*## 不能丢的数据/, "要求的摘要格式");
    assert.doesNotMatch(text(msgs.at(-1)), /<之前的摘要>/, "第一次压缩没有之前的摘要");
    assert.ok(text(msgs.at(-1)).length < BIG.length * 3, "工具结果序列化时被截短");

    // 压缩后的请求：摘要在，较早的大工具结果被换掉了
    const last = text(e.go.modelRequests.at(-1)!.body.messages);
    assert.match(last, /把剧本拆成分镜[\s\S]*n_script/);
    assert.ok(last.split(MARK).length - 1 < 3, "较早的工具结果不再原样出现（原来有 3 份）");

    // 摘要不是助手的话：没有 message.done，没有增量
    assert.ok(!e.persisted("message.done").some((m) => String(m.text).includes("把剧本拆成分镜")));
    assert.ok(!e.events.some((ev) => ev.type === "message.delta" && String((ev.payload as { text: string }).text).includes("目标")));
  }));

test("摘要调用也要计费并推 run.usage；对话调用的积分不会错配到摘要调用上", () =>
  withEngine({
    setup: SMALL_WINDOW, turns: [toolTurn("c1"), toolTurn("c2"), toolTurn("c3"), { text: "做完了" }],
    tools: { canvas_get_state: { content: BIG } }, usage,
  }, async (e) => {
    const run = await e.start();
    await e.engine.idle();
    const mainIds = e.go.callLog.filter((c) => c.purpose === "main").map((c) => c.callId);
    const summaryIds = e.go.callLog.filter((c) => c.purpose === "summary").map((c) => c.callId);
    assert.ok(summaryIds.length >= 1);
    // 每条 message.done 的积分是它自己那次调用的
    assert.deepEqual(e.persisted("message.done").map((m) => m.credits), mainIds.map(chargedOf));
    // 每次结算（对话或摘要）都有一条 run.usage
    assert.equal(e.persisted("run.usage").length, mainIds.length + summaryIds.length);
    // 上下文大小只看对话调用：摘要调用不改它
    const r = e.run(run.id);
    assert.ok(r.contextTokens > 0);
  }));

test("摘要失败：不压缩，运行照常继续，这个运行片段里不再重试（免得每个回合都白付一次钱）", () =>
  withEngine({
    setup: SMALL_WINDOW, turns: [toolTurn("c1"), toolTurn("c2"), toolTurn("c3"), toolTurn("c4"), { text: "做完了" }],
    tools: { canvas_get_state: { content: BIG } }, summary: () => ({ error: { status: 502, message: "上游调用失败" } }),
  }, async (e) => {
    const run = await e.start();
    await e.engine.idle();
    assert.equal(e.go.summaryRequests.length, 1, "只试了一次");
    assert.equal(e.run(run.id).status, "succeeded");
  }));

test("再次压缩时把之前的摘要并进新摘要（增量更新）", () =>
  withEngine({
    setup: SMALL_WINDOW, turns: [1, 2, 3, 4, 5, 6, 7, 8].map((i) => toolTurn("c" + i)).concat([{ text: "做完了" } as never]),
    tools: { canvas_get_state: { content: BIG } }, summary: (_b, n) => `## 目标\n第 ${n} 版摘要`,
  }, async (e) => {
    await e.start({ maxSteps: 40 });
    await e.engine.idle();
    assert.ok(e.go.summaryRequests.length >= 2, `至少压缩两次（实际 ${e.go.summaryRequests.length}）`);
    const second = text((e.go.summaryRequests[1].body.messages as unknown[]).at(-1));
    assert.match(second, /<之前的摘要>[\s\S]*第 1 版摘要[\s\S]*<\/之前的摘要>/);
  }));

// 前端的压缩提示（设计文档 画布Agent助手设计 6.11）：开始发临时事件 context.compacting，结束发落库事件 context.compacted
test("压缩开始发临时事件（不落库），成功后发落库事件 context.compacted，带压缩前的上下文大小", () =>
  withEngine({
    setup: SMALL_WINDOW, turns: [toolTurn("c1"), toolTurn("c2"), toolTurn("c3"), { text: "做完了" }],
    tools: { canvas_get_state: { content: BIG } }, summary: "## 目标\n把剧本拆成分镜", usage,
  }, async (e) => {
    const run = await e.start();
    await e.engine.idle();
    assert.ok(e.go.summaryRequests.length >= 1, "触发了压缩");

    const starts = e.events.filter((ev) => ev.type === "context.compacting");
    assert.ok(starts.length >= 1);
    assert.ok(starts.every((ev) => ev.seq === 0 && ev.runId === run.id), "临时事件：序号 0，不落库、不参与回放");
    assert.deepEqual(e.persisted("context.compacting"), []);

    const done = e.persisted("context.compacted");
    assert.equal(done.length, starts.length, "每次压缩开始都有结束");
    assert.ok(done.every((d) => typeof d.before === "number" && d.before > 0 && d.failed === undefined), `成功的带 before：${JSON.stringify(done)}`);
    // 先说开始，再说完成
    const order = e.events.filter((ev) => ev.type.startsWith("context.compact")).map((ev) => ev.type);
    assert.deepEqual(order.slice(0, 2), ["context.compacting", "context.compacted"]);
  }));

test("摘要失败：照样发 context.compacted，标 failed，前端据此说「没整理成」", () =>
  withEngine({
    setup: SMALL_WINDOW, turns: [toolTurn("c1"), toolTurn("c2"), toolTurn("c3"), toolTurn("c4"), { text: "做完了" }],
    tools: { canvas_get_state: { content: BIG } }, summary: () => ({ error: { status: 502, message: "上游调用失败" } }), usage,
  }, async (e) => {
    await e.start();
    await e.engine.idle();
    assert.equal(e.go.summaryRequests.length, 1, "只试了一次");
    assert.equal(e.events.filter((ev) => ev.type === "context.compacting").length, 1);
    assert.deepEqual(e.persisted("context.compacted"), [{ failed: true }]);
  }));

test("没有触发压缩：不发任何压缩事件", () =>
  withEngine({ turns: [{ text: "好" }] }, async (e) => {
    await e.start();
    await e.engine.idle();
    assert.equal(e.events.filter((ev) => ev.type.startsWith("context.compact")).length, 0);
  }));
