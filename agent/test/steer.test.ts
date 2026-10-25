import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import { test } from "node:test";
import { runAgent } from "../src/run.ts";
import type { AgentMessage } from "@earendil-works/pi-agent-core";
import type { Msg } from "../src/context.ts";
import { baseInput, fakeBridge, sleep, text, withBridge } from "./helpers.ts";

const users = (msgs: Msg[]): string[] => msgs.filter((m) => m.role === "user").map(text);

test("工具执行时连发两条插话：不打断，下一次请求里一起带上，只多一次模型调用", async () => {
  await withBridge({
    models: [{ tools: [{ id: "c1", name: "model_list", args: { kind: "image" } }] }, { text: "收到" }],
    tool: async () => { await sleep(250); return { content: "ok" }; },
  }, async (b) => {
    const control = new EventEmitter();
    const run = runAgent(baseInput(b.url), { control });
    for (let i = 0; i < 250 && b.rec.tools.length === 0; i++) await sleep(20); // 等工具真的开始了再插话（机器忙时固定等待不可靠）
    control.emit("steer", "再暖一点");
    await sleep(40);
    control.emit("steer", "改成竖屏");
    const r = await run;
    assert.equal(r.status, "done");
    assert.equal(b.rec.chats.length, 2, "两条插话合并进同一次请求，不是各答一遍");
    assert.equal(b.rec.tools.length, 1, "正在执行的工具照常跑完");
    const sent = users(b.rec.chats[1].messages).join("\n");
    assert.match(sent, /再暖一点/);
    assert.match(sent, /改成竖屏/);
  });
});

test("插话带说明：告诉模型这是对当前任务的补充，要一起回应", async () => {
  await withBridge({
    models: [{ tools: [{ id: "c1", name: "model_list", args: { kind: "image" } }] }, { text: "收到" }],
    tool: async () => { await sleep(150); return { content: "ok" }; },
  }, async (b) => {
    const control = new EventEmitter();
    const run = runAgent(baseInput(b.url), { control });
    await sleep(80);
    control.emit("steer", "再暖一点");
    await run;
    const steer = users(b.rec.chats[1].messages).find((t) => t.includes("再暖一点"))!;
    assert.match(steer, /用户在你执行任务时补充/);
    assert.match(steer, /同一次回复/);
  });
});

test("模型正在输出时插话：中止半截回答，带着新旧两句话重答；半截回答不进历史", async () => {
  await withBridge({
    models: [{ text: "很长的回答".repeat(40), slow: 20 }, { text: "结合后的回答" }],
  }, async (b) => {
    const control = new EventEmitter();
    const run = runAgent(baseInput(b.url), { control });
    await sleep(150);
    control.emit("steer", "改成竖屏");
    const r = await run;
    assert.equal(r.status, "done", "内部中止不能被报成出错");
    assert.equal(b.rec.finishes.length, 1);
    assert.equal(b.rec.finishes[0].status, "done");
    assert.equal(b.rec.chats.length, 2);
    const second = b.rec.chats[1].messages;
    assert.deepEqual(users(second).slice(0, 1), ["拆分镜"], "原请求还在");
    assert.match(users(second).at(-1)!, /改成竖屏/, "插话排在原请求之后");
    assert.ok(!second.some((m: Msg) => m.role === "assistant"), "半截回答不交给模型");
    const saved = b.rec.states.at(-1)!;
    assert.ok(!saved.some((m: Msg) => m.stopReason === "aborted"), "保存的历史里没有被中止的消息");
    assert.match(JSON.stringify(saved.at(-1)), /结合后的回答/);
  });
});

test("模型输出时连插两句：只中止一次，重答时两句都在", async () => {
  await withBridge({
    models: [{ text: "很长的回答".repeat(40), slow: 20 }, { text: "好" }],
  }, async (b) => {
    const control = new EventEmitter();
    const run = runAgent(baseInput(b.url), { control });
    await sleep(150);
    control.emit("steer", "改成竖屏");
    control.emit("steer", "再加一个角色");
    await run;
    assert.equal(b.rec.chats.length, 2);
    const sent = users(b.rec.chats[1].messages).join("\n");
    assert.match(sent, /改成竖屏/);
    assert.match(sent, /再加一个角色/);
  });
});

test("用户点停止仍然正常停止，不会因为插话逻辑又重启一轮", async () => {
  await withBridge({ models: [{ text: "很长的回答".repeat(40), slow: 20 }, { text: "不该出现" }] }, async (b) => {
    const control = new EventEmitter();
    const run = runAgent(baseInput(b.url), { control });
    await sleep(150);
    control.emit("steer", "改成竖屏");
    control.emit("abort");
    const r = await run;
    assert.equal(r.status, "done");
    assert.equal(b.rec.chats.length, 1, "停止之后不再请求模型");
  });
});

test("暂停等回答时队列里还有插话：不丢，追加进历史，续跑时模型看得到", async () => {
  await withBridge({
    models: [{ tools: [{ id: "c1", name: "ask_user", args: { question: "要哪种风格？", kind: "choice", options: ["暖", "冷"] } }] }],
    tool: async () => { await sleep(200); return { content: "已提交，等待用户回答", terminate: true }; },
  }, async (b) => {
    const control = new EventEmitter();
    const run = runAgent(baseInput(b.url), { control });
    await sleep(80);
    control.emit("steer", "对了，要竖屏");
    const r = await run;
    assert.equal(r.status, "paused");
    assert.equal(b.rec.chats.length, 1, "暂停时不再请求模型");
    const saved = b.rec.states.at(-1)!;
    assert.equal(saved.at(-1).role, "user", "插话排在工具结果之后");
    assert.equal(saved.at(-2).role, "toolResult");
    assert.match(text(saved.at(-1)), /对了，要竖屏/);
    assert.match(text(saved.at(-1)), /用户在你执行任务时补充/);
  });
});

