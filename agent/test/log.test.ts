import assert from "node:assert/strict";
import { test } from "node:test";

import Fastify from "fastify";

import type { PublishedEvent } from "../src/engine/engine.ts";
import { createLogger, installRequestLog, logRunEvent } from "../src/log.ts";

/** 把日志收进数组，每行解析成对象 */
function capture(level?: string) {
  const lines: Record<string, unknown>[] = [];
  const log = createLogger({
    write: (line) => lines.push(JSON.parse(line)),
    level,
    instance: "agent-1",
    now: () => new Date("2026-11-14T09:21:22.950Z"),
  });
  return { log, lines };
}

const ev = (status: string, extra: Record<string, unknown> = {}): PublishedEvent => ({
  id: "1",
  sessionId: "10",
  runId: "88213",
  userId: "42",
  canvasId: "7",
  seq: 1,
  type: "run.status",
  payload: { status, ...extra },
  createdAt: 0,
});

test("日志是一行一条 JSON：时间、级别、消息、服务、实例和附加字段", () => {
  const { log, lines } = capture();
  log("Agent 服务已启动", { addr: "0.0.0.0:8090" });
  log("推送事件给平台失败", { error: "ECONNREFUSED" });
  assert.equal(lines.length, 2);
  assert.match(String(lines[0].time), /^2026-11-14T\d\d:21:22\.950[+-]\d{4}$/);
  assert.deepEqual(
    {
      level: lines[0].level,
      msg: lines[0].msg,
      service: lines[0].service,
      instance: lines[0].instance,
      addr: lines[0].addr,
    },
    {
      level: "info",
      msg: "Agent 服务已启动",
      service: "agent",
      instance: "agent-1",
      addr: "0.0.0.0:8090",
    },
  );
  assert.equal(lines[1].level, "error", "带 error 字段的旧式调用按 error 级输出");
});

test("低于配置级别的日志不输出", () => {
  const { log, lines } = capture("warn");
  log.at("info", "运行开始");
  log.at("warn", "运行失败");
  assert.deepEqual(
    lines.map((l) => l.msg),
    ["运行失败"],
  );
});

test("运行状态事件映射成统一的运行日志：run_kind=agent、run_id 是十进制运行编号", () => {
  const { log, lines } = capture();
  logRunEvent(log, ev("queued"));
  logRunEvent(log, ev("running", { resumed: true }));
  logRunEvent(log, ev("waiting_approval"));
  logRunEvent(log, ev("succeeded"));
  logRunEvent(log, ev("failed", { error: "模型调用失败" }));
  logRunEvent(log, { ...ev("running"), type: "message.user" });

  assert.deepEqual(
    lines.map((l) => [l.event, l.status, l.level]),
    [
      ["run.create", "queued", "info"],
      ["run.start", "running", "info"],
      ["run.step", "waiting_approval", "info"],
      ["run.finish", "succeeded", "info"],
      ["run.finish", "failed", "warn"],
    ],
  );
  for (const l of lines) {
    assert.equal(l.run_kind, "agent");
    assert.equal(l.run_id, "88213");
    assert.equal(l.user_id, 42);
    assert.equal(l.session_id, "10");
  }
  assert.equal(lines[1].resumed, true);
  assert.equal(lines[4].error, "模型调用失败");
});

test("每个请求一行 http.request：沿用 Go 转发的 X-Request-ID，带用户、状态和耗时", async () => {
  const { log, lines } = capture();
  const app = Fastify();
  installRequestLog(app, log);
  app.get("/sessions/:id", async () => ({ ok: true }));
  app.get("/boom", async (_req, reply) => reply.code(500).send({}));
  await app.inject({
    method: "GET",
    url: "/sessions/9?x=1",
    headers: { "x-request-id": "req-abc", "x-agent-user": "42" },
  });
  await app.inject({ method: "GET", url: "/boom" });
  await app.close();

  assert.equal(lines.length, 2);
  const [ok, boom] = lines;
  assert.deepEqual(
    {
      event: ok.event,
      request_id: ok.request_id,
      method: ok.method,
      path: ok.path,
      status: ok.status,
      user_id: ok.user_id,
      level: ok.level,
    },
    {
      event: "http.request",
      request_id: "req-abc",
      method: "GET",
      path: "/sessions/9",
      status: 200,
      user_id: 42,
      level: "info",
    },
  );
  assert.equal(typeof ok.latency_ms, "number");
  assert.equal(boom.level, "error");
  assert.ok(boom.request_id, "没带 X-Request-ID 时也要有一个编号");
});
