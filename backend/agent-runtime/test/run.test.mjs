import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import { test } from "node:test";
import { runAgent } from "../src/run.mjs";
import { baseInput, fakeBridge, sleep } from "./helpers.mjs";

const withBridge = async (opts, fn) => { const b = await fakeBridge(opts); try { await fn(b); } finally { await b.close(); } };

test("请求形状：走桥、带令牌、流式带用量、max_tokens 而不是 max_completion_tokens、没有 store", async () => {
  await withBridge({ models: [{ text: "好" }] }, async (b) => {
    const r = await runAgent(baseInput(b.url));
    assert.deepEqual(r, { status: "done", message: "" });
    const body = b.rec.chats[0];
    assert.equal(b.rec.auths[0], "Bearer tok-123");
    assert.equal(body.stream, true);
    assert.ok(body.stream_options.include_usage);
    assert.equal(body.max_tokens, 8192);
    assert.ok(!("store" in body) && !("max_completion_tokens" in body));
    assert.equal(body.messages.filter((m) => m.role === "system").length, 1, "系统提示只能有一条");
  });
});

test("每个回合结束把对话历史交回 Go，最后报告完成", async () => {
  await withBridge({ models: [{ text: "好" }] }, async (b) => {
    await runAgent(baseInput(b.url));
    assert.ok(b.rec.states.length >= 1);
    const last = b.rec.states.at(-1);
    assert.deepEqual(last.map((m) => m.role), ["user", "assistant"]);
    assert.deepEqual(b.rec.finishes, [{ status: "done", message: "" }]);
  });
});

test("工具往返：参数拼完整、回调 Go、结果回传模型", async () => {
  await withBridge({
    models: [{ tools: [{ id: "call_1", name: "canvas_get_state", args: { nodeIds: ["a"] } }] }, { text: "读完了" }],
    tool: async () => ({ content: "画布有 3 个节点", is_error: false, terminate: false }),
  }, async (b) => {
    await runAgent(baseInput(b.url));
    assert.deepEqual(b.rec.tools, [{ tool_call_id: "call_1", name: "canvas_get_state", args: { nodeIds: ["a"] } }]);
    const toolMsg = b.rec.chats[1].messages.at(-1);
    assert.equal(toolMsg.role, "tool");
    assert.equal(toolMsg.tool_call_id, "call_1");
    assert.match(toolMsg.content, /画布有 3 个节点/);
  });
});

test("工具返回 terminate：整批执行完后停下，不再请求模型（等审批）", async () => {
  await withBridge({
    models: [{ tools: [{ id: "c1", name: "canvas_delete", args: { nodeIds: ["a"], reason: "废稿" } }] }, { text: "不该走到这里" }],
    tool: async () => ({ content: "已提交给用户确认，等待决定。", is_error: false, terminate: true }),
  }, async (b) => {
    const r = await runAgent(baseInput(b.url));
    assert.equal(b.rec.chats.length, 1, "terminate 后不能再调模型");
    assert.equal(r.status, "paused", "因工具要求停下而结束要报 paused：Go 据此不改运行状态，避免误伤已经续跑的新片段");
    assert.deepEqual(b.rec.finishes.at(-1), { status: "paused", message: "" });
    assert.equal(b.rec.states.at(-1).at(-1).role, "toolResult", "历史停在工具结果上，等审批后续跑");
  });
});

test("工具层失败（is_error）：模型收到错误内容并继续", async () => {
  await withBridge({
    models: [{ tools: [{ id: "c1", name: "canvas_apply_ops", args: { ops: [] } }] }, { text: "我改一下" }],
    tool: async () => ({ content: "操作没有生效：第 1 项：节点 \"x\" 不存在", is_error: true, terminate: false }),
  }, async (b) => {
    const r = await runAgent(baseInput(b.url));
    assert.equal(r.status, "done");
    assert.equal(b.rec.chats.length, 2);
    assert.match(b.rec.chats[1].messages.at(-1).content, /不存在/);
  });
});

test("桥回调失败（500）：模型看到错误，运行不中断", async () => {
  await withBridge({
    models: [{ tools: [{ id: "c1", name: "model_list", args: { kind: "image" } }] }, { text: "好吧" }],
    tool: async () => { throw new Error("画布写入冲突"); },
  }, async (b) => {
    const r = await runAgent(baseInput(b.url));
    assert.equal(r.status, "done");
    assert.match(b.rec.chats[1].messages.at(-1).content, /画布写入冲突/);
  });
});

