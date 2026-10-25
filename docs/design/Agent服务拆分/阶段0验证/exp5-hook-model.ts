import { CompactionTask, createRegistry, defineExtension, Harness, hook, MemoryStorage } from "@earendil-works/pi-durable";
import { brief, context, fauxAssistantMessage, makeModels } from "./common.ts";

// 钩子里自己调模型写摘要（用画布专用的提示词），返回 {summary}
let hookResult = "";
const Canvas = defineExtension({
  name: "canvas-summary",
  hooks: [hook(CompactionTask, {
    beforeCompact: async (c: any, api: any) => {
      const model = api.models.getModel("faux", "tiny");
      const msg = await api.models.completeSimple(
        model,
        { systemPrompt: "你是画布助手的上下文摘要器，按「目标/已完成/不能丢的数据」写。", messages: [{ role: "user", content: [{ type: "text", text: c.messages.map(brief).join("\n") }], timestamp: Date.now() }] },
        { signal: api.signal, maxTokens: 500 },
      );
      console.log("钩子里的调用结果:", JSON.stringify({ stopReason: msg.stopReason, errorMessage: msg.errorMessage, content: msg.content }));
      hookResult = msg.content.map((p: any) => (p.type === "text" ? p.text : "")).join("");
      return { summary: hookResult };
    },
  })],
});
const registry = createRegistry();
registry.install(Canvas);
const steps: any[] = [
  fauxAssistantMessage("回答一 " + "细节 ".repeat(700)), fauxAssistantMessage("回答二 " + "细节 ".repeat(700)), fauxAssistantMessage("回答三 " + "细节 ".repeat(700)),
  (t: any) => fauxAssistantMessage(t.messages[0].role === "system" && String(t.messages[0].content).includes("画布助手的上下文摘要器") ? "## 目标\n钩子里调模型写出的画布摘要" : "不是摘要请求"),
  fauxAssistantMessage("回答四"),
];
const { models, seen, model } = makeModels(steps, { id: "tiny", contextWindow: 3000, maxTokens: 1000 });
const settings = { compaction: { reserveTokens: 1000, keepRecentTokens: 400, backgroundTokens: 0 } }; // 保留量调小，两轮对话也有东西可压
const harness = await Harness.open(new MemoryStorage(), { models, registry, settings }, context);
const root = await harness.root(context, { agent: { model } });
for (const q of ["一", "二", "三"]) await (await root.submit({ type: "input", content: q }, context)).wait(context);
const id = await root.compact("保留节点 id", context);
const { state } = await harness.waitForTask(id, context);
console.log("压缩任务结果:", state.outcome.status);
console.log("钩子里模型返回的摘要:", JSON.stringify(hookResult));
await (await root.submit({ type: "input", content: "四" }, context)).wait(context);
console.log("压缩后模型看到的上下文:");
for (const m of seen.at(-1)!.filter((m) => m.role !== "system")) console.log("  ", brief(m));
console.log("\n摘要消息完整内容:\n" + JSON.stringify((seen.at(-1)!.find((m) => m.role === "user") as any).content));
await harness.close(context);
