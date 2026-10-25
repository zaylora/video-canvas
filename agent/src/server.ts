/**
 * Agent 服务：一个常驻的 HTTP 服务，Go 通过它启动、插话、中止运行片段（每个片段就是原来一个 Node 进程跑的那一段）。
 * 多个片段在同一个进程里并发，各自独立：runAgent 没有进程级的全局状态，每个片段的令牌、历史、控制通道都是自己的。
 *
 * 协议：
 *   POST /segments            请求体是启动参数（和进程模式 stdin 第一行一样），请求头 X-Segment-Id 是 Go 生成的片段 id。
 *                             响应是 NDJSON 流，一直开到片段结束：先一行 {"type":"accepted"}，运行期间定时 {"type":"heartbeat"}，
 *                             最后一行 {"type":"exit","code":0|1,"stderr_tail":"..."}。code 的含义和进程模式的退出码一样：0 报告了结束，1 没能报告。
 *                             这条连接就是租约：Go 关掉它（强杀、退出）服务就中止这个片段，服务崩了 Go 也立刻知道。
 *   POST /segments/:id/steer  {"text":"..."} 运行中插话
 *   POST /segments/:id/abort  中止
 *   GET  /segments            正在跑的片段
 *   GET  /health              健康检查，不要令牌
 * 除健康检查外都要带请求头 X-Agent-Service-Token。
 */
import { timingSafeEqual } from "node:crypto";
import { EventEmitter } from "node:events";
import http from "node:http";
import type { AddressInfo } from "node:net";
import { runAgent, type RunIO } from "./run.ts";
import type { Log, RunInput, RunResult } from "./types.ts";

/** 服务令牌的最短长度：它是这个服务唯一的屏障，和 Go 侧的要求一致 */
export const MIN_SERVICE_TOKEN_LEN = 32;
export const SERVICE_TOKEN_HEADER = "x-agent-service-token";
export const SEGMENT_ID_HEADER = "x-segment-id";

const MAX_BODY_BYTES = 32 << 20; // 启动参数里有对话历史（最大 8MB）和工具声明
const MAX_STEER_BYTES = 64 << 10;
const MAX_TAIL_BYTES = 4 << 10; // 和进程模式一样，只留 stderr 的最后 4KB 给 Go 存诊断
const SEGMENT_ID = /^[A-Za-z0-9_-]{1,64}$/;

export interface ServerOptions {
  serviceToken: string;
  /** 同时最多跑几个片段，默认 64 */
  maxSegments?: number;
  /** 心跳间隔，默认 15 秒 */
  heartbeatMs?: number;
  /** 跑一个片段的函数，默认 runAgent；测试里用来注入异常 */
  run?: (input: RunInput, io: RunIO) => Promise<RunResult>;
  /** 服务自己的日志（一行 JSON），默认写 stderr */
  log?: (line: string) => void;
}

export interface AgentServer {
  server: http.Server;
  /** 开始监听，返回实际端口 */
  listen(port: number, host: string): Promise<number>;
  /** 放弃所有片段（中止、不向桥报告）、等它们收尾（最多 graceMs），然后关闭服务；可以重复调用 */
  shutdown(graceMs: number): Promise<void>;
  segmentIds(): string[];
}

interface Segment {
  id: string;
  startedAt: number;
  control: EventEmitter;
  /** 放弃这个片段：中止它，并且不再向桥报告任何东西（连接断开、服务停机时用） */
  orphan(): void;
  /** 片段结束（含最后一行 exit 写出）后兑现 */
  done: Promise<void>;
}

/** 只留最后 maxBytes 字节的日志 */
class Tail {
  private buf = "";
  private readonly maxBytes: number;
  constructor(maxBytes: number) {
    this.maxBytes = maxBytes;
  }
  push(line: string) {
    this.buf += line;
    if (Buffer.byteLength(this.buf) > this.maxBytes) this.buf = Buffer.from(this.buf).subarray(-this.maxBytes).toString("utf8").replace(/^�+/, "");
  }
  toString() {
    return this.buf;
  }
}

