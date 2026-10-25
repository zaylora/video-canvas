import assert from "node:assert/strict";
import { test } from "node:test";
import { PlatformError, createPlatform, decodeRunContext, encodeRunContext, type RunContext } from "../src/platform-client.ts";
import { fakePlatform } from "./platform-fake.ts";

const TOKEN = "service-token-0123456789abcdef0123456789";
// 雪花大小的 ID：超过 2^53，必须一直是字符串
const rc: RunContext = {
  run_id: "1152921504606846977", user_id: "1", session_id: "1152921504606846978", canvas_id: "7",
  mode: "all", model_key: "claude", selection: ["n_剧本", "n2"], budget_credits: 50, spent_extra: 8,
};

async function withPlatform(fn: (p: ReturnType<typeof createPlatform>, f: Awaited<ReturnType<typeof fakePlatform>>) => Promise<void>) {
  const f = await fakePlatform();
  try {
    await fn(createPlatform({ baseUrl: f.url + "/", serviceToken: TOKEN }), f);
  } finally {
    await f.close();
  }
}

test("运行上下文头：base64url（无填充）的 JSON，ID 保持字符串，能原样还原（含中文）", () => {
  const h = encodeRunContext(rc);
  assert.match(h, /^[A-Za-z0-9_-]+$/, "不含 + / =");
  assert.deepEqual(decodeRunContext(h), rc);
  assert.match(Buffer.from(h, "base64url").toString("utf8"), /"run_id":"1152921504606846977"/);
});

test("运行上下文：身份字段必须是规范的十进制字符串（数字会在 JavaScript 里丢精度）", () => {
  for (const bad of [{ run_id: 1152921504606846977 }, { user_id: "" }, { session_id: "007" }, { canvas_id: "abc" }, { run_id: "0" }] as Array<Partial<RunContext>>) {
    assert.throws(() => encodeRunContext({ ...rc, ...bad } as RunContext), /run_id|user_id|session_id|canvas_id/);
  }
});

test("运行上下文头太大（选中的节点太多）要在本地报错，而不是等 Go 返回 400", () => {
  assert.throws(() => encodeRunContext({ ...rc, selection: Array.from({ length: 2000 }, (_, i) => "n_" + "x".repeat(20) + i) }), /太大/);
});

test("tool：路径、请求头（服务令牌、运行上下文、幂等键）、请求体；返回 data（含 tool_end）", () =>
  withPlatform(async (p, f) => {
    f.reply("POST /internal/platform/v1/tools/", { json: { code: 0, data: { content: "ok", is_error: false, terminate: false, tool_end: { id: "c1", summary: "读取画布" } } } });
    const res = await p.tool(rc, "canvas_get_state", { a: 1 }, { toolCallId: "call_0", idemKey: "task-0192:abc" });
    assert.equal(res.content, "ok");
    assert.equal(res.tool_end?.summary, "读取画布");
    const s = f.seen[0];
    assert.equal(s.method, "POST");
    assert.equal(s.path, "/internal/platform/v1/tools/canvas_get_state");
    assert.equal(s.headers["x-agent-service-token"], TOKEN);
    assert.deepEqual(decodeRunContext(String(s.headers["x-agent-context"])), rc);
    assert.equal(s.headers["x-idempotency-key"], "task-0192:abc");
    assert.deepEqual(s.body, { tool_call_id: "call_0", args: { a: 1 } });
    assert.match(String(s.headers["content-type"]), /application\/json/);
  }));

test("tool：没给幂等键就不带这个头；args 不传按空对象；工具名里的特殊字符要转义", () =>
  withPlatform(async (p, f) => {
    await p.tool(rc, "a/b c", undefined, { toolCallId: "c" });
    assert.equal(f.seen[0].headers["x-idempotency-key"], undefined);
    assert.equal(f.seen[0].path, "/internal/platform/v1/tools/a%2Fb%20c");
    assert.deepEqual((f.seen[0].body as { args: unknown }).args, {});
  }));

test("tool：幂等键格式不对（Go 只认 1–64 个字母数字和 . _ : -）要在本地报错", () =>
  withPlatform(async (p) => {
    for (const bad of ["", "has space", "中文", "x".repeat(65)]) {
      await assert.rejects(p.tool(rc, "t", {}, { toolCallId: "c", idemKey: bad }), /幂等键/);
    }
  }));

test("错误：code 不为 0 的信封、非 2xx、不是 JSON（反向代理的 502 页面）都是 PlatformError，带状态码和业务码", () =>
  withPlatform(async (p, f) => {
    f.reply("POST /internal/platform/v1/tools/", { status: 402, json: { code: 40001, msg: "积分不足" } });
    await assert.rejects(p.tool(rc, "t", {}, { toolCallId: "c" }), (e: PlatformError) => e instanceof PlatformError && e.status === 402 && e.code === 40001 && /积分不足/.test(e.message));
    f.reply("POST /internal/platform/v1/tools/", { status: 200, json: { code: 10001, msg: "参数错误" } });
    await assert.rejects(p.tool(rc, "t", {}, { toolCallId: "c" }), (e: PlatformError) => e.status === 200 && e.code === 10001);
    f.reply("POST /internal/platform/v1/tools/", { status: 502, text: "<html>Bad Gateway</html>" });
    await assert.rejects(p.tool(rc, "t", {}, { toolCallId: "c" }), (e: PlatformError) => e.status === 502 && e.code === undefined && /502/.test(e.message));
  }));

