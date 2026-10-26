import { Type } from "@earendil-works/pi-ai";
import { createRegistry, defineExtension, defineTool, Harness, MemoryStorage } from "@earendil-works/pi-durable";
import { brief, context, fauxAssistantMessage, fauxToolCall, makeModels } from "./common.ts";

// 审批建模：工具返回 terminate 结束这一轮；用户决定到来时，用带 edits 的写入把占位工具结果换成真实结果，再提交一条输入让模型继续。
let deleted = 0;
const CanvasDelete = defineTool({
  name: "canvas_delete",
  description: "删除节点，需要用户批准",
  parameters: Type.Object({ nodeIds: Type.Array(Type.String()) }),
  replay: "never",
  execute: async () => ({ output: [{ type: "text", text: "已提交确认，等待用户决定" }], control: { terminate: true } }),
});
const Canvas = defineExtension({ name: "canvas", tools: [CanvasDelete] });
const registry = createRegistry();
registry.install(Canvas);

const { models, seen, model } = makeModels([
  fauxAssistantMessage([fauxToolCall("canvas_delete", { nodeIds: ["n_old"] }, { id: "call_del" })], { stopReason: "toolUse" }),
  fauxAssistantMessage("已清理。"),
]);
const harness = await Harness.open(new MemoryStorage(), { models, registry }, context);
const root = await harness.root(context, { agent: { model } });

console.log("== 1. 用户发起，模型调用需要审批的工具");
const first = await (await root.submit({ type: "input", content: "把废稿清掉" }, context)).wait(context);
console.log("第一次提交的状态:", first.status, first.status === "unanswered" ? first.reason : "");
let page = await root.entries({}, 20, undefined, context);
const entries = [...page.items].reverse();
console.log("记录:", entries.map((e) => e.kind).join(" → "));
const toolResult = entries.find((e) => e.kind === "pi.tool-result")!;
console.log("占位工具结果:", brief(toolResult.model![0]));
console.log("等审批期间模型被请求了", seen.length, "次（应为 1）");

console.log("\n== 2. 用户批准：写入 edits 替换占位结果，再提交输入让模型继续");
const real = { ...toolResult.model![0], isError: false, content: [{ type: "text", text: "用户批准，已删除 1 个节点：n_old" }] } as any;
const write = await root.submit({ type: "write", entry: { kind: "app.approval", data: { approvalId: 7, decision: "approve" }, edits: [{ target: toolResult.id, action: "replace", messages: [real] }] } }, context);
console.log("写入状态:", (await write.wait(context)).status);
const second = await (await root.submit({ type: "input", content: "（用户已处理上面的确认，请继续）" }, context)).wait(context);
console.log("第二次提交的状态:", second.status);
console.log("\n模型第 2 次请求看到的上下文:");
for (const m of seen[1]) console.log("  ", brief(m));
await harness.close(context);
