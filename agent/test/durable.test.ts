import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { configFromEnv, startDurable, type DurableConfig } from "../src/durable.ts";
import { createIdCodec } from "../src/ids.ts";
import { fakeGo } from "./fake-go.ts";

const TOKEN = "s".repeat(40);
const ID_KEY = "test-id-key";
const base = { AGENT_SERVICE_TOKEN: TOKEN, AGENT_DB_PATH: "/tmp/x/agent.sqlite", AGENT_BRIDGE_URL: "http://backend:47001", AGENT_ID_KEY: ID_KEY };
const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

test("配置：环境变量解析、默认值，不对的直接说清楚哪一项", () => {
  const c = configFromEnv(base);
  assert.deepEqual(
    [c.dbPath, c.bridgeUrl, c.idKey, c.workerId, c.resumeWindowMs, c.listen, c.serviceToken],
    ["/tmp/x/agent.sqlite", "http://backend:47001", ID_KEY, 1, 300_000, { host: "0.0.0.0", port: 8090 }, TOKEN],
  );
  const full = configFromEnv({ ...base, AGENT_WORKER_ID: "7", AGENT_RESUME_WINDOW_SECONDS: "60", AGENT_LISTEN: "127.0.0.1:9000", AGENT_BRIDGE_URL: "http://b:1/" });
  assert.deepEqual([full.workerId, full.resumeWindowMs, full.listen, full.bridgeUrl], [7, 60_000, { host: "127.0.0.1", port: 9000 }, "http://b:1"], "桥地址去掉结尾的斜杠");
  for (const [over, want] of [
    [{ AGENT_BRIDGE_URL: "" }, /AGENT_BRIDGE_URL/],
    [{ AGENT_BRIDGE_URL: "backend:47001" }, /AGENT_BRIDGE_URL.*http/],
    [{ AGENT_ID_KEY: "" }, /AGENT_ID_KEY.*APP_SERVER_ID_KEY/],
    [{ AGENT_WORKER_ID: "1024" }, /AGENT_WORKER_ID.*0.*1023/],
    [{ AGENT_WORKER_ID: "x" }, /AGENT_WORKER_ID/],
    [{ AGENT_RESUME_WINDOW_SECONDS: "-1" }, /AGENT_RESUME_WINDOW_SECONDS/],
    [{ AGENT_SERVICE_TOKEN: "short" }, /AGENT_SERVICE_TOKEN.*32/],
    [{ AGENT_LISTEN: "不是地址" }, /AGENT_LISTEN/],
  ] as const) {
    assert.throws(() => configFromEnv({ ...base, ...over }), want, JSON.stringify(over));
  }
});

async function withService(fn: (url: string, cfg: DurableConfig, go: Awaited<ReturnType<typeof fakeGo>>, dir: string) => Promise<void>) {
  const dir = mkdtempSync(join(tmpdir(), "durable-"));
  const go = await fakeGo({ turns: [] });
  const cfg = configFromEnv({ ...base, AGENT_DB_PATH: join(dir, "data", "agent.sqlite"), AGENT_BRIDGE_URL: go.url, AGENT_LISTEN: "127.0.0.1:0" });
  try {
    await fn("", cfg, go, dir);
  } finally {
    await go.close();
    rmSync(dir, { recursive: true, force: true });
  }
}

