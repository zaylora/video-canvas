import assert from "node:assert/strict";
import { test } from "node:test";
import type { AgentToolResult } from "@earendil-works/pi-agent-core";
import { buildAllTools, AGENT_TOOLS } from "../src/agent-tools/index.ts";
import type { Bridge } from "../src/bridge-client.ts";
import { runAgent } from "../src/run.ts";
import type { CodemodeLimits, RunInput, ToolEvent, ToolEventReply, ToolReply } from "../src/types.ts";
import { TOOL_DECLS, baseInput, fakeBridge, withBridge } from "./helpers.ts";

const text = (r: AgentToolResult<unknown>): string => r.content.map((p) => ("text" in p ? p.text : "")).join("\n");
const NOT_SCRIPTABLE = new Set(["canvas_delete", "ask_user", "generate_media", "canvas_inspect_image"]);
/** Go 下发的声明：会创建审批的和返回图片的工具标为不可被脚本调用 */
const decls = () => TOOL_DECLS.map((d) => ({ ...d, scriptable: !NOT_SCRIPTABLE.has(d.name) }));

type ToolHandler = (id: string, name: string, args: unknown, signal?: AbortSignal, extra?: { parentToolCallId?: string }) => Promise<Partial<ToolReply>>;

/** 假桥客户端：记录嵌套调用和报备，工具的回复由 handler 决定 */
function fakeBridgeClient({ tool = async (_id, name, args) => ({ content: `${name}:${JSON.stringify(args)}` }), toolEvent = async () => ({ allowed: true }) }: {
  tool?: ToolHandler;
  toolEvent?: (ev: ToolEvent) => Promise<ToolEventReply>;
} = {}) {
  const calls: { id: string; name: string; args: unknown; parent?: string }[] = [];
  const events: ToolEvent[] = [];
  return {
    calls, events,
    tool: async (id: string, name: string, args: unknown, signal?: AbortSignal, extra?: { parentToolCallId?: string }) => { calls.push({ id, name, args, parent: extra?.parentToolCallId }); return tool(id, name, args, signal, extra); },
    toolEvent: async (p: ToolEvent) => { events.push(p); return toolEvent(p); },
  };
}

function make(bridge: ReturnType<typeof fakeBridgeClient>, over: { input?: Partial<RunInput>; onTerminate?: () => void; limits?: Partial<CodemodeLimits> } = {}) {
  const logs: [string, unknown][] = [];
  const tools = buildAllTools({ bridge: bridge as unknown as Bridge, input: { tools: decls(), model: { vision: true }, ...over.input } as RunInput, onTerminate: over.onTerminate ?? (() => {}), log: (m, e) => logs.push([m, e]), limits: over.limits });
  return { tools, logs, codemode: tools.find((t) => t.name === "codemode")! };
}

// ---- Agent 注册表：和 Go 下发的工具合并 ----

test("合并：Go 的工具在前、Agent 的在后；codemode 在有可被脚本调用的工具时启用", () => {
  const { tools } = make(fakeBridgeClient());
  const names = tools.map((t) => t.name);
  assert.deepEqual(names.slice(0, 12), TOOL_DECLS.map((d) => d.name), "Go 的顺序不变，提示词缓存要稳定");
  assert.equal(names.at(-1), "codemode");
  assert.deepEqual(AGENT_TOOLS.map((t) => t.name), ["codemode"]);
});

test("合并：Go 没下发任何可被脚本调用的工具时不启用 codemode", () => {
  const { tools } = make(fakeBridgeClient(), { input: { tools: decls().map((d) => ({ ...d, scriptable: false })) } });
  assert.ok(!tools.some((t) => t.name === "codemode"));
  const { tools: none } = make(fakeBridgeClient(), { input: { tools: [] } });
  assert.deepEqual(none.map((t) => t.name), []);
});

test("合并：停用清单里的 Agent 工具不启用，其他不受影响", () => {
  const { tools } = make(fakeBridgeClient(), { input: { disabled_tools: ["codemode"] } });
  assert.ok(!tools.some((t) => t.name === "codemode"));
  assert.equal(tools.length, 12);
});

test("合并：和 Go 的工具重名时 Go 的优先，Agent 的被跳过并记一条日志", () => {
  const clash = decls().concat([{ name: "codemode", description: "Go 自己的 codemode", parameters: { type: "object", properties: {} }, executor: "server", scriptable: false }]);
  const { tools, logs } = make(fakeBridgeClient(), { input: { tools: clash } });
  const found = tools.filter((t) => t.name === "codemode");
  assert.equal(found.length, 1);
  assert.equal(found[0].description, "Go 自己的 codemode");
  assert.ok(logs.some(([m]) => /重名/.test(m)));
});

