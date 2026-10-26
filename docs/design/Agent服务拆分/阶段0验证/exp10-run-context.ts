// 每次模型请求怎么带上「这是哪个运行」（设计文档 20.7）：
// Go 的模型代理（上下文模式）要求每个请求带 X-Agent-Context（运行 id、用户、预算……），但 Pi 的 Harness 一个进程托管很多会话，
// 流选项（settings.stream.headers）是整个 Harness 共用的，不能按会话区分。
// 验证：beforeRequest 钩子里读出 Pi 给这个会话生成的 provider sessionId，登记「sessionId → 运行上下文」；
//       包一层 ProviderStreams，发请求时按 options.sessionId 查表加请求头。同时验证模型能在 Harness 打开之后动态注册。
import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import { createModels, createProvider } from "@earendil-works/pi-ai";
import { openAICompletionsApi } from "@earendil-works/pi-ai/api/openai-completions.lazy";
import { createRegistry, defineExtension, GenerationTask, Harness, hook, ProviderDoc } from "@earendil-works/pi-durable";
import { openNodeSqliteStorage } from "@earendil-works/pi-durable/storage/sqlite/node";
import { context, sleep } from "./common.ts";

const seen: Array<{ path: string; ctx: string | undefined; auth: string | undefined; model: string }> = [];
const server = createServer((req, res) => {
  let body = "";
  req.on("data", (d) => (body += d));
  req.on("end", () => {
    seen.push({ path: req.url ?? "", ctx: req.headers["x-agent-context"] as string | undefined, auth: req.headers["x-agent-service-token"] as string | undefined, model: JSON.parse(body).model });
    res.writeHead(200, { "content-type": "text/event-stream" });
    const chunk = (delta: object, finish: string | null = null) => `data: ${JSON.stringify({ id: "x", object: "chat.completion.chunk", choices: [{ index: 0, delta, finish_reason: finish }] })}\n\n`;
    res.write(chunk({ role: "assistant", content: "好的" }));
    res.write(chunk({}, "stop"));
    res.write(`data: ${JSON.stringify({ id: "x", object: "chat.completion.chunk", choices: [], usage: { prompt_tokens: 10, completion_tokens: 2, total_tokens: 12 } })}\n\n`);
    res.end("data: [DONE]\n\n");
  });
});
await new Promise<void>((r) => server.listen(0, "127.0.0.1", r));
const baseUrl = `http://127.0.0.1:${(server.address() as AddressInfo).port}/v1`;

/** sessionId → 运行上下文（登记在 beforeRequest 里） */
const bySession = new Map<string, string>();
/** 会话 id → 当前运行的上下文（应用在每次提交前设置） */
const byConversation = new Map<string, string>();

const inner = openAICompletionsApi();
const api = {
  ...inner,
  stream: (m: any, c: any, o: any = {}) => inner.stream(m, c, withContext(o)),
  streamSimple: (m: any, c: any, o: any = {}) => inner.streamSimple(m, c, withContext(o)),
};
function withContext(o: any) {
  const ctx = o.sessionId ? bySession.get(o.sessionId) : undefined;
  return ctx ? { ...o, headers: { ...o.headers, "X-Agent-Context": ctx } } : o;
}

const models = createModels();
const mkProvider = (id: string, modelIds: string[]) =>
  createProvider({
    id, name: id, baseUrl,
    auth: { apiKey: { name: id, resolve: async () => ({ auth: { apiKey: "unused", headers: { "X-Agent-Service-Token": "svc-token" } } }) } },
    models: modelIds.map((mid) => ({ id: mid, name: mid, api: "openai-completions", provider: id, baseUrl, reasoning: false, input: ["text"], cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 }, contextWindow: 100000, maxTokens: 4000 })) as any,
    api: api as any,
  });
models.setProvider(mkProvider("bridge", ["agent-a"]));

const registry = createRegistry();
registry.install(defineExtension({
  name: "run-context",
  hooks: [hook(GenerationTask, {
    beforeRequest: async (_req: any, hookApi: any, ctx: any) => {
      const sid = (await hookApi.snapshot(ProviderDoc, hookApi.conversationId, ctx)).sessionId;
      const run = byConversation.get(String(hookApi.conversationId));
      if (run) bySession.set(sid, run);
      return undefined;
    },
  })],
} as any));

const harness = await Harness.open(await openNodeSqliteStorage(":memory:"), { models, registry }, context);
const a = await harness.createConversation({ ownership: { kind: "ownerless" }, agent: { model: { provider: "bridge", modelId: "agent-a" } } } as any, context);
const b = await harness.createConversation({ ownership: { kind: "ownerless" }, agent: { model: { provider: "bridge", modelId: "agent-a" } } } as any, context);
byConversation.set(String(a.id), JSON.stringify({ run_id: "1001", user_id: "1" }));
byConversation.set(String(b.id), JSON.stringify({ run_id: "2002", user_id: "2" }));

const ask = async (conv: any, text: string) => {
  const sub = await conv.submit({ type: "input", content: text }, context);
  const rec = await sub.wait(context);
  if (rec.status !== "done") console.log("  提交状态:", rec.status, (rec as any).reason ?? "");
  await conv.waitForIdle(context);
  const items = (await conv.entries({}, 10, undefined, context)).items;
  const last = items[0] as any;
  if (process.env.DEBUG) console.log("  记录:", [...items].reverse().map((e: any) => e.kind + (e.data?.errorMessage ? `(${e.data.errorMessage})` : "")).join(" → "), JSON.stringify(last?.data?.errorMessage ?? last?.data?.stopReason ?? ""));
  void sub;
};
await Promise.all([ask(a, "你好 A"), ask(b, "你好 B")]);
console.log("\n两个会话并发各问一次：");
for (const s of seen) console.log(`  model=${s.model} ctx=${s.ctx} service-token=${s.auth}`);

// 同一个会话的第二次运行换了上下文（比如批准后 spent_extra 变了）
seen.length = 0;
byConversation.set(String(a.id), JSON.stringify({ run_id: "1002", user_id: "1", spent_extra: 8 }));
await ask(a, "再问一次");
console.log("\n同一会话的下一轮运行（上下文变了）：");
for (const s of seen) console.log(`  ctx=${s.ctx}`);

// Harness 打开之后再注册新模型（Go 下发的模型清单会变）
seen.length = 0;
models.setProvider(mkProvider("bridge", ["agent-a", "agent-b"]));
const c = await harness.createConversation({ ownership: { kind: "ownerless" }, agent: { model: { provider: "bridge", modelId: "agent-b" } } } as any, context);
byConversation.set(String(c.id), JSON.stringify({ run_id: "3003", user_id: "3" }));
await ask(c, "新模型");
console.log("\n动态加入的新模型：");
for (const s of seen) console.log(`  model=${s.model} ctx=${s.ctx}`);

await harness.close(context);
server.close();
await sleep(10);
