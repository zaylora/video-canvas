import assert from "node:assert/strict";
import { test } from "node:test";
import { Harness, MemoryStorage, createRegistry } from "@earendil-works/pi-durable";
import { openNodeSqliteStorage } from "@earendil-works/pi-durable/storage/sqlite/node";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createPlatform, type ApprovalKind, type RunContext, type ToolDecl, type ToolEndEvent } from "../src/platform-client.ts";
import { createToolsExtension, replayOf, type ToolCallInfo, type ToolHost, type ToolRun } from "../src/durable-tools.ts";
import { callTools, context, fauxAssistantMessage, fauxModels, textOf } from "./durable-helpers.ts";
import { fakePlatform } from "./platform-fake.ts";

const TOKEN = "service-token-0123456789abcdef0123456789";
const rc: RunContext = { run_id: "1152921504606846977", user_id: "1", session_id: "1152921504606846978", canvas_id: "7", mode: "all", model_key: "claude" };
const run: ToolRun = { id: rc.run_id, rc };

const decl = (name: string, verb = "动词", parameters: Record<string, unknown> = { type: "object", properties: {} }): ToolDecl => ({ name, label: "标签-" + name, verb, description: "说明 " + name, parameters, executor: "server", scriptable: false });
const DECLS: ToolDecl[] = [
  decl("canvas_get_state", "读取", { type: "object", properties: { nodeIds: { type: "array", items: { type: "string" } } } }),
  decl("canvas_apply_ops"), decl("canvas_capture_frame"), decl("canvas_inspect_image"), decl("web_search"),
  decl("plan_update", "计划", { type: "object", properties: { steps: { type: "array", items: { type: "object" } } } }),
  decl("generate_media"), decl("canvas_delete", "删除", { type: "object", properties: { nodeIds: { type: "array", items: { type: "string" } } } }), decl("ask_user"),
];

/** 假宿主：记录工具层对运行引擎的所有请求，可以设定步数上限和暂停 */
function fakeHost(over: Partial<{ blockWith: { content: string; terminate: boolean }; paused: () => boolean }> = {}) {
  const log: string[] = [];
  const begun: ToolCallInfo[] = [];
  const ended: Array<{ call: ToolCallInfo; event: ToolEndEvent }> = [];
  const plans: Array<Array<{ title: string; status: string }>> = [];
  const approvals: Array<{ kind: ApprovalKind; toolCallId: string; payload: unknown; quoteCredits: number; summary: string }> = [];
  let paused = false;
  const host: ToolHost = {
    runOf: () => run,
    isPaused: () => over.paused?.() ?? paused,
    begin: async (_r, call) => { log.push("begin:" + call.name); begun.push(call); return over.blockWith; },
    end: async (_r, call, event) => { log.push("end:" + call.name); ended.push({ call, event }); },
    plan: async (_r, steps) => { log.push("plan"); plans.push(steps); },
    approval: async (_r, a) => { log.push("approval:" + a.kind); approvals.push(a); paused = true; },
  };
  return { host, log, begun, ended, plans, approvals };
}

async function withEnv(steps: Parameters<typeof fauxModels>[0], hostOver: Parameters<typeof fakeHost>[0], fn: (e: Env) => Promise<void>) {
  const f = await fakePlatform();
  const h = fakeHost(hostOver);
  const m = fauxModels(steps);
  const registry = createRegistry();
  registry.install(createToolsExtension({ platform: createPlatform({ baseUrl: f.url, serviceToken: TOKEN }), decls: DECLS, host: h.host }));
  const harness = await Harness.open(new MemoryStorage(), { models: m.models, registry }, context);
  const root = await harness.root(context, { agent: { model: m.model } });
  const ask = async (text = "开始") => (await (await root.submit({ type: "input", content: text }, context)).wait(context));
  try {
    await fn({ f, h, m, harness, root, ask });
  } finally {
    await harness.close(context);
    await f.close();
  }
}
interface Env {
  f: Awaited<ReturnType<typeof fakePlatform>>;
  h: ReturnType<typeof fakeHost>;
  m: ReturnType<typeof fauxModels>;
  harness: Harness;
  root: Awaited<ReturnType<Harness["root"]>>;
  ask: (text?: string) => Promise<{ status: string }>;
}