test("codemode 的说明：只列可被脚本调用的工具，给出 TypeScript 声明", () => {
  const { codemode } = make(fakeBridgeClient());
  assert.equal(codemode.label, "运行脚本");
  for (const ok of ["canvas_get_state", "canvas_apply_ops", "plan_update", "task_get"]) assert.ok(codemode.description.includes(ok), `${ok} 应在说明里`);
  for (const no of NOT_SCRIPTABLE) assert.ok(!codemode.description.includes(no), `${no} 不能被脚本调用，不该出现在说明里`);
  assert.match(codemode.description, /tools\./);
  assert.match(codemode.description, /60 秒/);
  assert.deepEqual((codemode.parameters as { required?: string[] }).required, ["code"]);
});

// ---- 执行 ----

test("脚本：一次调用多个工具，嵌套调用带父调用 id，结果只给输出和返回值", async () => {
  const bridge = fakeBridgeClient();
  const { codemode } = make(bridge);
  const r = await codemode.execute("cm1", { code: `
    const a = await tools.canvas_get_state({ nodeIds: ["n1"] });
    const b = await tools.task_get({ taskId: "t1" });
    text("读了两次");
    return { a: a.length, b: b.length };
  ` });
  assert.equal(r.isError, false);
  assert.deepEqual(bridge.calls.map((c) => [c.name, c.parent]), [["canvas_get_state", "cm1"], ["task_get", "cm1"]]);
  assert.deepEqual(new Set(bridge.calls.map((c) => c.id)).size, 2, "每次嵌套调用的 id 不同");
  assert.ok(bridge.calls.every((c) => c.id.startsWith("cm1")));
  const out = text(r);
  assert.match(out, /读了两次/);
  assert.match(out, /"a"/);
  assert.match(out, /2 次/, "说明调用了几次工具");
  assert.ok(!out.includes("canvas_get_state:"), "工具的原始结果不进给模型的内容");
});

test("脚本：报备开始和结束，Go 据此在时间线里记「运行脚本」这一步", async () => {
  const bridge = fakeBridgeClient();
  const { codemode } = make(bridge);
  await codemode.execute("cm1", { code: `await tools.canvas_get_state({}); return 1` });
  assert.equal(bridge.events.length, 2);
  // 开始时带上脚本本身：事后分析 Agent 为什么这样写，要看到它写了什么
  assert.deepEqual(bridge.events[0], { phase: "start", tool_call_id: "cm1", name: "codemode", label: "运行脚本", verb: "脚本", args: { code: `await tools.canvas_get_state({}); return 1` } });
  const end = bridge.events[1];
  assert.equal(end.phase, "end");
  assert.equal(end.tool_call_id, "cm1");
  assert.equal(end.is_error, false);
  assert.match(end.summary!, /1 次/);
  // 结束时带上返回给模型的文本，和落库的工具结果一致
  assert.equal(typeof end.result, "string");
  assert.ok(end.result!.length > 0);
});

test("脚本：失败时结束报备也带上返回给模型的错误文本", async () => {
  const bridge = fakeBridgeClient();
  const { codemode } = make(bridge);
  await codemode.execute("cm1", { code: `throw new Error("坏了")` });
  const end = bridge.events[1];
  assert.equal(end.is_error, true);
  assert.match(end.result!, /坏了/);
});

test("脚本：单个值传给数组参数照样自动包成数组（和直接调用工具一致）", async () => {
  const bridge = fakeBridgeClient();
  const { codemode } = make(bridge);
  await codemode.execute("cm1", { code: `await tools.canvas_get_state({ nodeIds: "n1" })` });
  assert.deepEqual(bridge.calls[0].args, { nodeIds: ["n1"] });
});

test("脚本抛错：作为错误结果交回模型，并列出出错前已经完成的调用（它们已经生效，别重复）", async () => {
  const bridge = fakeBridgeClient();
  const { codemode } = make(bridge);
  const r = await codemode.execute("cm1", { code: `
    await tools.canvas_apply_ops({ ops: [] });
    await tools.canvas_arrange({ layout: "row" });
    throw new Error("我故意的");
  ` });
  assert.equal(r.isError, true);
  const out = text(r);
  assert.match(out, /我故意的/);
  assert.match(out, /已完成/);
  assert.match(out, /canvas_apply_ops/);
  assert.match(out, /canvas_arrange/);
  assert.match(out, /不要重复/);
  assert.equal(bridge.events.at(-1)!.is_error, true);
});

test("嵌套工具返回错误：脚本里是一个抛出的 Error，脚本可以接住继续", async () => {
  const bridge = fakeBridgeClient({ tool: async (_id, name) => (name === "task_get" ? { content: "任务不存在", is_error: true } : { content: "ok" }) });
  const { codemode } = make(bridge);
  const r = await codemode.execute("cm1", { code: `
    let msg = "";
    try { await tools.task_get({ taskId: "x" }); } catch (e) { msg = e.message; }
    await tools.canvas_get_state({});
    return msg;
  ` });
  assert.equal(r.isError, false);
  assert.match(text(r), /任务不存在/);
  assert.equal(bridge.calls.length, 2);
});

