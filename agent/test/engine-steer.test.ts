import assert from "node:assert/strict";
import { test } from "node:test";
import { AgentError } from "../src/engine/errors.ts";
import { withEngine, type Env } from "./engine-helpers.ts";

/**
 * 插话的排队：什么时候被读到、读不到时怎么办（设计文档 画布Agent助手设计 6.10，Agent服务拆分 20.23）。
 * 前端靠三个落库的事件显示：message.user（插话，带 id）、steer.read（这些被 Agent 读到了）、steer.dropped（不会再被读到了）。
 */

const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));
const ASK = { id: "c_ask", name: "ask_user", args: { question: "要几张？" } };

/** 等到条件成立（异步测试用轮询和超时，不用固定睡眠） */
async function until(cond: () => boolean, what: string) {
  for (let i = 0; i < 200 && !cond(); i++) await sleep(20);
  assert.ok(cond(), `等待超时：${what}`);
}

const idsOf = (e: Env, type: string) => e.persisted(type).flatMap((p) => (p.ids as string[]) ?? []);
const steers = (e: Env) => e.persisted("message.user").filter((p) => p.steer);

test("插话带 id；工具跑完、下一次请求之前被读到，这时才发 steer.read，位置在下一条回答之前", () =>
  withEngine({
    turns: [{ tools: [{ id: "c1", name: "canvas_get_state" }] }, { text: "收到" }],
    tools: { canvas_get_state: async () => { await sleep(300); return { content: "ok" }; } },
  }, async (e) => {
    const r = await e.start();
    await until(() => e.go.paths().some((p) => p.includes("/tools/")), "工具开始执行");
    await e.engine.interject("1", r.id, "再暖一点");
    // 工具还没跑完：插话收下了，还没有被读到
    assert.equal(steers(e).length, 1);
    assert.match(String(steers(e)[0].id), /\S/, "插话有 id");
    assert.deepEqual(e.persisted("steer.read"), []);
    await e.engine.idle();

    assert.deepEqual(e.persisted("steer.read"), [{ ids: [steers(e)[0].id] }]);
    const types = e.types();
    const read = types.indexOf("steer.read");
    assert.ok(types.indexOf("message.user", 1) < read, "先收下，后读到");
    assert.ok(types.indexOf("tool.end") < read, "工具跑完之后才读到");
    assert.ok(read < types.lastIndexOf("message.done"), "读到之后才有看了它的那条回答");
    assert.deepEqual(e.persisted("steer.dropped"), []);
  }));

test("连发几条插话，在同一个边界一起读到：只发一条 steer.read，带全部 id", () =>
  withEngine({
    turns: [{ tools: [{ id: "c1", name: "canvas_get_state" }] }, { text: "收到" }],
    tools: { canvas_get_state: async () => { await sleep(300); return { content: "ok" }; } },
  }, async (e) => {
    const r = await e.start();
    await until(() => e.go.paths().some((p) => p.includes("/tools/")), "工具开始执行");
    await e.engine.interject("1", r.id, "再暖一点");
    await e.engine.interject("1", r.id, "加一把剑");
    await e.engine.idle();
    const ids = steers(e).map((s) => s.id);
    assert.equal(new Set(ids).size, 2, "两条插话的 id 不同");
    assert.deepEqual(e.persisted("steer.read"), [{ ids }]);
  }));

test("模型正在写最终回答时插话：回答写完后 Pi 自动再开一轮去读它，不会丢；读到时发 steer.read", () =>
  withEngine({ turns: [{ text: "第一版方案", delayMs: 300 }, { text: "按你的补充改了" }] }, async (e) => {
    const r = await e.start();
    await e.engine.interject("1", r.id, "改成竖屏");
    await e.engine.idle();
    assert.equal(e.go.modelRequests.length, 2);
    assert.match(JSON.stringify(e.go.modelRequests[1].body.messages), /改成竖屏/);
    assert.deepEqual(e.persisted("steer.read"), [{ ids: [steers(e)[0].id] }]);
    assert.ok(e.types().indexOf("steer.read") < e.types().lastIndexOf("message.done"));
    assert.equal(e.run(r.id).status, "succeeded");
  }));

// 真实引擎复现的问题：这一回合的模型回答里有提问，插话是在模型输出期间发的。Pi 在本轮结束的边界把它放进记录并自动开新一轮，
// 运行已经在等回答，模型却又被请求一次、继续说话（用户截图里提问卡片后面冒出一段 Agent 的话）。
test("要用户回答的回合里收到的插话先扣着：等回答期间模型不再说话，不发 steer.read", () =>
  withEngine({ turns: [{ text: "我先问一下", tools: [ASK], delayMs: 300 }, { text: "收到，3 张" }] }, async (e) => {
    const r = await e.start();
    await e.engine.interject("1", r.id, "算了用双剑的");
    await e.engine.idle();
    await sleep(300); // 给 Pi 机会（错误地）再开一轮

    assert.equal(e.run(r.id).status, "waiting_input");
    assert.equal(e.go.modelRequests.length, 1, "还在等回答，模型不能被再请求");
    const types = e.types();
    assert.equal(types.slice(types.indexOf("approval.created")).includes("message.done"), false, "提问之后没有模型的话");
    assert.deepEqual(e.persisted("steer.read"), [], "还没有被读到");
    assert.deepEqual(e.persisted("steer.dropped"), [], "也没有丢，只是在等");
  }));

