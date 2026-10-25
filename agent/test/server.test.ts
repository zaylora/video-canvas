import assert from "node:assert/strict";
import { test } from "node:test";
import { runAgent } from "../src/run.ts";
import { createAgentServer, type AgentServer, type ServerOptions } from "../src/server.ts";
import { baseInput, sleep, withBridge } from "./helpers.ts";

// Agent 服务：一个常驻的 HTTP 服务，Go 通过它启动、插话、中止运行片段。
// POST /segments 的响应是一条 NDJSON 流，一直开到片段结束：连接本身就是租约，
// 断开（Go 退出或服务崩溃）双方都能立刻知道。

const TOKEN = "t".repeat(40);

interface Agent {
  url: string;
  agent: AgentServer;
}

async function withAgent(opts: Partial<ServerOptions>, fn: (a: Agent) => Promise<void>) {
  const agent = createAgentServer({ serviceToken: TOKEN, ...opts });
  const port = await agent.listen(0, "127.0.0.1");
  try {
    await fn({ url: `http://127.0.0.1:${port}`, agent });
  } finally {
    await agent.shutdown(500);
  }
}

const headers = (id?: string, token: string | null = TOKEN): Record<string, string> => ({
  "content-type": "application/json",
  ...(token && { "x-agent-service-token": token }),
  ...(id && { "x-segment-id": id }),
});

const post = (a: Agent, path: string, body: unknown, id?: string, token: string | null = TOKEN, signal?: AbortSignal) =>
  fetch(a.url + path, { method: "POST", headers: headers(id, token), body: typeof body === "string" ? body : JSON.stringify(body), signal });

const get = (a: Agent, path: string, token: string | null = TOKEN) => fetch(a.url + path, { headers: headers(undefined, token) });

/** 读完一条 NDJSON 响应 */
async function lines(res: Response): Promise<any[]> {
  return (await res.text()).split("\n").filter(Boolean).map((l) => JSON.parse(l));
}

const ids = async (a: Agent): Promise<string[]> => ((await (await get(a, "/segments")).json()) as { segments: { id: string }[] }).segments.map((s) => s.id);

async function until(cond: () => Promise<boolean> | boolean, what: string, ms = 5000) {
  const end = Date.now() + ms;
  while (Date.now() < end) {
    if (await cond()) return;
    await sleep(20);
  }
  assert.fail("等不到：" + what);
}

const slowText = { text: "字".repeat(300), slow: 20 }; // 约 3 秒的慢回答

test("鉴权：除健康检查外都要带服务令牌，缺了、错了、长度不同都是 401，不进业务", async () => {
  await withAgent({}, async (a) => {
    assert.equal((await fetch(a.url + "/health")).status, 200, "健康检查不要令牌，给容器的 healthcheck 用");
    assert.equal((await post(a, "/segments", {}, "s1", null)).status, 401);
    assert.equal((await post(a, "/segments", {}, "s1", "x".repeat(40))).status, 401);
    assert.equal((await post(a, "/segments", {}, "s1", "x")).status, 401);
    assert.equal((await get(a, "/segments", null)).status, 401);
    assert.equal((await post(a, "/segments/s1/abort", {}, undefined, null)).status, 401);
    assert.deepEqual(await ids(a), [], "被拒绝的请求不能留下片段");
  });
});

test("启动令牌太短就拒绝创建服务：令牌是唯一的屏障", () => {
  assert.throws(() => createAgentServer({ serviceToken: "short" }), /至少 32/);
  assert.throws(() => createAgentServer({ serviceToken: "" }), /至少 32/);
});

test("一次完整的运行：先回 accepted，片段结束回 exit，桥收到工具调用、历史和完成报告", async () => {
  await withBridge({
    models: [{ tools: [{ id: "c1", name: "canvas_get_state", args: { nodeIds: ["a"] } }] }, { text: "读完了" }],
    tool: async () => ({ content: "画布有 3 个节点" }),
  }, async (b) => {
    await withAgent({}, async (a) => {
      const res = await post(a, "/segments", baseInput(b.url), "seg-1");
      assert.equal(res.status, 200);
      assert.match(res.headers.get("content-type") ?? "", /x-ndjson/);
      const out = await lines(res);
      assert.equal(out[0].type, "accepted");
      assert.deepEqual({ type: out.at(-1).type, code: out.at(-1).code }, { type: "exit", code: 0 });
      assert.deepEqual(b.rec.finishes, [{ status: "done", message: "" }]);
      assert.equal(b.rec.tools.length, 1);
      assert.ok(b.rec.states.length >= 2);
      assert.deepEqual(await ids(a), [], "结束后片段已清掉");
    });
  });
});