const toolReply = (content: string, extra: Record<string, unknown> = {}, endExtra: Record<string, unknown> = {}) => ({
  json: { code: 0, data: { content, is_error: false, terminate: false, tool_end: { id: "x", name: "x", is_error: false, summary: "摘要", label: "L", verb: "V", ...endExtra }, ...extra } },
});

test("服务端工具：调用平台，幂等键是 Pi 的工具任务 id，结果交给模型；宿主先 begin 后 end，end 的事件内容是 Go 给的 tool_end", () =>
  withEnv([callTools({ id: "call_0", name: "canvas_get_state", args: { nodeIds: ["n1"] } }), fauxAssistantMessage("好了")], {}, async (e) => {
    e.f.reply("POST /internal/platform/v1/tools/canvas_get_state", toolReply("画布里有 3 个节点", {}, { id: "call_0", name: "canvas_get_state", summary: "读取 3 个节点", node_ids: ["n1"] }));
    assert.equal((await e.ask()).status, "done");
    const s = e.f.seen[0];
    assert.deepEqual(s.body, { tool_call_id: "call_0", args: { nodeIds: ["n1"] } });
    assert.ok(s.headers["x-idempotency-key"], "必须带幂等键");
    assert.match(String(s.headers["x-idempotency-key"]), /^[A-Za-z0-9._:-]{1,64}$/, "Pi 的工具任务 id 符合 Go 对幂等键的格式要求");
    assert.equal(e.h.log.join(","), "begin:canvas_get_state,end:canvas_get_state");
    assert.deepEqual(e.h.begun[0], { id: "call_0", name: "canvas_get_state", label: "标签-canvas_get_state", verb: "读取", parentId: undefined, args: { nodeIds: ["n1"] } });
    assert.equal(e.h.ended[0].event.summary, "读取 3 个节点");
    assert.deepEqual(e.h.ended[0].event.node_ids, ["n1"]);
    // 模型下一次请求看到的工具结果就是平台给的文字
    const result = e.m.seen[1].find((m) => m.role === "toolResult")!;
    assert.equal(textOf(result), "画布里有 3 个节点");
  }));

test("平台说工具失败（is_error）：模型看到的是错误结果，宿主收到的 tool.end 也是错误", () =>
  withEnv([callTools({ id: "c1", name: "canvas_apply_ops" }), fauxAssistantMessage("我改一下")], {}, async (e) => {
    e.f.reply("POST /internal/platform/v1/tools/canvas_apply_ops", { json: { code: 0, data: { content: "操作没有生效：第 2 项缺少 id", is_error: true, terminate: false, tool_end: { id: "c1", name: "canvas_apply_ops", is_error: true, summary: "操作没有生效" } } } });
    await e.ask();
    const result = e.m.seen[1].find((m) => m.role === "toolResult")! as { isError?: boolean };
    assert.equal(result.isError, true);
    assert.match(textOf(result as never), /第 2 项缺少 id/);
    assert.equal(e.h.ended[0].event.is_error, true);
  }));

test("图片随结果交给模型；对象存储里的图片只有地址，放在 data 里，发请求前由提供方还原", () =>
  withEnv([callTools({ id: "c1", name: "canvas_inspect_image" }), fauxAssistantMessage("看到了")], {}, async (e) => {
    e.f.reply("POST /internal/platform/v1/tools/canvas_inspect_image", toolReply("图片信息", { images: [{ mime_type: "image/png", data: "QUJD" }, { mime_type: "image/jpeg", url: "https://oss.example/a.jpg" }] }));
    await e.ask();
    const result = e.m.seen[1].find((m) => m.role === "toolResult")! as { content: Array<{ type: string; data?: string; mimeType?: string }> };
    const imgs = result.content.filter((p) => p.type === "image");
    assert.deepEqual(imgs.map((p) => [p.data, p.mimeType]), [["QUJD", "image/png"], ["https://oss.example/a.jpg", "image/jpeg"]]);
  }));