test("模型输出时间隔不远的两条插话：500ms 窗口内合并，只中止一次、只多一次模型调用", async () => {
  await withBridge({
    models: [{ text: "很长的回答".repeat(40), slow: 20 }, { text: "好" }],
  }, async (b) => {
    const control = new EventEmitter();
    const run = runAgent(baseInput(b.url), { control });
    await sleep(150);
    control.emit("steer", "改成竖屏");
    await sleep(300);
    control.emit("steer", "再加一个角色");
    const r = await run;
    assert.equal(r.status, "done");
    assert.equal(b.rec.chats.length, 2, "第二条没有再触发一次中止和重答");
    const sent = users(b.rec.chats[1].messages).join("\n");
    assert.match(sent, /改成竖屏/);
    assert.match(sent, /再加一个角色/);
  });
});

test("窗口内回答自己结束了：不中止，已完成的回答留在历史里，插话随下一轮注入", async () => {
  await withBridge({
    models: [{ text: "简短回答", slow: 10 }, { text: "收到" }],
  }, async (b) => {
    const control = new EventEmitter();
    const run = runAgent(baseInput(b.url), { control });
    await sleep(20);
    control.emit("steer", "补充一句");
    const r = await run;
    assert.equal(r.status, "done");
    assert.equal(b.rec.chats.length, 2);
    const second = b.rec.chats[1].messages;
    assert.ok(second.some((m: Msg) => m.role === "assistant" && /简短回答/.test(text(m))), "完整的回答不丢");
    assert.match(users(second).at(-1)!, /补充一句/);
  });
});

// ---- 慢模型：请求已经发出、响应头还没回来的这段时间也要能合并插话 ----
// pi 要等响应头到了才发 message_start，慢模型（排队、首字很慢）这段可能要等十几秒；
// 只看「正在输出」的话插话会排队，等第一个回答写完才处理，用户就看到两段分开的回答。

test("慢模型：响应头还没回来时插话，也中止这次请求并合并重答，不等它写完", async () => {
  await withBridge({
    models: [{ text: "第一个回答", delay: 3000 }, { text: "合并后的回答" }],
  }, async (b) => {
    const control = new EventEmitter();
    const start = Date.now();
    const run = runAgent(baseInput(b.url), { control });
    await sleep(300);
    control.emit("steer", "你是谁");
    const r = await run;
    assert.equal(r.status, "done");
    assert.ok(Date.now() - start < 2500, `不该等慢请求回来：用了 ${Date.now() - start}ms`);
    assert.equal(b.rec.chats.length, 2);
    const second = b.rec.chats[1].messages;
    assert.deepEqual(users(second).slice(0, 1), ["拆分镜"]);
    assert.match(users(second).at(-1)!, /你是谁/);
    assert.ok(!second.some((m: Msg) => m.role === "assistant"), "被中止的请求没有任何内容进历史");
    assert.match(JSON.stringify(b.rec.states.at(-1)!.at(-1)), /合并后的回答/);
  });
});

test("慢模型：连着两句插话（都在响应头回来之前），合并成一次中止", async () => {
  await withBridge({
    models: [{ text: "第一个回答", delay: 3000 }, { text: "好" }],
  }, async (b) => {
    const control = new EventEmitter();
    const start = Date.now();
    const run = runAgent(baseInput(b.url), { control });
    await sleep(200);
    control.emit("steer", "你是谁");
    await sleep(250);
    control.emit("steer", "再加一个角色");
    await run;
    assert.ok(Date.now() - start < 2500, `不该等慢请求回来：用了 ${Date.now() - start}ms`);
    assert.equal(b.rec.chats.length, 2);
    const second = b.rec.chats[1].messages;
    assert.ok(!second.some((m: Msg) => m.role === "assistant"), "第一次请求被中止，没有内容进历史");
    const sent = users(second).join("\n");
    assert.match(sent, /你是谁/);
    assert.match(sent, /再加一个角色/);
  });
});

test("压缩（摘要）期间收到插话：摘要不被打断，请求一发出就按窗口中止并合并重答", async () => {
  const history = Array.from({ length: 10 }, (_, i) => ({
    role: i % 2 ? "assistant" : "user",
    content: [{ type: "text", text: (i % 2 ? "A" : "U") + i + "字".repeat(358) }],
    ...(i % 2 ? { api: "openai-completions", provider: "bridge", model: "agent", usage: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, totalTokens: 0, cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 } }, stopReason: "stop" } : {}),
    timestamp: 1,
  })) as AgentMessage[];
  await withBridge({
    models: [{ text: "第一个回答", delay: 3000 }, { text: "合并后的回答" }],
    summaries: [{ text: "目标：拆分镜", delay: 600 }],
  }, async (b) => {
    const control = new EventEmitter();
    const start = Date.now();
    const run = runAgent(baseInput(b.url, { messages: history, model: { name: "C", context_window: 4000, max_tokens: 512, vision: false } }), { control });
    await sleep(200); // 摘要还在生成
    control.emit("steer", "你是谁");
    const r = await run;
    assert.equal(r.status, "done");
    assert.ok(Date.now() - start < 3000, `用了 ${Date.now() - start}ms`);
    assert.equal(b.rec.summaries.length, 1, "摘要照常生成完，没有被插话打断");
    assert.equal(b.rec.chats.length, 2);
    assert.match(users(b.rec.chats[1].messages).at(-1)!, /你是谁/);
    assert.match(JSON.stringify(b.rec.chats[1].messages), /目标：拆分镜/, "重答用的是压缩后的历史");
  });
});