test("allowed_tools：只把当前模式允许的工具声明给模型", async () => {
  await withBridge({ models: [{ text: "好" }] }, async (b) => {
    await runAgent(baseInput(b.url, { allowed_tools: ["canvas_get_state", "plan_update"] }));
    assert.deepEqual(b.rec.chats[0].tools.map((t) => t.function.name).sort(), ["canvas_get_state", "plan_update"]);
  });
  await withBridge({ models: [{ text: "好" }] }, async (b) => {
    await runAgent(baseInput(b.url));
    assert.equal(b.rec.chats[0].tools.length, 11, "不限制时声明全部工具");
  });
});

test("工具声明里带参数的 JSON Schema 和中文说明", async () => {
  await withBridge({ models: [{ text: "好" }] }, async (b) => {
    await runAgent(baseInput(b.url));
    const t = b.rec.chats[0].tools.find((x) => x.function.name === "canvas_apply_ops").function;
    assert.match(t.description, /30 项/);
    assert.equal(t.parameters.properties.ops.type, "array");
    assert.ok(t.parameters.properties.ops.items.properties.op.anyOf || t.parameters.properties.ops.items.properties.op.enum);
  });
});

test("历史恢复：新进程接着之前的对话", async () => {
  let history;
  await withBridge({ models: [{ text: "第一轮" }] }, async (b) => {
    await runAgent(baseInput(b.url));
    history = b.rec.states.at(-1);
  });
  await withBridge({ models: [{ text: "第二轮" }] }, async (b) => {
    await runAgent(baseInput(b.url, { messages: history, prompt: { text: "继续" } }));
    const msgs = b.rec.chats[0].messages;
    assert.deepEqual(msgs.map((m) => m.role), ["system", "user", "assistant", "user"], "系统提示不能因为恢复历史而重复");
    assert.match(JSON.stringify(msgs), /第一轮/);
  });
});

test("审批后续跑（continue）：模型看到的是真实结果，不是等待占位", async () => {
  let history;
  await withBridge({
    models: [{ tools: [{ id: "c1", name: "canvas_delete", args: { nodeIds: ["a"], reason: "x" } }] }],
    tool: async () => ({ content: "已提交给用户确认，等待决定。", terminate: true }),
  }, async (b) => {
    await runAgent(baseInput(b.url));
    history = b.rec.states.at(-1);
  });
  await withBridge({ models: [{ text: "已按你的决定删除" }] }, async (b) => {
    const r = await runAgent(baseInput(b.url, { mode: "continue", messages: history, tool_result: { tool_call_id: "c1", content: "用户已批准，已删除 1 个节点" } }));
    assert.equal(r.status, "done");
    const toolMsg = b.rec.chats[0].messages.findLast((m) => m.role === "tool");
    assert.match(toolMsg.content, /已批准，已删除 1 个节点/);
    assert.doesNotMatch(JSON.stringify(b.rec.chats[0].messages), /等待决定/);
  });
});

test("续跑时找不到对应的工具调用：报错而不是乱接", async () => {
  await withBridge({ models: [{ text: "x" }] }, async (b) => {
    const r = await runAgent(baseInput(b.url, { mode: "continue", messages: [{ role: "user", content: [{ type: "text", text: "hi" }], timestamp: 1 }], tool_result: { tool_call_id: "ghost", content: "x" } }));
    assert.equal(r.status, "error");
    assert.match(r.message, /ghost/);
    assert.deepEqual(b.rec.finishes.at(-1).status, "error");
  });
});

test("resume：去掉结尾中断的 assistant，从最后一条用户消息接着走", async () => {
  const history = [
    { role: "user", content: [{ type: "text", text: "拆分镜" }], timestamp: 1 },
    { role: "assistant", content: [{ type: "text", text: "说到一半" }], api: "openai-completions", provider: "bridge", model: "agent",
      usage: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, totalTokens: 0, cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 } }, stopReason: "aborted", timestamp: 2 },
  ];
  await withBridge({ models: [{ text: "接着做" }] }, async (b) => {
    const r = await runAgent(baseInput(b.url, { mode: "resume", messages: history }));
    assert.equal(r.status, "done");
    assert.deepEqual(b.rec.chats[0].messages.map((m) => m.role), ["system", "user"]);
  });
});