test("重放策略：读工具和 Go 已经幂等的写工具可重放；会写素材的截帧不可；没登记的工具默认不可", () => {
  for (const n of ["canvas_get_state", "model_list", "task_get", "skill_search", "skill_read", "canvas_inspect_image", "canvas_inspect_video", "web_search", "web_fetch", "canvas_apply_ops", "canvas_arrange"]) {
    assert.equal(replayOf(n), "safe", n);
  }
  for (const n of ["canvas_capture_frame", "something_new"]) assert.equal(replayOf(n), "unsafe", n);
  // 审批类和计划：宿主按调用 id 幂等，可重放
  for (const n of ["plan_update", "generate_media", "canvas_delete", "ask_user"]) assert.equal(replayOf(n), "safe", n);
});

test("步数上限：宿主拒绝开始时不调平台，模型收到宿主给的文字，本轮停下", () =>
  withEnv([callTools({ id: "c1", name: "canvas_get_state" })], { blockWith: { content: "已达到本轮的步数上限，运行已暂停，等待用户继续。", terminate: true } }, async (e) => {
    await e.ask();
    assert.equal(e.f.seen.length, 0, "没有调平台");
    assert.equal(e.h.log.join(","), "begin:canvas_get_state", "被拒绝的调用没有 end，宿主在 begin 里已经处理");
    assert.equal(e.m.seen.length, 1, "terminate：这一轮结束后不再请求模型");
  }));

test("本轮已暂停（等审批、等回答）：同一轮里后面的调用不执行，告诉模型稍后重调", () =>
  withEnv([callTools({ id: "c1", name: "ask_user", args: { question: "要几张？", kind: "choice", options: ["1", "2"] } }, { id: "c2", name: "canvas_apply_ops" }), fauxAssistantMessage("不该到这里")], {}, async (e) => {
    e.f.reply("POST /internal/platform/v1/approvals/propose", { json: { code: 0, data: { is_error: false, payload: { question: "要几张？" }, quote_credits: 0, summary: "向用户提问：要几张？", content: "已向用户提问，等待回答。", terminate: true } } });
    await e.ask();
    assert.equal(e.f.seen.filter((s) => s.path.includes("/tools/")).length, 0, "第二个调用没有到平台");
    const results = e.m.seen.length > 1 ? e.m.seen[1].filter((m) => m.role === "toolResult") : [];
    assert.equal(e.m.seen.length, 1, "两个结果都要求终止：不再请求模型");
    void results;
    const skipped = e.h.begun.filter((b) => b.name === "canvas_apply_ops");
    assert.equal(skipped.length, 0, "被跳过的调用连 tool.start 都不发，和现在一致");
  }));

test("plan_update：本地校验（规则和措辞与 Go 一致），通过后交给宿主发 plan.updated；不调平台", () =>
  withEnv([callTools({ id: "c1", name: "plan_update", args: { steps: [{ title: " 拆分镜 ", status: "doing" }, { title: "出图", status: "todo" }] } }), fauxAssistantMessage("好")], {}, async (e) => {
    await e.ask();
    assert.deepEqual(e.h.plans[0], [{ title: "拆分镜", status: "doing" }, { title: "出图", status: "todo" }]);
    assert.equal(e.f.seen.length, 0);
    assert.equal(e.h.ended[0].event.summary, "更新计划（2 步）");
    assert.equal(e.h.ended[0].event.label, "标签-plan_update");
    assert.equal(textOf(e.m.seen[1].find((m) => m.role === "toolResult")!), "计划已更新。");
  }));

test("plan_update 的错误：步数过多、标题为空或过长、状态不对", async () => {
  const bad: Array<[unknown, RegExp]> = [
    [{ steps: Array.from({ length: 13 }, () => ({ title: "x", status: "todo" })) }, /计划最多 12 步/],
    [{ steps: [{ title: "  ", status: "todo" }] }, /第 1 步的标题不能为空，且不超过 80 个字/],
    [{ steps: [{ title: "字".repeat(81), status: "todo" }] }, /第 1 步的标题不能为空，且不超过 80 个字/],
    [{ steps: [{ title: "a", status: "todo" }, { title: "b", status: "finished" }] }, /第 2 步的状态只能是 todo \/ doing \/ done/],
  ];
  for (const [args, want] of bad) {
    await withEnv([callTools({ id: "c1", name: "plan_update", args: args as Record<string, unknown> }), fauxAssistantMessage("好")], {}, async (e) => {
      await e.ask();
      const r = e.m.seen[1].find((m) => m.role === "toolResult")! as { isError?: boolean };
      assert.equal(r.isError, true);
      assert.match(textOf(r as never), want);
      assert.equal(e.h.plans.length, 0, "不合法的计划不能发事件");
      assert.equal(e.h.ended[0].event.is_error, true);
    });
  }
});

