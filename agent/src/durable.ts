/**
 * 长驻的 Agent 服务（durable 模式）：Pi Durable 驱动的运行引擎 + 前端用的 REST 接口，应用数据和 Pi 的数据在同一个 SQLite 文件里。
 * 用环境变量配置（见 configFromEnv）；Go 反向代理 /api/v1/agent/* 过来，Agent 经平台 API 向 Go 要它才有的东西。
 */
import { mkdirSync } from "node:fs";
import { dirname } from "node:path";
import { openNodeSqliteStorage } from "@earendil-works/pi-durable/storage/sqlite/node";
import { createApiApp } from "./api/http.ts";
import { AgentApiService } from "./api/service.ts";
import { AgentEngine } from "./engine/engine.ts";
import { createIdCodec } from "./ids.ts";
import { createPlatform } from "./platform-client.ts";
import { openStore } from "./store/store.ts";

const MIN_TOKEN_LEN = 32;
const DEFAULT_RESUME_WINDOW_SECONDS = 300;

export interface DurableConfig {
  /** SQLite 文件（应用数据和 Pi 的数据同一个文件，Pi 的表带 pi_ 前缀）。放在本地卷上，不要放网络盘 */
  dbPath: string;
  /** Go 的桥地址（平台 API 和模型代理），如 http://backend:47001 */
  bridgeUrl: string;
  serviceToken: string;
  /** 对外 ID 的编码密钥，必须和 Go 的 APP_SERVER_ID_KEY 一样，否则前端拿到的 ID 对不上 */
  idKey: string;
  /** 雪花 ID 的工作节点号 0..1023；现在只有一个 Agent 容器，默认 1。以后分片每个分片要不同 */
  workerId: number;
  /** 启动时没跑完的运行，停机不超过这么久就自动续跑（毫秒） */
  resumeWindowMs: number;
  listen: { host: string; port: number };
}

function fail(msg: string): never {
  throw new Error(msg);
}

/** 从环境变量读配置；不对就抛错，说清楚是哪一项（调用方打印后退出，不要等到第一次请求才发现） */
export function configFromEnv(env: Record<string, string | undefined>): DurableConfig {
  const need = (name: string, hint = ""): string => env[name] || fail(`缺少环境变量 ${name}${hint}`);
  const int = (name: string, def: number, min: number, max: number): number => {
    const raw = env[name];
    if (raw === undefined || raw === "") return def;
    const n = Number(raw);
    if (!Number.isInteger(n) || n < min || n > max) fail(`${name} 必须是 ${min} 到 ${max} 的整数，当前是 ${JSON.stringify(raw)}`);
    return n;
  };
  const serviceToken = need("AGENT_SERVICE_TOKEN", "（至少 32 个字符，建议 openssl rand -hex 32）");
  if (serviceToken.length < MIN_TOKEN_LEN) fail(`AGENT_SERVICE_TOKEN 至少 ${MIN_TOKEN_LEN} 个字符`);
  const bridgeUrl = need("AGENT_BRIDGE_URL", "（Go 的桥地址，如 http://backend:47001）").replace(/\/+$/, "");
  if (!/^https?:\/\/[^/\s]+/.test(bridgeUrl)) fail(`AGENT_BRIDGE_URL 必须以 http:// 或 https:// 开头，当前是 ${JSON.stringify(bridgeUrl)}`);
  const listenRaw = env.AGENT_LISTEN || "0.0.0.0:8090";
  const m = /^(\[[^\]]+\]|[^:\s]+):(\d{1,5})$/.exec(listenRaw);
  if (!m) fail(`AGENT_LISTEN 必须是 host:port 形式，当前是 ${JSON.stringify(listenRaw)}`);
  return {
    dbPath: need("AGENT_DB_PATH"),
    bridgeUrl,
    serviceToken,
    idKey: need("AGENT_ID_KEY", "（必须和 Go 的 APP_SERVER_ID_KEY 一样）"),
    workerId: int("AGENT_WORKER_ID", 1, 0, 1023),
    resumeWindowMs: int("AGENT_RESUME_WINDOW_SECONDS", DEFAULT_RESUME_WINDOW_SECONDS, 0, 7 * 24 * 3600) * 1000,
    listen: { host: m[1].replace(/^\[|\]$/g, ""), port: Number(m[2]) },
  };
}

export interface DurableService {
  /** 实际监听的地址，如 http://127.0.0.1:8090 */
  url: string;
  engine: AgentEngine;
  /** 停机：不再接受请求，等进行中的请求处理完，然后关闭引擎（不中止运行，下次启动按窗口续跑）和数据库。幂等 */
  close(): Promise<void>;
}

export async function startDurable(cfg: DurableConfig, log: (msg: string, extra?: Record<string, unknown>) => void): Promise<DurableService> {
  mkdirSync(dirname(cfg.dbPath), { recursive: true });
  const codec = createIdCodec(cfg.idKey);
  const store = openStore(cfg.dbPath, { workerId: cfg.workerId });
  let engine: AgentEngine | undefined;
  try {
    const platform = createPlatform({ baseUrl: cfg.bridgeUrl, serviceToken: cfg.serviceToken });
    engine = await AgentEngine.open({
      store, platform, baseUrl: cfg.bridgeUrl, serviceToken: cfg.serviceToken, codec,
      storage: await openNodeSqliteStorage(cfg.dbPath, { prefix: "pi_" }), resumeWindowMs: cfg.resumeWindowMs,
      // 事件推给 Go（再经 WebSocket 发给前端）的入口在 3.4；之前前端靠 HTTP 回放（events / history）对账
    });
    const app = createApiApp({ service: new AgentApiService({ store, engine, platform, codec }), codec, serviceToken: cfg.serviceToken, log });
    app.get("/health", async () => ({ ok: true }));
    const url = await app.listen({ host: cfg.listen.host, port: cfg.listen.port });
    let closing: Promise<void> | undefined;
    const eng = engine;
    return {
      url, engine: eng,
      close: () => (closing ??= (async () => {
        await app.close();
        await eng.close();
        store.close();
      })()),
    };
  } catch (e) {
    await engine?.close().catch(() => {});
    store.close();
    throw e;
  }
}