class HttpError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));

export function createAgentServer(opts: ServerOptions): AgentServer {
  if (opts.serviceToken.length < MIN_SERVICE_TOKEN_LEN) throw new Error(`服务令牌至少 ${MIN_SERVICE_TOKEN_LEN} 个字符`);
  const want = Buffer.from(opts.serviceToken);
  const maxSegments = opts.maxSegments ?? 64;
  const heartbeatMs = opts.heartbeatMs ?? 15_000;
  const run = opts.run ?? runAgent;
  const serverLog = opts.log ?? ((line: string) => void process.stderr.write(line + "\n"));
  const segments = new Map<string, Segment>();
  let closing = false;
  let shutdownPromise: Promise<void> | undefined;

  const authorized = (req: http.IncomingMessage) => {
    const got = Buffer.from(String(req.headers[SERVICE_TOKEN_HEADER] ?? ""));
    return got.length === want.length && timingSafeEqual(got, want);
  };

  const json = (res: http.ServerResponse, status: number, body: unknown) => {
    res.writeHead(status, { "content-type": "application/json", "cache-control": "no-store" });
    res.end(JSON.stringify(body));
  };

  async function readBody(req: http.IncomingMessage, limit: number): Promise<string> {
    const chunks: Buffer[] = [];
    let size = 0;
    for await (const c of req) {
      size += (c as Buffer).length;
      if (size > limit) throw new HttpError(413, "请求体太大");
      chunks.push(c as Buffer);
    }
    return Buffer.concat(chunks).toString("utf8");
  }

  function parseObject(body: string): Record<string, unknown> {
    let v: unknown;
    try {
      v = JSON.parse(body);
    } catch {
      throw new HttpError(400, "请求体不是合法的 JSON");
    }
    if (!v || typeof v !== "object" || Array.isArray(v)) throw new HttpError(400, "请求体必须是 JSON 对象");
    return v as Record<string, unknown>;
  }

  /** 启动一个片段：响应保持打开，直到片段结束 */
  function startSegment(id: string, input: RunInput, res: http.ServerResponse) {
    const control = new EventEmitter();
    const tail = new Tail(MAX_TAIL_BYTES);
    const log: Log = (msg, extra = {}) => {
      const line = JSON.stringify({ msg, segment: id, ...extra });
      tail.push(line + "\n");
      serverLog(line);
    };
    const write = (o: unknown) => {
      if (!res.writableEnded && !res.destroyed) res.write(JSON.stringify(o) + "\n");
    };

    res.writeHead(200, { "content-type": "application/x-ndjson", "cache-control": "no-store", "x-accel-buffering": "no" });
    write({ type: "accepted" });
    const heartbeat = setInterval(() => write({ type: "heartbeat" }), heartbeatMs);
    heartbeat.unref();

    // 片段的主人（Go 那条连接）没了，或者服务要停机：中止它，并且切断它和桥的联系。
    // 不能让它再报告结束：报了 done，Go 会把被切断的运行标成「成功」；和进程模式被 SIGKILL 一样，
    // 什么都不报，Go 看到连接结束，才会把运行标为「已中断」，用户可以继续
    let orphaned = false;
    const orphan = () => {
      orphaned = true;
      control.emit("abort");
    };
    const bridgeFetch: typeof fetch = (input, init) => (orphaned ? Promise.reject(new Error("片段已被放弃，不再访问桥")) : fetch(input, init));

    let finished = false;
    // 连接断开就是中止：Go 强杀、退出，或者网络断了，都不能留下没人管的运行
    res.on("close", () => {
      if (!finished) orphan();
    });

    const done = (async () => {
      let code = 0;
      try {
        await run(input, { control, log, fetchImpl: bridgeFetch });
      } catch (e) {
        code = 1;
        log("运行异常退出", { error: String((e as Error)?.message ?? e) });
      } finally {
        finished = true;
        clearInterval(heartbeat);
        segments.delete(id);
      }
      write({ type: "exit", code, stderr_tail: tail.toString() });
      if (!res.writableEnded && !res.destroyed) res.end();
    })();
    segments.set(id, { id, startedAt: Date.now(), control, orphan, done });
  }

  async function handle(req: http.IncomingMessage, res: http.ServerResponse) {
    const url = new URL(req.url ?? "/", "http://agent");
    const path = url.pathname;
    const method = req.method ?? "GET";

    if (method === "GET" && path === "/health") {
      return json(res, closing ? 503 : 200, { ok: !closing, segments: segments.size });
    }
    if (!authorized(req)) throw new HttpError(401, "服务令牌无效");

    if (method === "GET" && path === "/segments") {
      return json(res, 200, { segments: [...segments.values()].map((s) => ({ id: s.id, started_at: s.startedAt })) });
    }

    if (method === "POST" && path === "/segments") {
      const id = String(req.headers[SEGMENT_ID_HEADER] ?? "");
      if (!SEGMENT_ID.test(id)) throw new HttpError(400, "缺少合法的 X-Segment-Id（1-64 位字母、数字、- 或 _）");
      if (closing) throw new HttpError(503, "服务正在关闭");
      if (segments.has(id)) throw new HttpError(409, "这个片段已经在运行");
      if (segments.size >= maxSegments) throw new HttpError(503, `同时运行的片段已达上限（${maxSegments}）`);
      const input = parseObject(await readBody(req, MAX_BODY_BYTES)) as unknown as RunInput;
      // 读请求体期间可能有别的请求占了位置，再查一次
      if (closing || segments.has(id) || segments.size >= maxSegments) throw new HttpError(segments.has(id) ? 409 : 503, "现在不能启动这个片段");
      return startSegment(id, input, res);
    }

    const m = /^\/segments\/([^/]+)\/(steer|abort)$/.exec(path);
    if (method === "POST" && m) {
      const seg = segments.get(m[1]);
      if (!seg) throw new HttpError(404, "没有这个片段在运行");
      if (m[2] === "abort") {
        seg.control.emit("abort");
      } else {
        const text = parseObject(await readBody(req, MAX_STEER_BYTES)).text;
        if (typeof text !== "string") throw new HttpError(400, "text 必须是字符串");
        seg.control.emit("steer", text);
      }
      return json(res, 202, { ok: true });
    }

    throw new HttpError(404, "没有这个接口");
  }

  const server = http.createServer((req, res) => {
    handle(req, res).catch((e) => {
      const status = e instanceof HttpError ? e.status : 500;
      if (!(e instanceof HttpError)) serverLog(JSON.stringify({ msg: "处理请求出错", error: String((e as Error)?.message ?? e) }));
      if (res.headersSent) return void res.destroy();
      json(res, status, { code: status, msg: e instanceof HttpError ? e.message : "服务内部错误" });
      req.resume(); // 没读完的请求体丢掉，免得占着连接
    });
  });

  return {
    server,
    segmentIds: () => [...segments.keys()],
    listen: (port, host) =>
      new Promise<number>((resolve, reject) => {
        server.once("error", reject);
        server.listen(port, host, () => resolve((server.address() as AddressInfo).port));
      }),
    shutdown(graceMs) {
      shutdownPromise ??= (async () => {
        closing = true;
        const running = [...segments.values()];
        for (const s of running) s.orphan(); // 停机不是用户停止：不报告结束，运行由 Go 标为中断
        await Promise.race([Promise.allSettled(running.map((s) => s.done)), sleep(graceMs)]);
        // 响应写完后连接变成空闲，反复关掉空闲连接，等服务自然关闭；超时的直接断开
        const closed = new Promise<void>((r) => server.close(() => r()));
        const idle = setInterval(() => server.closeIdleConnections(), 50);
        await Promise.race([closed, sleep(1000)]);
        clearInterval(idle);
        server.closeAllConnections();
      })();
      return shutdownPromise;
    },
  };
}