test("审批类工具：先向平台预估，通过后交给宿主创建审批（审批内容、预估积分、调用 id），本轮停下", () =>
  withEnv([callTools({ id: "call_g", name: "generate_media", args: { items: [{ nodeId: "n1" }], reason: "出图" } }), fauxAssistantMessage("不该到这里")], {}, async (e) => {
    e.f.reply("POST /internal/platform/v1/approvals/propose", { json: { code: 0, data: { is_error: false, payload: { items: [{ node_id: "n1", price: 5 }] }, quote_credits: 5, summary: "生成 1 个，预估 5 积分", content: "已提交给用户批准，等待决定。", terminate: true } } });
    await e.ask();
    assert.deepEqual(e.f.seen[0].body, { kind: "generate", args: { items: [{ nodeId: "n1" }], reason: "出图" } });
    assert.deepEqual(e.h.approvals[0], { kind: "generate", toolCallId: "call_g", payload: { items: [{ node_id: "n1", price: 5 }] }, quoteCredits: 5, summary: "生成 1 个，预估 5 积分" });
    // 顺序和现在一致：tool.start → 创建审批 → tool.end
    assert.equal(e.h.log.join(","), "begin:generate_media,approval:generate,end:generate_media");
    assert.equal(e.h.ended[0].event.summary, "生成 1 个，预估 5 积分");
    assert.equal(e.m.seen.length, 1, "terminate：等用户决定，不再请求模型");
  }));

test("审批类工具的三种：generate_media / canvas_delete / ask_user 对应 generate / delete / ask", async () => {
  for (const [tool, kind] of [["generate_media", "generate"], ["canvas_delete", "delete"], ["ask_user", "ask"]] as const) {
    await withEnv([callTools({ id: "c1", name: tool, args: { nodeIds: ["n1"] } }), fauxAssistantMessage("x")], {}, async (e) => {
      e.f.reply("POST /internal/platform/v1/approvals/propose", { json: { code: 0, data: { is_error: false, payload: {}, quote_credits: 0, summary: "s", content: "已提交", terminate: true } } });
      await e.ask();
      assert.equal((e.f.seen[0].body as { kind: string }).kind, kind);
      assert.equal(e.h.approvals[0].kind, kind);
    });
  }
});

test("预估失败（参数不对、节点不存在、模式不允许）：是给模型看的错误，不创建审批", () =>
  withEnv([callTools({ id: "c1", name: "canvas_delete", args: { nodeIds: ["n_不存在"] } }), fauxAssistantMessage("我再看看")], {}, async (e) => {
    e.f.reply("POST /internal/platform/v1/approvals/propose", { json: { code: 0, data: { is_error: true, content: "节点 n_不存在 不存在", quote_credits: 0, terminate: false } } });
    await e.ask();
    assert.equal(e.h.approvals.length, 0);
    const r = e.m.seen[1].find((m) => m.role === "toolResult")! as { isError?: boolean };
    assert.equal(r.isError, true);
    assert.match(textOf(r as never), /不存在/);
    assert.equal(e.h.ended[0].event.is_error, true);
    assert.match(String(e.h.ended[0].event.summary), /不存在/);
  }));

test("平台调用本身失败（HTTP 500、连不上）：模型看到错误结果，宿主收到 is_error 的 tool.end，运行不崩", () =>
  withEnv([callTools({ id: "c1", name: "canvas_get_state" }), fauxAssistantMessage("稍后再试")], {}, async (e) => {
    e.f.reply("POST /internal/platform/v1/tools/canvas_get_state", { status: 500, json: { code: 50000, msg: "内部错误" } });
    assert.equal((await e.ask()).status, "done");
    const r = e.m.seen[1].find((m) => m.role === "toolResult")! as { isError?: boolean };
    assert.equal(r.isError, true);
    assert.match(textOf(r as never), /内部错误/);
    assert.equal(e.h.ended[0].event.is_error, true);
    assert.equal(e.h.log.join(","), "begin:canvas_get_state,end:canvas_get_state");
  }));