test("连不上：抛 PlatformError（status 0），消息说明地址，区别于业务错误", async () => {
  const p = createPlatform({ baseUrl: "http://127.0.0.1:1", serviceToken: TOKEN });
  await assert.rejects(p.tool(rc, "t", {}, { toolCallId: "c" }), (e: PlatformError) => e instanceof PlatformError && e.status === 0 && /127\.0\.0\.1:1/.test(e.message));
});

test("signal 中止请求：抛出的是中止，不是 PlatformError（调用方要分得清）", () =>
  withPlatform(async (p) => {
    const ac = new AbortController();
    ac.abort();
    await assert.rejects(p.tool(rc, "t", {}, { toolCallId: "c", signal: ac.signal }), (e: Error) => e.name === "AbortError" || /abort/i.test(e.message));
  }));

test("prepare：POST /runs/prepare，请求体 message 和 max_steps；不传请求体字段就是空对象", () =>
  withPlatform(async (p, f) => {
    f.reply("POST /internal/platform/v1/runs/prepare", { json: { code: 0, data: { system_prompt: "你是小镜", tools: [], model: { context_window: 200000, max_tokens: 8192, vision: true }, harness: { key: "abc" } } } });
    const r = await p.prepare(rc, { message: "拆分镜", max_steps: 30 });
    assert.equal(r.system_prompt, "你是小镜");
    assert.deepEqual(f.seen[0].body, { message: "拆分镜", max_steps: 30 });
    await p.prepare(rc);
    assert.deepEqual(f.seen[1].body, {});
  }));

test("审批三接口：propose / resolve / execute 的路径和请求体；审批 id 是字符串", () =>
  withPlatform(async (p, f) => {
    f.reply("POST /internal/platform/v1/approvals/propose", { json: { code: 0, data: { is_error: false, payload: { items: [] }, quote_credits: 5, content: "已提交", terminate: true } } });
    const pr = await p.propose(rc, "generate", { items: [{ nodeId: "n1" }] });
    assert.equal(pr.quote_credits, 5);
    assert.deepEqual(f.seen[0].body, { kind: "generate", args: { items: [{ nodeId: "n1" }] } });

    f.reply("POST /internal/platform/v1/approvals/resolve", { json: { code: 0, data: { status: "approved", decision: {}, quote_credits: 5 } } });
    const rs = await p.resolve(rc, "generate", { items: [] }, { decision: "approve", items: [{ index: 0, approve: true, count: 1 }], add_budget: 10 });
    assert.equal(rs.status, "approved");
    assert.deepEqual(f.seen[1].body, { kind: "generate", payload: { items: [] }, decision: { decision: "approve", items: [{ index: 0, approve: true, count: 1 }], add_budget: 10 } });

    f.reply("POST /internal/platform/v1/approvals/execute", { json: { code: 0, data: { status: "executed", result: { a: 1 }, outcome_text: "已删除 1 个节点" } } });
    const ex = await p.execute(rc, "delete", "1152921504606846983", "call_9", { status: "approved", decision: {}, quote_credits: 0, node_ids: ["n1"] });
    assert.equal(ex.outcome_text, "已删除 1 个节点");
    const b = f.seen[2].body as { approval_id: unknown; outcome: { node_ids: string[] } };
    assert.equal(b.approval_id, "1152921504606846983");
    assert.deepEqual(b.outcome.node_ids, ["n1"]);
    await assert.rejects(p.execute(rc, "delete", "123abc", "c", { status: "approved", decision: {}, quote_credits: 0 }), /审批 id/);
  }));

test("usage 和 endRun：路径、查询参数、方法", () =>
  withPlatform(async (p, f) => {
    f.reply("GET /internal/platform/v1/runs/", { json: { code: 0, data: { spent_credits: 3, context_tokens: 1200, call: { id: "99", status: "settled", credits: 3, charged: 3, estimated: false, input_tokens: 1000, output_tokens: 50 } } } });
    const u = await p.usage(rc, "1152921504606847000");
    assert.equal(u.call?.charged, 3);
    assert.equal(f.seen[0].path, "/internal/platform/v1/runs/1152921504606846977/usage?call_id=1152921504606847000");
    f.reply("GET /internal/platform/v1/runs/", { json: { code: 0, data: { spent_credits: 0, context_tokens: 0 } } });
    await p.usage(rc);
    assert.equal(f.seen[1].path, "/internal/platform/v1/runs/1152921504606846977/usage");
    await p.endRun(rc);
    assert.equal(f.seen[2].method, "DELETE");
    assert.equal(f.seen[2].path, "/internal/platform/v1/runs/1152921504606846977");
  }));
