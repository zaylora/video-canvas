import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { WebAccess } from "../src/web-access.ts";

test("联网 MCP：启动与网页请求超时分别说明原因，不泄露供应商信息", async () => {
  const dir = mkdtempSync(join(tmpdir(), "agent-web-timeout-"));
  const timeout = () => Object.assign(new Error("secret-provider-key"), { code: -32001 });
  const starting = new WebAccess({
    dataDir: dir,
    connect: async () => {
      throw timeout();
    },
  });
  const running = new WebAccess({
    dataDir: dir,
    connect: async () => ({
      callTool: async () => {
        throw timeout();
      },
      close: async () => {},
    }),
  });
  try {
    const rc = { user_id: "1", session_id: "2" };
    await assert.rejects(
      starting.call(rc, "web_search", {}),
      (error) =>
        /联网工具启动超时/.test(String(error)) && !String(error).includes("secret-provider-key"),
    );
    await assert.rejects(running.call(rc, "web_search", {}), /联网请求超时/);
  } finally {
    await starting.close();
    await running.close();
    rmSync(dir, { recursive: true, force: true });
  }
});

test("联网 MCP：同会话复用、跨用户隔离，分页只到自己的进程，关闭后释放全部子进程", async () => {
  const dir = mkdtempSync(join(tmpdir(), "agent-web-"));
  const opened: Array<{
    dir: string;
    env: Record<string, string>;
    calls: unknown[];
    closed: boolean;
  }> = [];
  const web = new WebAccess({
    dataDir: dir,
    env: { PATH: process.env.PATH, EXA_API_KEY: "test-key", AGENT_SERVICE_TOKEN: "must-not-leak" },
    connect: async (profile, env) => {
      const entry = { dir: profile, env, calls: [] as unknown[], closed: false };
      opened.push(entry);
      return {
        callTool: async (name, args) => {
          entry.calls.push({ name, args });
          return { content: [{ type: "text", text: name }] };
        },
        close: async () => {
          entry.closed = true;
        },
      };
    },
  });
  try {
    const a = { user_id: "1", session_id: "2" };
    await web.call(a, "web_search", { query: "API" });
    await web.call(a, "get_search_content", { responseId: "cached" });
    await web.call({ user_id: "9", session_id: "2" }, "web_search", { query: "other" });
    assert.equal(opened.length, 2);
    assert.equal(opened[0].calls.length, 2);
    assert.notEqual(opened[0].dir, opened[1].dir);
    assert.equal(opened[0].env.EXA_API_KEY, "test-key");
    assert.equal(opened[0].env.AGENT_SERVICE_TOKEN, undefined, "子进程不能拿到平台令牌");
    const config = JSON.parse(readFileSync(join(opened[0].dir, "web-search.json"), "utf8"));
    assert.equal(config.provider, "exa", "优先使用独立 Key");
    assert.equal(config.allowBrowserCookies, false);
    assert.equal(config.githubClone.enabled, false);
    await assert.rejects(web.call({ user_id: "../escape", session_id: "2" }, "web_search", {}));
    await web.close();
    assert(opened.every((p) => p.closed));
    await assert.rejects(web.call(a, "web_search", {}), /关闭/);
  } finally {
    await web.close();
    rmSync(dir, { recursive: true, force: true });
  }
});

test("联网 MCP：取消信号传给请求，故障后下一次才重连，不泄露底层错误或自动重试", async () => {
  const dir = mkdtempSync(join(tmpdir(), "agent-web-"));
  let connections = 0;
  let calls = 0;
  let closed = 0;
  let seenSignal: AbortSignal | undefined;
  const web = new WebAccess({
    dataDir: dir,
    connect: async () => {
      connections++;
      return {
        callTool: async (_name, _args, signal) => {
          calls++;
          seenSignal = signal;
          throw new Error("secret-provider-key");
        },
        close: async () => {
          closed++;
        },
      };
    },
  });
  try {
    const rc = { user_id: "1", session_id: "2" };
    const abort = new AbortController();
    await assert.rejects(
      web.call(rc, "web_search", {}, abort.signal),
      (e) => !String(e).includes("secret-provider-key"),
    );
    assert.equal(seenSignal, abort.signal);
    assert.equal(connections, 1);
    assert.equal(calls, 1);
    assert.equal(closed, 1);
    await assert.rejects(web.call(rc, "web_search", {}));
    assert.equal(connections, 2);
    assert.equal(calls, 2);
    abort.abort();
    await assert.rejects(web.call(rc, "web_search", {}, abort.signal));
    assert.equal(connections, 2, "已经取消的调用不应新建进程");
  } finally {
    await web.close();
    rmSync(dir, { recursive: true, force: true });
  }
});

test("联网 MCP：进行中的请求取消后回收连接，保留原取消原因", async () => {
  const dir = mkdtempSync(join(tmpdir(), "agent-web-"));
  let started!: () => void;
  const ready = new Promise<void>((resolve) => {
    started = resolve;
  });
  let closed = false;
  const web = new WebAccess({
    dataDir: dir,
    connect: async () => ({
      callTool: async (_name, _args, signal) =>
        await new Promise((_resolve, reject) => {
          signal!.addEventListener("abort", () => reject(signal!.reason), { once: true });
          started();
        }),
      close: async () => {
        closed = true;
      },
    }),
  });
  try {
    const abort = new AbortController();
    const pending = web.call({ user_id: "1", session_id: "2" }, "web_search", {}, abort.signal);
    await ready;
    const reason = new Error("用户停止");
    abort.abort(reason);
    await assert.rejects(pending, (error) => error === reason);
    assert(closed);
  } finally {
    await web.close();
    rmSync(dir, { recursive: true, force: true });
  }
});
