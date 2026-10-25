import { CompactionEntry, CompactionTask, createRegistry, defineExtension, Harness, hook, LiveDoc, MemoryStorage } from "@earendil-works/pi-durable";
import { brief, context, fauxAssistantMessage, makeModels } from "./common.ts";

// 用 beforeCompact 接管摘要：看钩子拿到什么、返回 {summary} 后上下文是什么样、第二次压缩能不能拿到上一份摘要做增量更新
const calls: any[] = [];
const Canvas = defineExtension({
  name: "canvas-summary",
  hooks: [hook(CompactionTask, {
    beforeCompact: async (c: any, api: any) => {
      calls.push({ reason: c.reason, entries: c.entries.map((e: any) => e.kind), first: brief(c.messages[0]), n: c.messages.length, instructions: c.instructions, apiKeys: Object.keys(api).slice(0, 12) });
      return { summary: `## 目标\n画布专用摘要 #${calls.length}（共摘要 ${c.messages.length} 条消息）\n## 已完成\n节点 n1、n2\n## 不能丢的数据\n模型 key: claude` };
    },
  })],
});
const registry = createRegistry();
registry.install(Canvas);

const answer = (t: any) => fauxAssistantMessage(`对「${(t.messages.findLast((m: any) => m.role === "user") as any)?.content?.[0]?.text ?? ""}」的详细回答：${"细节 ".repeat(700)}`);
const { models, seen, model } = makeModels(Array.from({ length: 30 }, () => answer), { id: "tiny", contextWindow: 3000, maxTokens: 1000 });
const settings = { compaction: { reserveTokens: 1000, keepRecentTokens: 400, backgroundTokens: 0 } }; // 关掉后台压缩，只在超线时阻塞压缩
const harness = await Harness.open(new MemoryStorage(), { models, registry, settings }, context);
const root = await harness.root(context, { agent: { model } });

for (const q of ["住哪里", "吃什么", "去哪玩", "看博物馆", "夜生活", "怎么出行", "带什么"]) {
  await (await root.submit({ type: "input", content: q }, context)).wait(context);
  const running = (await harness.snapshot(LiveDoc, root.id, context))?.compactions ?? [];
  for (const { taskId } of running) await harness.waitForTask(taskId, context);
}
console.log("== 自动压缩时钩子被调用", calls.length, "次");
calls.forEach((c, i) => console.log(`  第 ${i + 1} 次: reason=${c.reason}  摘要 ${c.n} 条消息  entries=[${c.entries.join(",")}]\n    第一条: ${c.first}\n    api 字段: ${c.apiKeys.join(",")}`));

const view = await root.context(context);
console.log("\n== 现在发给模型的上下文（头部）:");
console.log("  head 是压缩记录:", CompactionEntry.is(view.head), " reason:", CompactionEntry.is(view.head) ? view.head.data.reason : "-");
for (const m of view.messages.slice(0, 3)) console.log("   ", brief(m));

console.log("\n== 手动压缩带说明");
const id = await root.compact("保留节点 id、任务 id 和待处理的审批", context);
await harness.waitForTask(id, context);
const last = calls.at(-1);
console.log("  第", calls.length, "次: reason=" + last.reason, " instructions=", JSON.stringify(last.instructions));
console.log("  第一条（是不是上一份摘要）:", last.first);
await harness.close(context);
