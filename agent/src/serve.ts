#!/usr/bin/env node
import { configFromEnv, startDurable } from "./durable.ts";
import { createAgentServer, MIN_SERVICE_TOKEN_LEN } from "./server.ts";

/**
 * Agent 服务的入口，用环境变量配置：
 *   AGENT_SERVICE_TOKEN  必填，至少 32 个字符；Go 调这个服务、这个服务调 Go 的桥，都靠它
 *   AGENT_LISTEN         监听地址，默认 0.0.0.0:8090；写 127.0.0.1:0 表示本机随机端口（测试用）
 *   AGENT_MAX_SEGMENTS   同时最多跑几个片段，默认 64
 *   AGENT_HEARTBEAT_MS   心跳间隔，默认 15000
 * 日志写 stderr（JSON 行）。收到 SIGTERM / SIGINT 时中止所有运行、等它们向桥报告结束，再退出。
 *
 * 配了 AGENT_DB_PATH 就是 durable 模式（Pi Durable 引擎 + 前端用的 REST 接口），额外的环境变量见 durable.ts 的 configFromEnv：
 *   AGENT_DB_PATH                  SQLite 文件（放本地卷）
 *   AGENT_BRIDGE_URL               Go 的桥地址，如 http://backend:47001
 *   AGENT_ID_KEY                   对外 ID 的编码密钥，必须和 Go 的 APP_SERVER_ID_KEY 一样
 *   AGENT_WORKER_ID                雪花 ID 的节点号 0..1023，默认 1
 *   AGENT_RESUME_WINDOW_SECONDS    重启后自动续跑的窗口，默认 300
 * 这个模式下收到 SIGTERM 只关闭服务和存储，不中止运行：运行留在原状态，下次启动按窗口续跑或标为中断。
 */
const log = (msg: string, extra: Record<string, unknown> = {}) => process.stderr.write(JSON.stringify({ msg, ...extra }) + "\n");

/** 启动前自检失败：说清楚原因就退出，不要等到第一次请求才发现 */
function die(msg: string): never {
  log(msg);
  process.exit(1);
}

function positiveInt(name: string, fallback: number): number {
  const raw = process.env[name];
  if (raw === undefined || raw === "") return fallback;
  const n = Number(raw);
  if (!Number.isInteger(n) || n <= 0) die(`${name} 必须是正整数，当前是 ${JSON.stringify(raw)}`);
  return n;
}

function parseListen(raw: string): { host: string; port: number } {
  const m = /^(\[[^\]]+\]|[^:\s]+):(\d{1,5})$/.exec(raw);
  if (!m) die(`AGENT_LISTEN 必须是 host:port 形式，当前是 ${JSON.stringify(raw)}`);
  return { host: m[1].replace(/^\[|\]$/g, ""), port: Number(m[2]) };
}

// 两种模式：配了 AGENT_DB_PATH 就是 durable 模式（Pi Durable 引擎 + REST 接口，会话、运行、审批都在这里）；
// 否则是旧的无状态模式（Go 逐个启动运行片段，/segments），阶段 4 删除
if (process.env.AGENT_DB_PATH) {
  let svc;
  try {
    svc = await startDurable(configFromEnv(process.env), log);
  } catch (e) {
    die(String((e as Error)?.message ?? e));
  }
  log("Agent 服务已启动（durable 模式）", { addr: svc.url.replace(/^https?:\/\//, "") });
  process.on("unhandledRejection", (e) => log("未处理的 Promise 异常", { error: String((e as Error)?.message ?? e) }));
  let stopping = false;
  for (const sig of ["SIGTERM", "SIGINT"] as const) {
    process.on(sig, () => {
      if (stopping) return;
      stopping = true;
      log("收到停机信号，关闭服务（运行留在原状态，下次启动按窗口续跑）", { signal: sig });
      // 容器的停止宽限期是 30 秒，留 5 秒给进程退出
      const timer = setTimeout(() => process.exit(1), 25_000);
      svc.close().then(() => process.exit(0), () => process.exit(1)).finally(() => clearTimeout(timer));
    });
  }
} else {
  await startLegacy();
}

async function startLegacy() {
const token = process.env.AGENT_SERVICE_TOKEN ?? "";
if (!token) die("缺少环境变量 AGENT_SERVICE_TOKEN（至少 32 个字符，建议 openssl rand -hex 32）");
if (token.length < MIN_SERVICE_TOKEN_LEN) die(`AGENT_SERVICE_TOKEN 至少 ${MIN_SERVICE_TOKEN_LEN} 个字符`);
const { host, port } = parseListen(process.env.AGENT_LISTEN ?? "0.0.0.0:8090");
const maxSegments = positiveInt("AGENT_MAX_SEGMENTS", 64);
const heartbeatMs = positiveInt("AGENT_HEARTBEAT_MS", 15_000);

// 一个运行里漏掉的 Promise 异常不该拖垮别的运行：记下来继续。
// 同步抛出又没人接住的异常说明进程状态不可信：退出，由容器重启；Go 那边连接断开会把这些运行标为中断
process.on("unhandledRejection", (e) => log("未处理的 Promise 异常", { error: String((e as Error)?.message ?? e) }));
process.on("uncaughtException", (e) => {
  log("未捕获的异常，进程退出", { error: String(e?.message ?? e) });
  process.exit(1);
});

const agent = createAgentServer({ serviceToken: token, maxSegments, heartbeatMs, log: (line) => process.stderr.write(line + "\n") });
const actual = await agent.listen(port, host);
log("Agent 服务已启动", { addr: `${host}:${actual}`, maxSegments });

let stopping = false;
for (const sig of ["SIGTERM", "SIGINT"] as const) {
  process.on(sig, () => {
    if (stopping) return;
    stopping = true;
    log("收到停机信号，中止所有运行", { signal: sig, segments: agent.segmentIds().length });
    // 容器的停止宽限期是 30 秒，留 5 秒给进程退出
    agent.shutdown(25_000).then(() => process.exit(0), () => process.exit(1));
  });
}
}
