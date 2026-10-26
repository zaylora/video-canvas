import { Type } from "@earendil-works/pi-ai";
import { createRegistry, defineExtension, defineTool, Harness, MemoryStorage, watchEvents } from "@earendil-works/pi-durable";
import { context, fauxAssistantMessage, fauxText, fauxToolCall, makeModels, sleep } from "./common.ts";

// 看 Pi 的 watchEvents 在一次「用户提问 → 模型说话并调工具 → 工具返回 → 模型回答」里给什么，和平台现有事件逐项对照。
const ReadCanvas = defineTool({
  name: "canvas_get_state",
  description: "读画布",
  parameters: Type.Object({ nodeIds: Type.Array(Type.String()) }),
  replay: "safe",
  execute: async (_args, api) => {
    api.output("读取中…");
    await sleep(120);
    return { output: [{ type: "text", text: "画布有 3 个节点" }], details: { summary: "读取 3 个节点", node_ids: ["n1", "n2", "n3"] } as any };
  },
});
const registry = createRegistry();
registry.install(defineExtension({ name: "canvas", tools: [ReadCanvas] }));
const { models, model } = makeModels([
  fauxAssistantMessage([fauxText("我先看看画布。"), fauxToolCall("canvas_get_state", { nodeIds: ["n1"] }, { id: "call_1" })], { stopReason: "toolUse" }),
  fauxAssistantMessage("读完了，有 3 个节点。"),
]);
const harness = await Harness.open(new MemoryStorage(), { models, registry }, context);
const root = await harness.root(context, { agent: { model } });
const stream = await watchEvents(harness, root.id, context);

const seen: string[] = [];
stream.start(async (events) => {
  for (const e of events as any[]) {
    let note = "";
    if (e.type === "message_update") note = e.changes.map((c: any) => c.type + (c.delta ? `:${JSON.stringify(c.delta)}` : "")).join(",");
    else if (e.type === "message_start") note = e.message.role;
    else if (e.type === "message_end") note = `${e.entry.kind} model=${e.entry.model?.[0]?.role}${e.entry.data !== undefined ? " data=" + JSON.stringify(e.entry.data).slice(0, 40) : ""}`;
    else if (e.type.startsWith("tool_execution")) note = `${e.toolName}#${e.toolCallId}${e.args ? " args=" + JSON.stringify(e.args) : ""}${e.output ? " output=" + JSON.stringify(e.output) : ""}${e.details ? " details=" + JSON.stringify(e.details) : ""}${e.entry ? " entry=" + e.entry.kind : ""}`;
    else if (e.type === "entry_appended") note = e.entry.kind;
    else if (e.type === "submission") note = `${e.record.type}:${e.record.status}${e.record.reason ? "/" + e.record.reason : ""}`;
    else if (e.type === "usage_changed") note = "";
    seen.push(note ? `${e.type} ${note}` : e.type);
  }
});

const sub = await root.submit({
  type: "input", content: "看看画布",
  entry: { kind: "app.user", data: { text: "看看画布", selection: ["n1"], steer: false } },
}, context);
await sub.wait(context);
await sleep(100);
console.log("snapshot 里没有的东西（只有实时事件才有）和 Pi 给的事件序列：\n");
seen.forEach((s, i) => console.log(String(i + 1).padStart(2), s));
await harness.close(context);