test("运行中可以看到片段，插话在当前工具结束后注入，下一次模型请求带上", async () => {
  await withBridge({
    models: [{ tools: [{ id: "c1", name: "model_list", args: { kind: "image" } }] }, { text: "收到" }],
    tool: async () => { await sleep(300); return { content: "x" }; },
  }, async (b) => {
    await withAgent({}, async (a) => {
      const res = await post(a, "/segments", baseInput(b.url), "seg-1");
      await until(async () => (await ids(a)).includes("seg-1"), "片段出现在列表里");
      assert.equal((await post(a, "/segments/seg-1/steer", { text: "再暖一点" })).status, 202);
      const out = await lines(res);
      assert.equal(out.at(-1).code, 0);
      assert.match(JSON.stringify(b.rec.chats[1].messages), /再暖一点/);
    });
  });
});

test("中止：运行立即停下，仍然报告完成（状态由 Go 管），片段被清掉", async () => {
  await withBridge({ models: [slowText] }, async (b) => {
    await withAgent({}, async (a) => {
      const res = await post(a, "/segments", baseInput(b.url), "seg-1");
      await sleep(150);
      assert.equal((await post(a, "/segments/seg-1/abort", {})).status, 202);
      const start = Date.now();
      const out = await lines(res);
      assert.ok(Date.now() - start < 2000, "不用等回答写完");
      assert.equal(out.at(-1).code, 0);
      assert.equal(b.rec.finishes[0].status, "done");
      assert.deepEqual(await ids(a), []);
    });
  });
});

test("连接断开就是中止：Go 退出或强杀时服务不会留下孤儿运行", async () => {
  await withBridge({ models: [slowText] }, async (b) => {
    await withAgent({}, async (a) => {
      const ac = new AbortController();
      const res = await post(a, "/segments", baseInput(b.url), "seg-1", TOKEN, ac.signal);
      assert.equal(res.status, 200);
      await sleep(150);
      ac.abort(); // 模拟 Go 那边关了连接
      await until(async () => (await ids(a)).length === 0, "片段被清掉");
      await until(() => b.rec.closed >= 1, "正在进行的模型请求被断开");
      await sleep(100);
      // 和进程模式被 SIGKILL 一样：不再向桥报告。报了 done，Go 会把被切断的运行标成「成功」，而不是「已中断」
      assert.deepEqual(b.rec.finishes, [], "连接断开后不能再向桥报告结束");
    });
  });
});

test("请求不合法：缺片段 id、重复 id、坏 JSON、不是对象、未知片段、坏的插话", async () => {
  await withBridge({ models: [slowText] }, async (b) => {
    await withAgent({}, async (a) => {
      assert.equal((await post(a, "/segments", baseInput(b.url))).status, 400, "缺 X-Segment-Id");
      assert.equal((await post(a, "/segments", baseInput(b.url), "bad id!")).status, 400, "id 只能是字母数字和 - _");
      assert.equal((await post(a, "/segments", "{坏的", "s2")).status, 400);
      assert.equal((await post(a, "/segments", "[]", "s2")).status, 400);
      const first = await post(a, "/segments", baseInput(b.url), "s1");
      assert.equal(first.status, 200);
      assert.equal((await post(a, "/segments", baseInput(b.url), "s1")).status, 409, "同一个 id 不能同时跑两个");
      assert.equal((await post(a, "/segments/nope/steer", { text: "x" })).status, 404);
      assert.equal((await post(a, "/segments/nope/abort", {})).status, 404);
      assert.equal((await post(a, "/segments/s1/steer", { text: 1 })).status, 400);
      assert.equal((await post(a, "/segments/s1/steer", "不是 JSON")).status, 400);
      assert.equal((await fetch(a.url + "/nope", { headers: headers() })).status, 404);
      await post(a, "/segments/s1/abort", {});
      await first.text();
    });
  });
});

test("并发上限：满了回 503，让 Go 知道现在启动不了", async () => {
  await withBridge({ models: [slowText, slowText] }, async (b) => {
    await withAgent({ maxSegments: 1 }, async (a) => {
      const first = await post(a, "/segments", baseInput(b.url), "s1");
      const second = await post(a, "/segments", baseInput(b.url), "s2");
      assert.equal(second.status, 503);
      await post(a, "/segments/s1/abort", {});
      await first.text();
    });
  });
});

