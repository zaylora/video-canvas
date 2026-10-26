import http from "node:http";
import type { AddressInfo } from "node:net";
import { Type } from "@earendil-works/pi-ai";
import { createRegistry, defineExtension, defineTool, GenerationTask, Harness, hook, MemoryStorage } from "@earendil-works/pi-durable";
import { brief, context, fauxAssistantMessage, fauxToolCall, makeModels } from "./common.ts";

// 模型请求前的钩子（GenerationTask 的 beforeRequest）：能不能在里面向 Go 要数据、注入给模型看；出错时会怎样；能不能拦住这次调用。
// 「Go」用一个本机 HTTP 服务代替。
let goHits = 0;
const go = http.createServer((_req, res) => {
  goHits++;
  res.setHeader("content-type", "application/json");
  res.end(JSON.stringify({ revision: 6 + goHits, summary: `画布有 3 个节点（第 ${goHits} 次查询），用户刚手动改了 n2` }));
});
await new Promise<void>((r) => go.listen(0, "127.0.0.1", r));
const goUrl = `http://127.0.0.1:${(go.address() as AddressInfo).port}`;

const ReadCanvas = defineTool({
  name: "canvas_get_state", description: "读画布", parameters: Type.Object({}), replay: "safe",
  execute: async () => ({ output: [{ type: "text", text: "工具结果：3 个节点" }] }),
});

async function run(title: string, beforeRequest: (req: { messages: readonly any[] }) => Promise<any> | any) {
  console.log(`\n== ${title}`);
  const reports: string[] = [];
  const registry = createRegistry();
  registry.install(defineExtension({ name: "canvas", tools: [ReadCanvas], hooks: [hook(GenerationTask, { beforeRequest: async (req: any) => beforeRequest(req) })] }));
  const { models, seen, model } = makeModels([
    fauxAssistantMessage([fauxToolCall("canvas_get_state", {}, { id: "c1" })], { stopReason: "toolUse" }),
    fauxAssistantMessage("好的"),
  ]);
  const harness = await Harness.open(new MemoryStorage(), { models, registry, onReport: (e: any, info: any) => reports.push(`${info?.type}:${String(e?.message ?? e).slice(0, 40)}`) } as any, context);
  const root = await harness.root(context, { agent: { model } });
  const rec = await (await root.submit({ type: "input", content: "看看画布" }, context)).wait(context);
  console.log("提交状态:", rec.status, rec.status === "unanswered" ? rec.reason : "", " 模型被请求", seen.length, "次");
  seen.forEach((msgs, i) => console.log(`  第 ${i + 1} 次请求的最后一条:`, brief(msgs.at(-1)!)));
  const kinds = [...(await root.entries({}, 30, undefined, context)).items].reverse();
  console.log("  记录里有没有注入的内容:", kinds.some((e) => JSON.stringify(e).includes("画布最新状态")) ? "有（不该有）" : "没有（只对那一次请求生效）");
  if (reports.length) console.log("  onReport:", reports.join(" | "));
  await harness.close(context);
}

await run("A. 钩子里向 Go 要数据，注入给模型看", async (req) => {
  const j: any = await (await fetch(goUrl + "/canvas-brief")).json();
  return { messages: [...req.messages, { role: "user", content: [{ type: "text", text: `【画布最新状态】${j.summary}` }], timestamp: Date.now() }] };
});
console.log("Go 被查询了", goHits, "次");

await run("B. 钩子里向 Go 要数据失败（连不上）", async () => {
  await fetch("http://127.0.0.1:1/nope");
});

await run("C. 钩子想拦住这次调用：抛异常", async () => {
  throw new Error("本轮额度用完了");
});
go.close();
