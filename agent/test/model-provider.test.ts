import assert from "node:assert/strict";
import { test } from "node:test";
import { Harness, MemoryStorage, createRegistry } from "@earendil-works/pi-durable";
import { BACKGROUND_CONTEXT } from "@earendil-works/chord/context";
import { decodeRunContext, encodeRunContext, type RunContext } from "../src/platform-client.ts";
import { PROVIDER_ID, createModelRegistry, createRunContexts, runContextExtension, type ModelSpec } from "../src/model-provider.ts";
import { fakeModelProxy } from "./model-proxy-fake.ts";

const context = BACKGROUND_CONTEXT;
const TOKEN = "service-token-0123456789abcdef0123456789";
const SPEC: ModelSpec = { key: "claude", name: "Claude", context_window: 200000, max_tokens: 8192, vision: true };
const rc = (run: string, over: Partial<RunContext> = {}): RunContext => ({ run_id: run, user_id: "1", session_id: "1152921504606846978", canvas_id: "7", mode: "all", model_key: "claude", ...over });

async function setup() {
  const proxy = await fakeModelProxy("收到");
  const contexts = createRunContexts();
  const calls: Array<{ sessionId: string | undefined; callId: string }> = [];
  const reg = createModelRegistry({ baseUrl: proxy.url, serviceToken: TOKEN, contexts, onCall: (c) => calls.push(c) });
  reg.ensure(SPEC);
  const registry = createRegistry();
  registry.install(runContextExtension(contexts));
  const harness = await Harness.open(new MemoryStorage(), { models: reg.models, registry }, context);
  const conv = () => harness.createConversation({ ownership: { kind: "ownerless" }, agent: { model: { provider: PROVIDER_ID, modelId: "claude" } } }, context);
  const ask = async (c: Awaited<ReturnType<typeof conv>>, text: string) => {
    const sub = await c.submit({ type: "input", content: text }, context);
    return sub.wait(context);
  };
  return { proxy, contexts, calls, reg, harness, conv, ask, close: async () => { await harness.close(context); await proxy.close(); } };
}

test("请求发到平台的模型代理，带服务令牌和这个会话当前的运行上下文", async () => {
  const s = await setup();
  try {
    const c = await s.conv();
    s.contexts.set(String(c.id), rc("1152921504606846977", { spent_extra: 8 }));
    const done = await s.ask(c, "你好");
    assert.equal(done.status, "done");
    const r = s.proxy.requests[0];
    assert.equal(r.path, "/internal/platform/v1/model/chat/completions");
    assert.equal(r.headers["x-agent-service-token"], TOKEN);
    assert.deepEqual(decodeRunContext(String(r.headers["x-agent-context"])), rc("1152921504606846977", { spent_extra: 8 }));
  } finally {
    await s.close();
  }
});

test("两个会话并发各问一次，各带各的运行上下文，不串", async () => {
  const s = await setup();
  try {
    const [a, b] = [await s.conv(), await s.conv()];
    s.contexts.set(String(a.id), rc("1001"));
    s.contexts.set(String(b.id), rc("2002", { user_id: "2" }));
    await Promise.all([s.ask(a, "A"), s.ask(b, "B")]);
    const seen = s.proxy.requests.map((r) => decodeRunContext(String(r.headers["x-agent-context"])).run_id).sort();
    assert.deepEqual(seen, ["1001", "2002"]);
    // 每个请求的内容也对得上它的上下文：A 的运行带的是 A 的消息
    for (const r of s.proxy.requests) {
      const run = decodeRunContext(String(r.headers["x-agent-context"])).run_id;
      const user = (r.body.messages as Array<{ role: string; content: unknown }>).find((m) => m.role === "user");
      assert.match(JSON.stringify(user?.content), run === "1001" ? /A/ : /B/);
    }
  } finally {
    await s.close();
  }
});

test("同一会话的下一轮运行换了上下文（比如批准后已花积分变了），请求带的是新的", async () => {
  const s = await setup();
  try {
    const c = await s.conv();
    s.contexts.set(String(c.id), rc("1001"));
    await s.ask(c, "第一轮");
    s.contexts.set(String(c.id), rc("1002", { spent_extra: 12 }));
    await s.ask(c, "第二轮");
    const ctxs = s.proxy.requests.map((r) => decodeRunContext(String(r.headers["x-agent-context"])));
    assert.deepEqual(ctxs.map((x) => [x.run_id, x.spent_extra ?? 0]), [["1001", 0], ["1002", 12]]);
  } finally {
    await s.close();
  }
});

test("模型调用 id（响应头 X-Agent-Call-Id，十进制字符串，超过 2^53）交给 onCall，带上提供方会话 id", async () => {
  const s = await setup();
  try {
    const c = await s.conv();
    s.contexts.set(String(c.id), rc("1001"));
    await s.ask(c, "你好");
    assert.equal(s.calls.length, 1);
    assert.equal(s.calls[0].callId, "5000000000000000001");
    assert.ok(s.calls[0].sessionId, "要能关联回会话");
    assert.equal(s.contexts.conversationOf(s.calls[0].sessionId!), String(c.id), "按提供方会话 id 能找回是哪个会话");
  } finally {
    await s.close();
  }
});