test("多个运行在同一个进程里并发，各自独立：令牌、历史、结果互不串", async () => {
  await withBridge({ models: [{ text: "甲的回答" }, { text: "乙的回答" }] }, async (b) => {
    await withAgent({}, async (a) => {
      const [r1, r2] = await Promise.all([
        post(a, "/segments", baseInput(b.url, { token: "tok-a" }), "s1"),
        post(a, "/segments", baseInput(b.url, { token: "tok-b" }), "s2"),
      ]);
      const [o1, o2] = await Promise.all([lines(r1), lines(r2)]);
      assert.equal(o1.at(-1).code, 0);
      assert.equal(o2.at(-1).code, 0);
      assert.equal(b.rec.finishes.length, 2);
      assert.ok(b.rec.finishes.every((f) => f.status === "done"));
      assert.deepEqual(new Set(b.rec.auths), new Set(["Bearer tok-a", "Bearer tok-b"]));
    });
  });
});

test("错误边界：一个运行抛出未处理的异常，只让它自己以 exit 1 结束，别的运行和服务不受影响", async () => {
  await withBridge({ models: [{ text: "正常的回答" }] }, async (b) => {
    const run: ServerOptions["run"] = async (input, io) => {
      if (input.token === "boom") throw new Error("炸了");
      return runAgent(input, io);
    };
    await withAgent({ run }, async (a) => {
      const [bad, good] = await Promise.all([
        post(a, "/segments", baseInput(b.url, { token: "boom" }), "bad"),
        post(a, "/segments", baseInput(b.url, { token: "ok" }), "good"),
      ]);
      const [ob, og] = await Promise.all([lines(bad), lines(good)]);
      assert.equal(ob.at(-1).code, 1);
      assert.match(ob.at(-1).stderr_tail, /炸了/, "原因留在 stderr_tail 里，Go 存进诊断记录");
      assert.equal(og.at(-1).code, 0);
      assert.equal((await fetch(a.url + "/health")).status, 200);
      assert.deepEqual(await ids(a), []);
    });
  });
});

test("心跳：运行期间每隔一段时间发一行，Go 靠它发现服务假死", async () => {
  await withBridge({ models: [{ text: "字".repeat(100), slow: 20 }] }, async (b) => {
    await withAgent({ heartbeatMs: 40 }, async (a) => {
      const out = await lines(await post(a, "/segments", baseInput(b.url), "s1"));
      assert.ok(out.filter((l) => l.type === "heartbeat").length >= 2, JSON.stringify(out.map((l) => l.type)));
      assert.equal(out.at(-1).type, "exit");
    });
  });
});

test("诊断：运行里记的日志随 exit 交给 Go（取最后一小段），不带别的运行的日志", async () => {
  await withBridge({ models: [{ text: "好" }], stateStatus: 401 }, async (b) => {
    await withAgent({}, async (a) => {
      const out = await lines(await post(a, "/segments", baseInput(b.url), "s1"));
      assert.match(out.at(-1).stderr_tail, /保存对话历史失败/);
    });
  });
});

test("优雅停机：中止所有运行，以 exit 1 结束连接，不向桥报告（Go 据此把运行标为中断，而不是成功）", async () => {
  await withBridge({ models: [slowText, slowText] }, async (b) => {
    const agent = createAgentServer({ serviceToken: TOKEN });
    const port = await agent.listen(0, "127.0.0.1");
    const a = { url: `http://127.0.0.1:${port}`, agent };
    const r1 = await post(a, "/segments", baseInput(b.url), "s1");
    const r2 = await post(a, "/segments", baseInput(b.url), "s2");
    await sleep(150);
    const start = Date.now();
    await agent.shutdown(3000);
    assert.ok(Date.now() - start < 2500, "不用等慢回答写完");
    const [o1, o2] = [await lines(r1), await lines(r2)];
    assert.deepEqual([o1.at(-1).type, o1.at(-1).code, o2.at(-1).type, o2.at(-1).code], ["exit", 1, "exit", 1], "没报告结束，和进程被杀一样退出码是 1");
    assert.deepEqual(agent.segmentIds(), []);
    assert.deepEqual(b.rec.finishes, [], "停机不能向桥报 done：会把被切断的运行标成成功");
  });
});