test("启动：建数据目录和库、起 REST；/health 不要令牌；REST 要令牌和用户；数据重启后还在", () =>
  withService(async (_u, cfg, go) => {
    const codec = createIdCodec(ID_KEY);
    const a = await startDurable(cfg, () => {});
    let sessionId = "";
    try {
      const health = await fetch(`${a.url}/health`);
      assert.deepEqual([health.status, await health.json()], [200, { ok: true }]);
      assert.equal((await fetch(`${a.url}/agent/models`)).status, 401, "REST 要令牌");
      const h = { "x-agent-service-token": TOKEN, "x-agent-user": "1", "content-type": "application/json" };
      const models = await (await fetch(`${a.url}/agent/models`, { headers: h })).json() as any;
      assert.equal(models.data[0].key, "claude", "模型清单来自 Go");
      const created = await (await fetch(`${a.url}/canvas/${codec.encode(7n)}/agent/sessions`, { method: "POST", headers: h, body: JSON.stringify({ title: "持久化" }) })).json() as any;
      assert.equal(created.code, 0);
      sessionId = created.data.id;
      assert.ok(go.paths().includes("POST /canvases/check"), "画布归属问了 Go");
    } finally {
      await a.close();
    }
    // 重启：同一个库，会话还在，雪花 id 还能继续分配
    const b = await startDurable(cfg, () => {});
    try {
      const h = { "x-agent-service-token": TOKEN, "x-agent-user": "1" };
      const list = await (await fetch(`${b.url}/canvas/${codec.encode(7n)}/agent/sessions`, { headers: h })).json() as any;
      assert.deepEqual(list.data.map((s: { id: string; title: string }) => [s.id, s.title]), [[sessionId, "持久化"]]);
    } finally {
      await b.close();
    }
  }));

test("事件推送：运行产生的事件按序号成批推给 Go（带临时的文字增量），停机前推完", async () => {
  const dir = mkdtempSync(join(tmpdir(), "durable-"));
  const go = await fakeGo({ turns: [{ text: "你好呀，这是一段需要流一会儿的回答", charDelayMs: 25 }] });
  const cfg = configFromEnv({ ...base, AGENT_DB_PATH: join(dir, "agent.sqlite"), AGENT_BRIDGE_URL: go.url, AGENT_LISTEN: "127.0.0.1:0" });
  const codec = createIdCodec(ID_KEY);
  const svc = await startDurable(cfg, () => {});
  try {
    const h = { "x-agent-service-token": TOKEN, "x-agent-user": "1", "content-type": "application/json" };
    const created = await (await fetch(`${svc.url}/canvas/${codec.encode(7n)}/agent/sessions`, { method: "POST", headers: h, body: "{}" })).json() as any;
    const run = await fetch(`${svc.url}/agent/sessions/${created.data.id}/runs`, { method: "POST", headers: h, body: JSON.stringify({ message: "打个招呼" }) });
    assert.equal(run.status, 202);
    const pushed = () => go.seen.filter((s) => s.path === "/internal/platform/v1/events").flatMap((s) => s.body.events as Array<{ seq: number; type: string; user_id: string; canvas_id: string; run_id: string }>);
    const t0 = Date.now();
    while (!pushed().some((e) => e.type === "run.status" && (e as any).data?.status === "succeeded")) {
      if (Date.now() - t0 > 5000) assert.fail("运行结束的事件没有推给 Go：" + JSON.stringify(pushed().map((e) => e.type)));
      await sleep(10);
    }
    const events = pushed();
    assert.ok(events.every((e) => e.user_id === "1" && e.canvas_id === "7"), "用户和画布 id 是十进制");
    assert.ok(events.some((e) => e.seq === 0 && e.type === "message.delta"), "临时的文字增量也推");
    const done = events.findIndex((e) => e.type === "message.done");
    assert.ok(done >= 0 && events.slice(done).every((e) => e.type !== "message.delta"), "增量不会排在 message.done 之后");
    const persisted = events.filter((e) => e.seq > 0).map((e) => e.seq);
    assert.deepEqual(persisted, [...persisted].sort((a, b) => a - b), "落库的事件序号递增");
    assert.deepEqual(persisted, [...new Set(persisted)], "没有重复");
  } finally {
    await svc.close();
    await go.close();
    rmSync(dir, { recursive: true, force: true });
  }
});

test("停机：关闭后不再接受请求；关闭是幂等的", () =>
  withService(async (_u, cfg) => {
    const a = await startDurable(cfg, () => {});
    await a.close();
    await a.close();
    await assert.rejects(fetch(`${a.url}/health`));
  }));

