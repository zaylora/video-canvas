import assert from "node:assert/strict";
import { spawn, type ChildProcess } from "node:child_process";
import { test } from "node:test";
import { baseInput, sleep, withBridge } from "./helpers.ts";

// 服务入口：用环境变量配置，启动时把监听地址写到 stderr，收到 SIGTERM 优雅停机。

const ENTRY = new URL("../src/serve.ts", import.meta.url).pathname;
const TOKEN = "s".repeat(40);

interface Started {
  child: ChildProcess;
  url: string;
  stderr: () => string;
  exited: Promise<number | null>;
}

function launch(env: Record<string, string>): { child: ChildProcess; stderr: () => string; exited: Promise<number | null> } {
  const child = spawn("node", ["--disable-warning=ExperimentalWarning", ENTRY], { env: { PATH: process.env.PATH ?? "", ...env }, stdio: ["ignore", "ignore", "pipe"] });
  let err = "";
  child.stderr!.on("data", (c) => (err += c));
  const exited = new Promise<number | null>((r) => child.on("close", r));
  return { child, stderr: () => err, exited };
}

async function start(env: Record<string, string>): Promise<Started> {
  const p = launch({ AGENT_SERVICE_TOKEN: TOKEN, AGENT_LISTEN: "127.0.0.1:0", ...env });
  const end = Date.now() + 10_000;
  while (Date.now() < end) {
    const m = /"addr":"([^"]+)"/.exec(p.stderr());
    if (m) return { ...p, url: "http://" + m[1] };
    await sleep(20);
  }
  p.child.kill();
  assert.fail("服务没有启动：" + p.stderr());
}

test("启动前自检：没配令牌、令牌太短、监听地址不对都直接退出，并说清楚原因", async () => {
  for (const [env, want] of [
    [{}, /AGENT_SERVICE_TOKEN/],
    [{ AGENT_SERVICE_TOKEN: "short" }, /至少 32/],
    [{ AGENT_SERVICE_TOKEN: TOKEN, AGENT_LISTEN: "不是地址" }, /AGENT_LISTEN/],
    [{ AGENT_SERVICE_TOKEN: TOKEN, AGENT_MAX_SEGMENTS: "0" }, /AGENT_MAX_SEGMENTS/],
  ] as const) {
    const p = launch(env);
    assert.equal(await p.exited, 1, JSON.stringify(env));
    assert.match(p.stderr(), want);
    assert.ok(!p.stderr().includes(TOKEN), "日志里不能出现令牌");
  }
});

test("启动后健康检查可用；收到 SIGTERM 正常退出（退出码 0）", async () => {
  const s = await start({});
  assert.equal((await fetch(s.url + "/health")).status, 200);
  s.child.kill("SIGTERM");
  assert.equal(await s.exited, 0);
});

test("SIGTERM 时正在跑的运行被中止、连接以 exit 结束，不向桥报告（运行由 Go 标为中断）", async () => {
  await withBridge({ models: [{ text: "字".repeat(300), slow: 20 }] }, async (b) => {
    const s = await start({});
    const res = await fetch(s.url + "/segments", {
      method: "POST",
      headers: { "content-type": "application/json", "x-agent-service-token": TOKEN, "x-segment-id": "s1" },
      body: JSON.stringify(baseInput(b.url)),
    });
    assert.equal(res.status, 200);
    await sleep(200);
    s.child.kill("SIGTERM");
    const out = (await res.text()).split("\n").filter(Boolean).map((l) => JSON.parse(l));
    assert.equal(out.at(-1).type, "exit");
    assert.equal(await s.exited, 0);
    assert.deepEqual(b.rec.finishes, [], "停机不能向桥报 done：会把被切断的运行标成成功");
  });
});

test("环境变量配置并发上限", async () => {
  await withBridge({ models: [{ text: "字".repeat(300), slow: 20 }] }, async (b) => {
    const s = await start({ AGENT_MAX_SEGMENTS: "1" });
    const post = (id: string) => fetch(s.url + "/segments", {
      method: "POST",
      headers: { "content-type": "application/json", "x-agent-service-token": TOKEN, "x-segment-id": id },
      body: JSON.stringify(baseInput(b.url)),
    });
    const first = await post("a");
    assert.equal((await post("b")).status, 503);
    s.child.kill("SIGTERM");
    await first.text();
    await s.exited;
  });
});