test("插话（steer）：当前工具结束后注入，模型下一次请求能看到", async () => {
  await withBridge({
    models: [{ tools: [{ id: "c1", name: "model_list", args: { kind: "image" } }] }, { text: "收到" }],
    tool: async () => { await sleep(150); return { content: "ok" }; },
  }, async (b) => {
    const control = new EventEmitter();
    const run = runAgent(baseInput(b.url), { control });
    await sleep(80);
    control.emit("steer", "再暖一点");
    await run;
    assert.match(JSON.stringify(b.rec.chats[1].messages), /再暖一点/);
  });
});

test("中止（abort）：连接断开，运行报告完成（状态由 Go 管）", async () => {
  await withBridge({ models: [{ text: "很长的回答".repeat(40), slow: 20 }] }, async (b) => {
    const control = new EventEmitter();
    const run = runAgent(baseInput(b.url), { control });
    await sleep(150);
    control.emit("abort");
    const r = await run;
    await sleep(100);
    assert.ok(b.rec.closed >= 1, "桥这一侧要能看到连接断开，才能按已产生的用量结算");
    assert.equal(r.status, "done");
    assert.equal(b.rec.finishes.length, 1);
  });
});

test("模型报错（预算用尽 402）：报告 error 并带上原因", async () => {
  await withBridge({ models: [{ status: 402, msg: "超出本轮积分预算" }] }, async (b) => {
    const r = await runAgent(baseInput(b.url));
    assert.equal(r.status, "error");
    assert.match(r.message, /超出本轮积分预算/);
    assert.equal(b.rec.chats.length, 1, "不能自动重试：每次尝试都会计费");
    assert.equal(b.rec.finishes.at(-1).status, "error");
  });
});

test("历史保存失败（令牌无效）：只记日志，运行照常报告", async () => {
  await withBridge({ models: [{ text: "好" }], stateStatus: 401 }, async (b) => {
    const logs = [];
    const r = await runAgent(baseInput(b.url), { log: (m) => logs.push(m) });
    assert.equal(r.status, "done");
    assert.ok(logs.some((m) => m.includes("保存对话历史失败")));
  });
});

test("上下文裁剪：超过窗口的 70% 时较早的工具结果被换成占位文字", async () => {
  const big = "长".repeat(6000);
  await withBridge({
    models: [
      { tools: [{ id: "a", name: "canvas_get_state", args: {} }] },
      { tools: [{ id: "b", name: "canvas_get_state", args: {} }] },
      { tools: [{ id: "c", name: "canvas_get_state", args: {} }] },
      { tools: [{ id: "d", name: "canvas_get_state", args: {} }] },
      { text: "完成" },
    ],
    tool: async () => ({ content: big }),
  }, async (b) => {
    // 窗口很小，让 4 个大结果必然超出
    await runAgent(baseInput(b.url, { model: { name: "m", context_window: 12000, max_tokens: 1000, vision: false } }));
    const last = b.rec.chats.at(-1).messages.filter((m) => m.role === "tool");
    assert.ok(last.some((m) => String(m.content).includes("已省略")), "较早的工具结果应被省略");
    assert.ok(!String(last.at(-1).content).includes("已省略"), "最近的工具结果不能被省略");
  });
});

test("多个工具按顺序执行（不并发）", async () => {
  const order = [];
  await withBridge({
    models: [{ tools: [{ id: "a", name: "model_list", args: { kind: "image" } }, { id: "b", name: "model_list", args: { kind: "video" } }] }, { text: "ok" }],
    tool: async (j) => { order.push(j.tool_call_id + ":start"); await sleep(j.tool_call_id === "a" ? 80 : 1); order.push(j.tool_call_id + ":end"); return { content: "x" }; },
  }, async (b) => {
    await runAgent(baseInput(b.url));
    assert.deepEqual(order, ["a:start", "a:end", "b:start", "b:end"]);
  });
});

test("看图：启动消息带图片 → 请求里是 image_url 分段", async () => {
  await withBridge({ models: [{ text: "看到了" }] }, async (b) => {
    await runAgent(baseInput(b.url, { prompt: { text: "看这张图", images: [{ data: "AAAA", mime_type: "image/png" }] } }));
    const user = b.rec.chats[0].messages.find((m) => m.role === "user");
    assert.ok(user.content.some((p) => p.type === "image_url"));
  });
});