test("模型规格：窗口、输出上限、是否看图；Harness 打开后可以更新和新增，下一次请求用新的", async () => {
  const s = await setup();
  try {
    const m = s.reg.models.getModel(PROVIDER_ID, "claude")!;
    assert.deepEqual([m.contextWindow, m.maxTokens, m.input], [200000, 8192, ["text", "image"]]);
    assert.equal(s.reg.ensure(SPEC), false, "没变化不重复注册");
    assert.equal(s.reg.ensure({ ...SPEC, context_window: 100000, vision: false }), true);
    const m2 = s.reg.models.getModel(PROVIDER_ID, "claude")!;
    assert.deepEqual([m2.contextWindow, m2.input], [100000, ["text"]]);
    assert.equal(s.reg.ensure({ key: "deepseek", name: "DeepSeek", context_window: 64000, max_tokens: 4096, vision: false }), true);
    assert.ok(s.reg.models.getModel(PROVIDER_ID, "claude"), "新增不影响已有的");
    assert.ok(s.reg.models.getModel(PROVIDER_ID, "deepseek"));
    // 新注册的模型能真的发请求
    const c = await s.harness.createConversation({ ownership: { kind: "ownerless" }, agent: { model: { provider: PROVIDER_ID, modelId: "deepseek" } } }, context);
    s.contexts.set(String(c.id), rc("3003"));
    assert.equal((await s.ask(c, "新模型")).status, "done");
  } finally {
    await s.close();
  }
});

test("兼容老的 OpenAI 网关：请求里没有 store、max_completion_tokens 这些较新的字段", async () => {
  const s = await setup();
  try {
    const c = await s.conv();
    s.contexts.set(String(c.id), rc("1001"));
    await s.ask(c, "你好");
    const body = s.proxy.requests[0].body;
    assert.equal("store" in body, false);
    assert.equal("max_completion_tokens" in body, false);
    assert.equal(body.max_tokens, 8192);
  } finally {
    await s.close();
  }
});

test("编码的运行上下文原样发出（和平台客户端同一个编码）", () => {
  const ctxs = createRunContexts();
  ctxs.set("7", rc("1001", { selection: ["n_剧本"] }));
  assert.equal(ctxs.headerFor("7"), encodeRunContext(rc("1001", { selection: ["n_剧本"] })));
  assert.equal(ctxs.headerFor("404"), undefined);
  ctxs.delete("7");
  assert.equal(ctxs.headerFor("7"), undefined);
});

test("对象存储里的图片：工具把地址放在 data 里，发给上游前还原成真正的地址（不是 data: URL）", async () => {
  const s = await setup();
  try {
    const c = await s.conv();
    s.contexts.set(String(c.id), rc("1001"));
    const sub = await c.submit({ type: "input", content: [{ type: "text", text: "看图" }, { type: "image", data: "https://oss.example/a.jpg", mimeType: "image/jpeg" }, { type: "image", data: "QUJD", mimeType: "image/png" }] }, context);
    await sub.wait(context);
    const user = (s.proxy.requests[0].body.messages as Array<{ role: string; content: unknown }>).find((m) => m.role === "user")!;
    const urls = (user.content as Array<{ type: string; image_url?: { url: string } }>).filter((p) => p.type === "image_url").map((p) => p.image_url!.url);
    assert.deepEqual(urls, ["https://oss.example/a.jpg", "data:image/png;base64,QUJD"], "地址原样，内联图片仍是 data URL");
  } finally {
    await s.close();
  }
});

test("模型代理回错误：HTTP 状态和业务错误码（响应头）交给 onError，不会当成一次成功的调用", async () => {
  const { createServer } = await import("node:http");
  const srv = createServer((req, res) => {
    req.resume();
    req.on("end", () => {
      res.writeHead(402, { "content-type": "application/json", "x-agent-error-code": "60008" });
      res.end(JSON.stringify({ error: { message: "超出本轮积分预算", type: "agent_error" } }));
    });
  });
  await new Promise<void>((r) => srv.listen(0, "127.0.0.1", r));
  const port = (srv.address() as import("node:net").AddressInfo).port;
  const contexts = createRunContexts();
  const errors: Array<{ status: number; code: number | undefined }> = [];
  const calls: unknown[] = [];
  const reg = createModelRegistry({ baseUrl: `http://127.0.0.1:${port}`, serviceToken: TOKEN, contexts, onCall: (c) => calls.push(c), onError: (e) => errors.push({ status: e.status, code: e.code }) });
  reg.ensure(SPEC);
  const registry = createRegistry();
  registry.install(runContextExtension(contexts));
  const harness = await Harness.open(new MemoryStorage(), { models: reg.models, registry, settings: { retry: { enabled: false } } }, context);
  try {
    const c = await harness.createConversation({ ownership: { kind: "ownerless" }, agent: { model: { provider: PROVIDER_ID, modelId: "claude" } } }, context);
    contexts.set(String(c.id), rc("1001"));
    await (await c.submit({ type: "input", content: "你好" }, context)).wait(context);
    assert.deepEqual(errors, [{ status: 402, code: 60008 }]);
    assert.equal(calls.length, 0);
  } finally {
    await harness.close(context);
    await new Promise<void>((r) => srv.close(() => r()));
  }
});
