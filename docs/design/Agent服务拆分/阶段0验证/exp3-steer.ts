import { createRegistry, Harness, LiveDoc, MemoryStorage } from "@earendil-works/pi-durable";
import { brief, context, fauxAssistantMessage, makeModels, sleep } from "./common.ts";

/** 一个慢回答：等 ms 毫秒才回；被中止就返回 aborted */
const slow = (text: string, ms: number) => async (_t: any, options: any) => {
  const aborted = await new Promise<boolean>((resolve) => {
    const timer = setTimeout(() => resolve(false), ms);
    options?.signal?.addEventListener("abort", () => { clearTimeout(timer); resolve(true); }, { once: true });
  });
  return aborted ? fauxAssistantMessage("", { stopReason: "aborted" }) : fauxAssistantMessage(text);
};

async function scenario(title: string, run: (root: any, harness: Harness) => Promise<void>, steps: any[]) {
  console.log(`\n== ${title}`);
  const { models, seen, model } = makeModels(steps);
  const harness = await Harness.open(new MemoryStorage(), { models, registry: createRegistry() }, context);
  const root = await harness.root(context, { agent: { model } });
  await run(root, harness);
  console.log("模型被请求", seen.length, "次");
  seen.forEach((msgs, i) => console.log(`  第 ${i + 1} 次看到:`, msgs.filter((m) => m.role !== "system").map(brief).join("  /  ")));
  const items = [...(await root.entries({}, 30, undefined, context)).items].reverse();
  console.log("  记录:", items.map((e) => e.kind + (e.model?.[0]?.role === "assistant" && (e.model[0] as any).stopReason !== "stop" ? `[${(e.model[0] as any).stopReason}]` : "")).join(" → "));
  await harness.close(context);
}

// A. 默认：模型正在写回答时插话
await scenario("A. 默认 steer：模型正在输出时插话", async (root) => {
  const first = await root.submit({ type: "input", content: "拆分镜" }, context);
  await sleep(200);
  const steer = await root.submit({ type: "input", content: "改成竖屏", whenBusy: "steer" }, context);
  const [a, b] = [await first.wait(context), await steer.wait(context)];
  console.log("原请求:", a.status, " 插话:", b.status);
}, [slow("回答一（横屏）", 800), fauxAssistantMessage("回答二（结合了竖屏）")]);

// B. 中止后重提：还原「打断重答」
await scenario("B. 中止当前回答，再把补充作为新输入提交", async (root, harness) => {
  const first = await root.submit({ type: "input", content: "拆分镜" }, context);
  await sleep(200);
  const live = await harness.snapshot(LiveDoc, root.id, context);
  console.log("进行中的状态（LiveDoc）:", JSON.stringify({ generation: live?.generation ? Object.keys(live.generation) : null, tools: live?.tools ? Object.keys(live.tools).length : 0 }));
  const t0 = Date.now();
  await root.abort(context);
  console.log("abort 返回耗时", Date.now() - t0, "ms；原请求状态:", (await first.wait(context)).status);
  const second = await root.submit({ type: "input", content: "改成竖屏" }, context);
  console.log("补充:", (await second.wait(context)).status);
}, [slow("回答一（不该出现）", 5000), fauxAssistantMessage("合并后的回答")]);
