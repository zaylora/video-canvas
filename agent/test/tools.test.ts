import assert from "node:assert/strict";
import { test } from "node:test";
import type { AgentTool } from "@earendil-works/pi-agent-core";
import { validateToolArguments, type Tool } from "@earendil-works/pi-ai";
import { buildTools, wrapScalarArrays } from "../src/tools.ts";
import { TOOL_DEFS as LEGACY } from "./fixtures/legacy-tool-defs.ts";
import type { ToolReply } from "../src/types.ts";
import { TOOL_DECLS } from "./helpers.ts";

const fakeBridge = (r: ToolReply = { content: "ok" }) => ({ tool: async () => r });

test("buildTools：按 Go 下发的声明构造，名字、顺序、说明、参数原样；label 用下发的界面名称", () => {
  const decls = [
    { ...TOOL_DECLS[2], label: "整理布局" },
    { ...TOOL_DECLS[0], label: "读取画布" },
  ];
  const tools = buildTools(fakeBridge(), decls);
  assert.deepEqual(tools.map((t) => t.name), ["canvas_arrange", "canvas_get_state"]);
  assert.equal(tools[0].label, "整理布局");
  assert.equal(tools[0].description, decls[0].description);
  assert.deepEqual(tools[0].parameters, decls[0].parameters);
});

test("buildTools：没有声明就没有工具；不认识的执行方直接报错，不静默忽略", () => {
  assert.deepEqual(buildTools(fakeBridge(), undefined), []);
  assert.throws(() => buildTools(fakeBridge(), [{ ...TOOL_DECLS[0], executor: "client" }]), /执行方/);
});

test("buildTools：execute 回调桥，terminate 通知运行时", async () => {
  let terminated = 0;
  const [t] = buildTools(fakeBridge({ content: "等用户回答", terminate: true }), [TOOL_DECLS[0]], () => terminated++);
  const r = await t.execute("c1", {}, undefined);
  assert.equal(terminated, 1);
  assert.equal(r.terminate, true);
  assert.equal((r.content[0] as { text: string }).text, "等用户回答");
});

// 迁移前用 typebox 定义，现在 Go 下发普通 JSON Schema：同一批参数过两边，接受与拒绝的结果必须一致
const CASES = {
  canvas_get_state: [{}, { nodeIds: ["a"] }, { nodeIds: "a" }, { nodeIds: [1] }, { groupId: null }, { groupId: 5 }, { groupId: "g", extra: 1 }],
  canvas_apply_ops: [{}, { ops: [] }, { ops: [{ op: "create_node", kind: "image" }] }, { ops: [{ op: "bad" }] }, { ops: [{ op: "move", extra: 1 }] },
    { ops: [{ op: "move", id: "a", position: { x: "1", y: 2 } }] }, { ops: [{ op: "create_node", label: null }] }, { ops: "x" }, { ops: [{ op: "create_node", params: { a: 1 } }] }],
  canvas_arrange: [{ layout: "row" }, { layout: "diag" }, {}, { layout: "grid", nodeIds: ["a", "b"] }, { layout: "grid", nodeIds: [1] }],
  canvas_delete: [{ reason: "废稿" }, {}, { reason: 1 }, { reason: "x", nodeIds: ["a"], edgeIds: null }],
  plan_update: [{ steps: [{ title: "a", status: "todo" }] }, { steps: [{ title: "a", status: "x" }] }, { steps: [] }, {}, { steps: [{ status: "done" }] }],
  ask_user: [{ question: "q", kind: "choice" }, { question: "q", kind: "x" }, { kind: "model" }, { question: "q", kind: "model", modelKind: "video" }, { question: "q", kind: "model", modelKind: "audio" }, { question: "q", kind: "choice", allowCustom: "true" }],
  model_list: [{ kind: "image" }, { kind: "text" }, {}, { kind: "audio" }],
  generate_media: [{ items: [{ nodeId: "a" }] }, { items: [{}] }, {}, { items: [{ nodeId: 1 }], reason: "r" }],
  task_get: [{ taskId: "1" }, { taskId: 1 }, {}, { taskId: null }],
  skill_search: [{}, { query: "x" }, { query: 1 }, { query: null }],
  skill_read: [{ name: "a" }, {}, { name: "a", file: "x.md" }, { name: "a", file: 3 }],
  canvas_inspect_image: [{ nodeIds: ["a"] }, { nodeIds: [] }, {}, { nodeIds: "a" }],
};

// 新的一边按 pi 的真实流程走：先 prepareArguments，再校验
const outcome = (tool: Pick<AgentTool, "name"> & Partial<Pick<AgentTool, "prepareArguments">> & Tool, args: unknown) => {
  try {
    const prepared = tool.prepareArguments ? tool.prepareArguments(args) : args;
    return { ok: true, args: validateToolArguments(tool, { type: "toolCall", id: "t", name: tool.name, arguments: prepared as never }) };
  } catch {
    return { ok: false };
  }
};

test("参数校验：新的 JSON Schema 和迁移前的 typebox 定义，对同一批参数接受与拒绝一致", () => {
  assert.deepEqual(Object.keys(CASES), TOOL_DECLS.map((d) => d.name), "每个工具都要有校验用例");
  const bridge = fakeBridge();
  const diffs: string[] = [];
  for (const decl of TOOL_DECLS) {
    const legacy = LEGACY.find((d) => d.name === decl.name)!;
    const built = buildTools(bridge, [decl])[0];
    for (const args of CASES[decl.name as keyof typeof CASES]) {
      const a = outcome(legacy, args), b = outcome(built, args);
      if (a.ok !== b.ok) diffs.push(`${decl.name} ${JSON.stringify(args)}: typebox=${a.ok} json-schema=${b.ok}`);
    }
  }
  assert.deepEqual(diffs, []);
});

test("单个值传给数组参数：自动包成数组（和迁移前一样），对象不动", () => {
  const schema = { type: "object", properties: { ids: { type: "array", items: { type: "string" } }, deep: { type: "object", properties: { xs: { type: "array", items: { type: "number" } } } } } };
  assert.deepEqual(wrapScalarArrays(schema, { ids: "a" }), { ids: ["a"] });
  assert.deepEqual(wrapScalarArrays(schema, { ids: ["a", "b"] }), { ids: ["a", "b"] });
  assert.deepEqual(wrapScalarArrays(schema, { deep: { xs: 3 } }), { deep: { xs: [3] } });
  assert.deepEqual(wrapScalarArrays(schema, { ids: { a: 1 } }), { ids: { a: 1 } }, "对象放在要求数组的地方不改，让校验报错");
  assert.deepEqual(wrapScalarArrays(schema, {}), {});
  const input = { ids: "a" };
  wrapScalarArrays(schema, input);
  assert.deepEqual(input, { ids: "a" }, "不修改入参");
});

test("基准里的声明和冻结的 typebox 定义是同一份（序列化后相同）", () => {
  const ser = LEGACY.map((d) => ({ name: d.name, description: d.description, parameters: JSON.parse(JSON.stringify(d.parameters)) }));
  assert.deepEqual(TOOL_DECLS.map(({ name, description, parameters }) => ({ name, description, parameters })), ser);
});