test("脚本超时：中止并说明，已完成的调用要列出来", async () => {
  const bridge = fakeBridgeClient({ tool: async (_id, name, _a, signal) => (name === "task_get" ? new Promise((_res, rej) => signal!.addEventListener("abort", () => rej(new Error("被中止")))) : { content: "ok" }) });
  const { codemode } = make(bridge, { limits: { timeoutMs: 400 } });
  const r = await codemode.execute("cm1", { code: `await tools.canvas_get_state({}); await tools.task_get({ taskId: "slow" }); return 1` });
  assert.equal(r.isError, true);
  const out = text(r);
  assert.match(out, /超时/);
  assert.match(out, /canvas_get_state/);
});

test("嵌套调用次数超过上限：脚本被中止，原因写清楚", async () => {
  const bridge = fakeBridgeClient();
  const { codemode } = make(bridge, { limits: { maxNestedCalls: 5 } });
  const r = await codemode.execute("cm1", { code: `for (let i = 0; i < 20; i++) await tools.canvas_get_state({}); return "不该到这里"` });
  assert.equal(r.isError, true);
  assert.match(text(r), /上限/);
  assert.equal(bridge.calls.length, 5, "超过上限的调用不发给 Go");
});

test("用户停止：脚本立即中止，不再调用工具", async () => {
  const bridge = fakeBridgeClient({ tool: async (_id, _n, _a, signal) => new Promise((_res, rej) => signal!.addEventListener("abort", () => rej(new Error("被中止")))) });
  const { codemode } = make(bridge);
  const ac = new AbortController();
  setTimeout(() => ac.abort(), 150);
  const start = Date.now();
  const r = await codemode.execute("cm1", { code: `await tools.canvas_get_state({}); await tools.canvas_get_state({}); return 1` }, ac.signal);
  assert.ok(Date.now() - start < 3000);
  assert.equal(r.isError, true);
  assert.match(text(r), /中止/);
  assert.equal(bridge.calls.length, 1);
});

test("步数用尽（报备时 Go 不允许）：不运行脚本，停下等用户继续", async () => {
  let terminated = 0;
  const bridge = fakeBridgeClient({ toolEvent: async () => ({ allowed: false, content: "已达到本轮的步数上限，运行已暂停，等待用户继续。", terminate: true }) });
  const { codemode } = make(bridge, { onTerminate: () => terminated++ });
  const r = await codemode.execute("cm1", { code: `await tools.canvas_get_state({})` });
  assert.equal(r.isError, true);
  assert.equal(r.terminate, true);
  assert.match(text(r), /步数上限/);
  assert.equal(bridge.calls.length, 0);
  assert.equal(terminated, 1);
  assert.equal(bridge.events.length, 1, "没有开始就没有结束的报备");
});

test("脚本里没有网络、文件和进程：只能调工具", async () => {
  const { codemode } = make(fakeBridgeClient());
  const r = await codemode.execute("cm1", { code: `return [typeof fetch, typeof require, typeof process, typeof setTimeout, typeof WebAssembly].join(",")` });
  assert.match(text(r), /undefined,undefined,undefined,undefined,undefined/);
});

test("返回值太大会被截断，免得一个脚本把上下文撑满", async () => {
  const { codemode } = make(fakeBridgeClient());
  const r = await codemode.execute("cm1", { code: `return "长".repeat(100000)` });
  assert.ok(text(r).length < 12000);
  assert.match(text(r), /已截断/);
});

// ---- 接入运行 ----

test("运行：模型调 codemode，嵌套调用经桥带父 id、报备开始和结束，结果回到模型", async () => {
  await withBridge({
    models: [
      { tools: [{ id: "cm1", name: "codemode", args: { code: `const r = await tools.canvas_get_state({ nodeIds: ["a"] }); text("读到 " + r); return 42;` } }] },
      { text: "做完了" },
    ],
    tool: async (j) => ({ content: `结果-${j.name}` }),
  }, async (b) => {
    const r = await runAgent(baseInput(b.url, { tools: decls() }));
    assert.equal(r.status, "done");
    assert.deepEqual(b.rec.chats[0].tools.map((t: any) => t.function.name).at(-1), "codemode", "codemode 作为 Agent 自己的工具下发给模型");
    assert.equal(b.rec.tools.length, 1);
    assert.equal(b.rec.tools[0].name, "canvas_get_state");
    assert.equal(b.rec.tools[0].parent_tool_call_id, "cm1");
    assert.deepEqual(b.rec.toolEvents.map((e) => e.phase), ["start", "end"]);
    const toolMsg = b.rec.chats[1].messages.at(-1);
    assert.equal(toolMsg.role, "tool");
    assert.match(String(toolMsg.content), /读到 结果-canvas_get_state/);
    assert.match(String(toolMsg.content), /42/);
  });
});

test("运行：停用清单带 codemode 时模型看不到它", async () => {
  await withBridge({ models: [{ text: "好" }] }, async (b) => {
    await runAgent(baseInput(b.url, { tools: decls(), disabled_tools: ["codemode"] }));
    assert.ok(!b.rec.chats[0].tools.some((t: any) => t.function.name === "codemode"));
  });
});