test("回答之后，扣着的插话和回答一起交给模型：steer.read 发在 approval.decided 之后、下一条回答之前", () =>
  withEngine({ turns: [{ text: "我先问一下", tools: [ASK], delayMs: 300 }, { text: "收到，3 张，用双剑" }] }, async (e) => {
    const r = await e.start();
    await e.engine.interject("1", r.id, "算了用双剑的");
    await e.engine.interject("1", r.id, "再加一张背面");
    await e.engine.idle();
    const [approval] = e.store.listApprovals(r.id);
    await e.engine.decide("1", approval.id, { decision: "approve", answer: "3 张" });
    await e.engine.idle();

    assert.equal(e.go.modelRequests.length, 2);
    const seen = JSON.stringify(e.go.modelRequests[1].body.messages);
    assert.match(seen, /算了用双剑的/);
    assert.match(seen, /再加一张背面/);
    const ids = steers(e).map((s) => s.id);
    assert.deepEqual(e.persisted("steer.read"), [{ ids }], "两条一起读到");
    const types = e.types();
    assert.ok(types.indexOf("approval.decided") < types.indexOf("steer.read"));
    assert.ok(types.indexOf("steer.read") < types.lastIndexOf("message.done"));
    assert.equal(e.run(r.id).status, "succeeded");
  }));

test("等回答期间点停止：扣着的插话发 steer.dropped（canceled），在「已停止」之前；不再被读到", () =>
  withEngine({ turns: [{ text: "我先问一下", tools: [ASK], delayMs: 300 }, { text: "不该走到" }] }, async (e) => {
    const r = await e.start();
    await e.engine.interject("1", r.id, "算了用双剑的");
    await e.engine.idle();
    await e.engine.cancel("1", r.id);
    await sleep(200);

    assert.deepEqual(e.persisted("steer.dropped").map((p) => [p.ids, p.reason]), [[[steers(e)[0].id], "canceled"]]);
    assert.deepEqual(e.persisted("steer.read"), []);
    const types = e.types();
    assert.ok(types.indexOf("steer.dropped") < types.lastIndexOf("run.status"));
    assert.equal(e.go.modelRequests.length, 1);
  }));

test("运行中点停止：排队里还没读到的插话发 steer.dropped（canceled）", () =>
  withEngine({ turns: [{ text: "写得很慢", delayMs: 5000 }] }, async (e) => {
    const r = await e.start();
    await e.engine.interject("1", r.id, "改成竖屏");
    await e.engine.cancel("1", r.id);
    assert.deepEqual(e.persisted("steer.dropped").map((p) => [p.ids, p.reason]), [[[steers(e)[0].id], "canceled"]]);
    assert.deepEqual(e.persisted("steer.read"), []);
  }));

test("运行失败时还没读到的插话：steer.dropped（failed），并且不会留在 Pi 的收件箱里等下一次再读", () =>
  withEngine({ turns: [{ delayMs: 300, error: { status: 500, message: "上游挂了" } }, { text: "不该读到" }] }, async (e) => {
    const r = await e.start();
    await e.engine.interject("1", r.id, "改成竖屏");
    await e.engine.idle();
    assert.equal(e.run(r.id).status, "failed");
    assert.deepEqual(e.persisted("steer.dropped").map((p) => [p.ids, p.reason]), [[[steers(e)[0].id], "failed"]]);
    assert.deepEqual(e.persisted("steer.read"), []);
    const types = e.types();
    assert.ok(types.indexOf("steer.dropped") < types.lastIndexOf("run.status"), "先说没读到，再说失败");
    assert.equal(await e.engine.liveTaskCount(), 0);
  }));

test("等回答期间不能再插话（输入框已被决定框顶替），仍是 60003", () =>
  withEngine({ turns: [{ text: "我先问一下", tools: [ASK] }] }, async (e) => {
    const r = await e.start();
    await e.engine.idle();
    await assert.rejects(e.engine.interject("1", r.id, "晚了"), (err: AgentError) => err.code === 60003);
    assert.deepEqual(steers(e), []);
  }));

test("步数用尽暂停时同样扣着：暂停期间不读，点「继续」时跟着继续的那条输入一起交给模型", () =>
  withEngine({
    turns: [{ tools: [{ id: "c1", name: "canvas_get_state" }] }, { tools: [{ id: "c2", name: "canvas_get_state" }], delayMs: 300 }, { text: "接着做完了" }],
  }, async (e) => {
    const r = await e.start({ maxSteps: 1 });
    await until(() => e.go.modelRequests.length === 2, "第二次请求已发出");
    await e.engine.interject("1", r.id, "用双剑的");
    await e.engine.idle();
    await sleep(300);
    assert.equal(e.run(r.id).status, "step_limit");
    assert.equal(e.go.modelRequests.length, 2, "暂停了，模型不能被再请求");
    assert.deepEqual(e.persisted("steer.read"), []);

    await e.engine.resume("1", r.id);
    await e.engine.idle();
    assert.match(JSON.stringify(e.go.modelRequests[2].body.messages), /用双剑的/);
    assert.deepEqual(e.persisted("steer.read"), [{ ids: [steers(e)[0].id] }]);
    assert.equal(e.run(r.id).status, "succeeded");
  }));