test("模型把单个值当数组给（nodeIds 写成字符串）：校验前补成数组，和迁移前的行为一致", () =>
  withEnv([callTools({ id: "c1", name: "canvas_get_state", args: { nodeIds: "n1" } }), fauxAssistantMessage("好")], {}, async (e) => {
    e.f.reply("POST /internal/platform/v1/tools/canvas_get_state", toolReply("ok"));
    await e.ask();
    assert.deepEqual((e.f.seen[0].body as { args: unknown }).args, { nodeIds: ["n1"] });
  }));

test("同一轮里的多个调用按顺序一个个执行（画布写入不能并发）", () =>
  withEnv([callTools({ id: "c1", name: "canvas_apply_ops" }, { id: "c2", name: "canvas_apply_ops" }), fauxAssistantMessage("好")], {}, async (e) => {
    e.f.reply("POST /internal/platform/v1/tools/canvas_apply_ops", toolReply("一"));
    e.f.reply("POST /internal/platform/v1/tools/canvas_apply_ops", toolReply("二"));
    await e.ask();
    assert.equal(e.h.log.join(","), "begin:canvas_apply_ops,end:canvas_apply_ops,begin:canvas_apply_ops,end:canvas_apply_ops", "第一个结束了第二个才开始");
    const keys = e.f.seen.map((s) => s.headers["x-idempotency-key"]);
    assert.notEqual(keys[0], keys[1], "两次调用的幂等键不同（即使模型的 tool_call_id 重复也不会丢写入）");
  }));

test("崩溃后重放：同一个调用用同一个幂等键再来一次，Go 据此返回第一次的结果", async () => {
  const dir = mkdtempSync(join(tmpdir(), "tools-replay-"));
  const f = await fakePlatform();
  try {
    const dbPath = join(dir, "s.sqlite");
    const build = async (steps: Parameters<typeof fauxModels>[0]) => {
      const h = fakeHost();
      const m = fauxModels(steps);
      const registry = createRegistry();
      registry.install(createToolsExtension({ platform: createPlatform({ baseUrl: f.url, serviceToken: TOKEN }), decls: DECLS, host: h.host }));
      const harness = await Harness.open(await openNodeSqliteStorage(dbPath), { models: m.models, registry }, context);
      return { harness, root: await harness.root(context, { agent: { model: m.model } }), m };
    };
    // 第一次：平台调用发出去了，进程在拿到结果前停掉（Go 这边其实可能已经写了画布）
    f.reply("POST /internal/platform/v1/tools/canvas_apply_ops", { hang: true } as never);
    const a = await build([callTools({ id: "call_0", name: "canvas_apply_ops", args: { ops: [] } })]);
    await a.root.submit({ type: "input", content: "改画布" }, context);
    for (let i = 0; i < 100 && f.seen.length === 0; i++) await new Promise((r) => setTimeout(r, 20));
    assert.equal(f.seen.length, 1);
    await a.harness.close(context);
    // 重开后续跑：写工具声明可重放，所以会再调一次，带的是同一个幂等键
    f.reply("POST /internal/platform/v1/tools/canvas_apply_ops", toolReply("第一次的结果"));
    const b = await build([fauxAssistantMessage("好了")]);
    b.harness.resume();
    for (let i = 0; i < 150 && f.seen.length < 2; i++) await new Promise((r) => setTimeout(r, 20));
    assert.equal(f.seen.length, 2, "重放调了平台");
    assert.equal(f.seen[1].headers["x-idempotency-key"], f.seen[0].headers["x-idempotency-key"], "同一个幂等键");
    await b.harness.waitForIdle(context);
    await b.harness.close(context);
  } finally {
    await f.close();
    rmSync(dir, { recursive: true, force: true });
  }
});

// ---- codemode：用 Pi Durable 自带的（脚本崩溃后能接着跑），脚本里调的工具仍然走上面的工具扩展 ----