test("服务入口（真起进程）：启动、/health、收到 SIGTERM 正常退出，日志里没有令牌和密钥", async () => {
  const dir = mkdtempSync(join(tmpdir(), "durable-proc-"));
  const go = await fakeGo({ turns: [] });
  try {
    const env = { PATH: process.env.PATH ?? "", ...base, AGENT_DB_PATH: join(dir, "agent.sqlite"), AGENT_BRIDGE_URL: go.url, AGENT_LISTEN: "127.0.0.1:0" };
    const child = spawn("node", ["--disable-warning=ExperimentalWarning", new URL("../src/serve.ts", import.meta.url).pathname], { env, stdio: ["ignore", "ignore", "pipe"] });
    let err = "";
    child.stderr!.on("data", (c) => (err += c));
    const exited = new Promise<number | null>((r) => child.on("close", r));
    let addr = "";
    for (let i = 0; i < 500 && !addr; i++) {
      addr = /"addr":"([^"]+)"/.exec(err)?.[1] ?? "";
      await sleep(20);
    }
    if (!addr) child.kill("SIGKILL"); // 断言失败也不能留下进程
    assert.ok(addr, "没有启动：" + err);
    assert.match(err, /Agent 服务已启动/);
    assert.equal((await fetch(`http://${addr}/health`)).status, 200);
    child.kill("SIGTERM");
    assert.equal(await exited, 0);
    assert.ok(!err.includes(TOKEN) && !err.includes(ID_KEY), "日志里不能出现令牌和密钥");
  } finally {
    await go.close();
    rmSync(dir, { recursive: true, force: true });
  }
});

test("服务入口：durable 模式缺配置时直接退出并说清楚", async () => {
  const child = spawn("node", ["--disable-warning=ExperimentalWarning", new URL("../src/serve.ts", import.meta.url).pathname], {
    env: { PATH: process.env.PATH ?? "", AGENT_SERVICE_TOKEN: TOKEN, AGENT_DB_PATH: "/tmp/x.sqlite" }, stdio: ["ignore", "ignore", "pipe"],
  });
  let err = "";
  child.stderr!.on("data", (c) => (err += c));
  assert.equal(await new Promise((r) => child.on("close", r)), 1);
  assert.match(err, /AGENT_BRIDGE_URL/);
});

test("ID 密钥自检：和 Go 一致 /health 是 200；不一致 /health 是 503 并说明；Go 改对之后恢复", async () => {
  const dir = mkdtempSync(join(tmpdir(), "durable-"));
  const go = await fakeGo({ turns: [], idKey: "another-go-key" }); // Go 用的密钥和 Agent 的（ID_KEY）不一样
  const cfg = configFromEnv({ ...base, AGENT_DB_PATH: join(dir, "agent.sqlite"), AGENT_BRIDGE_URL: go.url, AGENT_LISTEN: "127.0.0.1:0" });
  const logs: string[] = [];
  const svc = await startDurable(cfg, (m) => logs.push(m), { idCheckIntervalMs: 5 });
  try {
    for (let i = 0; i < 300 && !logs.some((l) => /ID 编码密钥和 Go 不一致/.test(l)); i++) await sleep(10);
    const res = await fetch(`${svc.url}/health`);
    assert.equal(res.status, 503);
    assert.match(((await res.json()) as { error: string }).error, /AGENT_ID_KEY/);
    assert.ok(logs.some((l) => /APP_SERVER_ID_KEY/.test(l)));
  } finally {
    await svc.close();
    await go.close();
  }
  const ok = await fakeGo({ turns: [] }); // 默认密钥和 Agent 的一致
  const svc2 = await startDurable(configFromEnv({ ...base, AGENT_DB_PATH: join(dir, "agent.sqlite"), AGENT_BRIDGE_URL: ok.url, AGENT_LISTEN: "127.0.0.1:0" }), () => {}, { idCheckIntervalMs: 5 });
  try {
    await sleep(100);
    assert.deepEqual([(await fetch(`${svc2.url}/health`)).status], [200]);
    assert.ok(ok.paths().some((p) => p.startsWith("GET /ids/")), "启动后核对过");
  } finally {
    await svc2.close();
    await ok.close();
    rmSync(dir, { recursive: true, force: true });
  }
});
