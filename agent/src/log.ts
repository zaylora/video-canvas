import { randomUUID } from "node:crypto";
import { hostname } from "node:os";

import type { FastifyInstance } from "fastify";

import type { PublishedEvent } from "./engine/engine.ts";

/**
 * Agent 服务的日志：一行一条 JSON 写标准输出，字段和 Go 后端的统一服务器日志对齐（设计见 docs/design/统一服务器日志 6.10），
 * 运维用同一个 run_id / request_id 能在两个进程的日志里 grep 到同一次运行。
 *   time / level / msg / service / instance，运行相关的带 event、run_kind=agent、run_id（十进制运行编号）、user_id
 */

export type Level = "debug" | "info" | "warn" | "error";

const RANK: Record<Level, number> = { debug: 0, info: 1, warn: 2, error: 3 };

/** 日志函数：直接调用是旧式写法（带 error 字段按 error 级，否则 info）；at 指定级别 */
export interface Logger {
  (msg: string, extra?: Record<string, unknown>): void;
  at(level: Level, msg: string, extra?: Record<string, unknown>): void;
}

export interface LoggerOptions {
  /** 写一行（不含换行），默认写标准输出 */
  write?: (line: string) => void;
  /** 最低级别，默认 info；也可用环境变量 AGENT_LOG_LEVEL */
  level?: string;
  service?: string;
  /** 实例名，默认主机名 */
  instance?: string;
  now?: () => Date;
}

export function createLogger(o: LoggerOptions = {}): Logger {
  const write = o.write ?? ((line: string) => void process.stdout.write(line + "\n"));
  const min = RANK[(o.level as Level) ?? "info"] ?? RANK.info;
  const base = { service: o.service ?? "agent", instance: o.instance ?? hostname() };
  const now = o.now ?? (() => new Date());
  const at = (level: Level, msg: string, extra: Record<string, unknown> = {}) => {
    if (RANK[level] < min) return;
    write(JSON.stringify({ time: localISO(now()), level, msg, ...base, ...extra }));
  };
  const log = ((msg: string, extra: Record<string, unknown> = {}) =>
    at("error" in extra ? "error" : "info", msg, extra)) as Logger;
  log.at = at;
  return log;
}

/** 把旧式日志函数（只有 msg 和附加字段）包成 Logger：已经是 Logger 的原样返回，否则级别放进附加字段 */
export function asLogger(fn: (msg: string, extra?: Record<string, unknown>) => void): Logger {
  if (typeof (fn as Partial<Logger>).at === "function") return fn as Logger;
  const log = ((msg: string, extra?: Record<string, unknown>) => fn(msg, extra)) as Logger;
  log.at = (level, msg, extra = {}) => fn(msg, { level, ...extra });
  return log;
}

/** 本地时区的 ISO 时间，和 Go（zap ISO8601）的写法一致：2026-11-14T17:21:22.950+0800 */
function localISO(d: Date): string {
  const off = -d.getTimezoneOffset();
  const local = new Date(d.getTime() + off * 60_000).toISOString().slice(0, 23);
  const sign = off >= 0 ? "+" : "-";
  const abs = Math.abs(off);
  return `${local}${sign}${String(Math.floor(abs / 60)).padStart(2, "0")}${String(abs % 60).padStart(2, "0")}`;
}

/** 十进制用户编号转数字（和 Go 的 user_id 类型一致）；超出安全整数范围时保留字符串 */
function userId(id: string): number | string {
  const n = Number(id);
  return Number.isSafeInteger(n) ? n : id;
}

/** 运行状态到运行日志事件：排队是创建，进行中是开始，等审批 / 等输入是过程，其余都是终态 */
const STATUS_EVENT: Record<string, string> = {
  queued: "run.create",
  running: "run.start",
  waiting_approval: "run.step",
  waiting_input: "run.step",
};

/** 正常结束、不算失败的终态：用 info；其他终态（失败、超时、中断、等待超时、取消）用 warn */
const QUIET_END = new Set(["succeeded", "budget_exhausted", "step_limit"]);

/** 把引擎的 run.status 事件记成运行日志；其他事件不记（它们已经推给前端，并在 Go 的运行记录里） */
export function logRunEvent(log: Logger, ev: PublishedEvent): void {
  if (ev.type !== "run.status") return;
  const p = (ev.payload ?? {}) as Record<string, unknown>;
  const status = String(p.status ?? "");
  const event = STATUS_EVENT[status] ?? "run.finish";
  const level: Level = event !== "run.finish" || QUIET_END.has(status) ? "info" : "warn";
  log.at(level, "Agent 运行状态", {
    event,
    status,
    run_kind: "agent",
    run_id: ev.runId,
    user_id: userId(ev.userId),
    session_id: ev.sessionId,
    canvas_id: ev.canvasId,
    ...(p.error ? { error: p.error } : {}),
    ...(p.resumed !== undefined ? { resumed: p.resumed } : {}),
  });
}

/** 每个请求结束时打一行 http.request：request_id 沿用 Go 转发过来的 X-Request-ID，没有时生成一个 */
export function installRequestLog(app: FastifyInstance, log: Logger): void {
  app.addHook("onResponse", async (req, reply) => {
    const status = reply.statusCode;
    const user = req.headers["x-agent-user"];
    log.at(status >= 500 ? "error" : status >= 400 ? "warn" : "info", "http request", {
      event: "http.request",
      request_id: String(req.headers["x-request-id"] ?? randomUUID()),
      method: req.method,
      path: req.url.split("?")[0],
      status,
      latency_ms: Math.round(reply.elapsedTime),
      ...(typeof user === "string" && user ? { user_id: userId(user) } : {}),
    });
  });
}