import { createCodemode } from "../src/codemode.ts";

const SCRIPTABLE = new Set(["canvas_get_state", "canvas_apply_ops", "web_search"]);

async function withCodemode(steps: Parameters<typeof fauxModels>[0], fn: (e: Env) => Promise<void>) {
  const f = await fakePlatform();
  const h = fakeHost();
  const m = fauxModels(steps);
  const registry = createRegistry();
  const decls = DECLS.map((d) => ({ ...d, scriptable: SCRIPTABLE.has(d.name) }));
  registry.install(createToolsExtension({ platform: createPlatform({ baseUrl: f.url, serviceToken: TOKEN }), decls, host: h.host }));
  registry.install(createCodemode(h.host));
  const harness = await Harness.open(new MemoryStorage(), { models: m.models, registry }, context);
  const root = await harness.root(context, { agent: { model: m.model } });
  const ask = async (text = "开始") => (await (await root.submit({ type: "input", content: text }, context)).wait(context));
  try {
    await fn({ f, h, m, harness, root, ask });
  } finally {
    await harness.close(context);
    await f.close();
  }
}

test("codemode：脚本里调工具各自走平台（幂等键各不相同），事件带上外层调用的 id，整个脚本只算 1 步", () =>
  withCodemode([callTools({ id: "call_s", name: "codemode", args: { code: "const a = await tools.canvas_get_state({}); const b = await tools.canvas_get_state({}); return 'ok';" } }), fauxAssistantMessage("好")], async (e) => {
    e.f.reply("POST /internal/platform/v1/tools/canvas_get_state", toolReply("一"));
    e.f.reply("POST /internal/platform/v1/tools/canvas_get_state", toolReply("二"));
    assert.equal((await e.ask()).status, "done");
    assert.equal(e.f.seen.length, 2);
    assert.notEqual(e.f.seen[0].headers["x-idempotency-key"], e.f.seen[1].headers["x-idempotency-key"]);
    const nested = e.h.begun.filter((b) => b.name === "canvas_get_state");
    assert.equal(nested.length, 2);
    for (const n of nested) assert.equal(n.parentId, "call_s", "嵌套调用带外层调用的 id");
    assert.equal(e.h.begun.filter((b) => b.name === "codemode").length, 1);
    // 宿主按「没有 parentId 才计步数」，所以这里只有外层那一次是一步
    assert.equal(e.h.ended.filter((x) => !x.call.parentId).length, 1);
    for (const x of e.h.ended.filter((x) => x.call.parentId)) assert.equal(x.event.parent_id, "call_s", "tool.end 也带 parent_id");
  }));

test("codemode：只有标为可被脚本调用的工具在 tools 里；需要用户决定的、看图的不行", () =>
  withCodemode([callTools({ id: "c1", name: "codemode", args: { code: "return Object.keys(tools).sort().join(',');" } }), fauxAssistantMessage("好")], async (e) => {
    await e.ask();
    const r = textOf(e.m.seen[1].find((m) => m.role === "toolResult")!);
    for (const ok of ["canvas_get_state", "canvas_apply_ops", "web_search"]) assert.match(r, new RegExp(ok));
    for (const no of ["generate_media", "canvas_delete", "ask_user", "canvas_inspect_image"]) assert.doesNotMatch(r, new RegExp(no));
  }));

test("codemode：脚本里的工具失败，脚本能 catch；脚本出错时模型收到错误结果", () =>
  withCodemode([callTools({ id: "c1", name: "codemode", args: { code: "await tools.canvas_apply_ops({}); return 1;" } }), fauxAssistantMessage("好")], async (e) => {
    e.f.reply("POST /internal/platform/v1/tools/canvas_apply_ops", { json: { code: 0, data: { content: "操作没有生效：缺少 ops", is_error: true, terminate: false, tool_end: { id: "x", name: "canvas_apply_ops", is_error: true, summary: "没有生效" } } } });
    await e.ask();
    const r = e.m.seen[1].find((m) => m.role === "toolResult")! as { isError?: boolean };
    assert.equal(r.isError, true);
    assert.match(textOf(r as never), /缺少 ops/);
  }));
