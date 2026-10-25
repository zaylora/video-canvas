import { Type } from "@earendil-works/pi-ai";
import { createRegistry, defineExtension, defineTool, Harness, MemoryStorage, watchEvents } from "@earendil-works/pi-durable";
import { brief, context, fauxAssistantMessage, fauxText, fauxToolCall, makeModels, sleep } from "./common.ts";

// 插话进了队列以后，什么时候算「被读到」，运行结束时没读到的会怎样？（前端「插话排队」要的事实）
// 观察 Pi 的 submission 事件（queued → placed → done / unanswered）和插话相对于工具、模型请求的位置。
const SlowTool = defineTool({
  name: "slow_tool", description: "慢工具", parameters: Type.Object({}), replay: "safe",
  execute: async () => { await sleep(400); return { output: [{ type: "text", text: "慢工具做完了" }] }; },
});
const AskTool = defineTool({
  name: "ask_tool", description: "提问：返回后本轮停下（同我们的 ask_user / 审批）", parameters: Type.Object({}), replay: "safe",
  execute: async () => { await sleep(300); return { output: [{ type: "text", text: "已提问，等待回答" }], control: { terminate: true } } as any; },
});
const registry = createRegistry();
registry.install(defineExtension({ name: "t", tools: [SlowTool, AskTool] }));

const slow = (text: string, ms: number) => async (_t: any, options: any) => {
  const aborted = await new Promise<boolean>((r) => { const t = setTimeout(() => r(false), ms); options?.signal?.addEventListener("abort", () => { clearTimeout(t); r(true); }, { once: true }); });
  return aborted ? fauxAssistantMessage("", { stopReason: "aborted" }) : fauxAssistantMessage(text);
};

async function scenario(title: string, steps: any[], drive: (root: any, harness: Harness, t0: number) => Promise<void>) {
  console.log(`\n== ${title}`);
  const { models, seen, model } = makeModels(steps);
  // 设置和 agent/src/engine/engine.ts 一致：同一轮的工具一个个执行，插话一起带上
  const harness = await Harness.open(new MemoryStorage(), { models, registry, settings: () => ({ retry: { enabled: false }, toolExecution: "sequential", steeringMode: "all" }) }, context);
  const root = await harness.root(context, { agent: { model } });
  const stream = await watchEvents(harness, root.id, context);
  const t0 = Date.now();
  const log: string[] = [];
  const ids = new Map<string, string>();
  stream.start(async (events) => {
    for (const e of events as any[]) {
      const at = String(Date.now() - t0).padStart(5);
      if (e.type === "submission") log.push(`${at}ms  submission ${ids.get(e.record.id) ?? e.record.id}  ${e.record.status}${e.record.reason ? " (" + e.record.reason + ")" : ""}`);
      else if (e.type === "tool_execution_start") log.push(`${at}ms  tool 开始 ${e.toolName}`);
      else if (e.type === "tool_execution_end") log.push(`${at}ms  tool 结束 ${e.toolName}`);
      else if (e.type === "message_start" && e.message.role === "assistant") log.push(`${at}ms  模型回答开始（第 ${seen.length} 次请求之后）`);
      else if (e.type === "run_end") log.push(`${at}ms  run_end`);
      else if (e.type === "run_start") log.push(`${at}ms  run_start`);
    }
  });
  (globalThis as any).__name = (id: any, label: string) => ids.set(String(id), label);
  await drive(root, harness, t0);
  await sleep(300);
  log.forEach((l) => console.log("  " + l));
  console.log(`  模型被请求 ${seen.length} 次`);
  seen.forEach((msgs, i) => console.log(`    第 ${i + 1} 次看到: ${msgs.filter((m) => m.role !== "system").map(brief).join("  /  ")}`));
  await harness.close(context);
}
const name = (id: any, l: string) => (globalThis as any).__name(id, l);

await scenario("A. 工具执行中插话（正常情况）", [
  fauxAssistantMessage([fauxText("先跑工具"), fauxToolCall("slow_tool", {}, { id: "c1" })], { stopReason: "toolUse" }),
  fauxAssistantMessage("工具之后的回答"),
], async (root) => {
  const first = await root.submit({ type: "input", content: "开始" }, context); name(first.id, "原请求");
  await sleep(150);
  const s = await root.submit({ type: "input", content: "改成竖屏", whenBusy: "steer" }, context); name(s.id, "插话");
  await first.wait(context); await s.wait(context);
});

await scenario("B. 模型正在写最终回答时插话（后面没有工具了）", [
  slow("最终回答（慢）", 600),
  fauxAssistantMessage("读了插话之后的第二次回答"),
], async (root) => {
  const first = await root.submit({ type: "input", content: "开始" }, context); name(first.id, "原请求");
  await sleep(200);
  const s = await root.submit({ type: "input", content: "改成竖屏", whenBusy: "steer" }, context); name(s.id, "插话");
  await first.wait(context); await s.wait(context);
});

await scenario("C. 提问工具执行中插话（工具返回后本轮停下，等回答）", [
  fauxAssistantMessage([fauxText("我问你一件事"), fauxToolCall("ask_tool", {}, { id: "c1" })], { stopReason: "toolUse" }),
  fauxAssistantMessage("（不该走到：本轮应该已经停下）"),
], async (root) => {
  const first = await root.submit({ type: "input", content: "开始" }, context); name(first.id, "原请求");
  await sleep(150);
  const s = await root.submit({ type: "input", content: "算了用双剑的", whenBusy: "steer" }, context); name(s.id, "插话");
  await first.wait(context);
  await sleep(300);
  console.log("  原请求结束后，插话状态:", (await Promise.race([s.wait(context), sleep(500).then(() => ({ status: "（500ms 内还没结束）" }))]) as any).status);
});

await scenario("D. 插话排队时用户点「停止」(abort)", [
  fauxAssistantMessage([fauxText("先跑工具"), fauxToolCall("slow_tool", {}, { id: "c1" })], { stopReason: "toolUse" }),
  fauxAssistantMessage("不该出现"),
], async (root) => {
  const first = await root.submit({ type: "input", content: "开始" }, context); name(first.id, "原请求");
  await sleep(150);
  const s = await root.submit({ type: "input", content: "改成竖屏", whenBusy: "steer" }, context); name(s.id, "插话");
  await sleep(50);
  await root.abort(context);
  console.log("  abort 后原请求:", (await first.wait(context)).status, " 插话:", (await s.wait(context)).status, (await s.wait(context) as any).reason ?? "");
});

await scenario("E. 连发两条插话（steeringMode = all）", [
  fauxAssistantMessage([fauxText("先跑工具"), fauxToolCall("slow_tool", {}, { id: "c1" })], { stopReason: "toolUse" }),
  fauxAssistantMessage("一起读了之后的回答"),
  fauxAssistantMessage("（多余的回答，用来看有没有第二个 run）"),
], async (root) => {
  const first = await root.submit({ type: "input", content: "开始" }, context); name(first.id, "原请求");
  await sleep(100);
  const a = await root.submit({ type: "input", content: "插话一", whenBusy: "steer" }, context); name(a.id, "插话一");
  await sleep(50);
  const b = await root.submit({ type: "input", content: "插话二", whenBusy: "steer" }, context); name(b.id, "插话二");
  await first.wait(context); await a.wait(context); await b.wait(context);
});
