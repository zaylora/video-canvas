import type { ServerMessage, TaskView } from "@/api/generation-task/type";
import type { ConnectionState } from "@/store/ws";

/** 客户端每隔多久发一次 ping */
export const HEARTBEAT_INTERVAL = 25_000;
/** 这么久没收到任何消息就判定连接已死，主动断开重连 */
export const DEAD_AFTER = 60_000;
/** 检查心跳的节拍 */
export const HEARTBEAT_TICK = 5_000;
export const BACKOFF_BASE = 1_000;
export const BACKOFF_MAX = 30_000;

/**
 * 指数退避（1s -> 30s）加 ±20% 抖动，避免服务重启后所有客户端同一时刻涌回来。
 * attempt 从 0 开始：第 0 次失败后等约 1s，第 1 次约 2s，……
 */
export function backoffDelay(attempt: number, random: () => number = Math.random): number {
  const exp = Math.min(BACKOFF_MAX, BACKOFF_BASE * 2 ** Math.max(0, attempt));
  const jitter = 0.8 + random() * 0.4;
  return Math.min(BACKOFF_MAX, Math.round(exp * jitter));
}

/**
 * 由 API 基地址推出 WebSocket 地址：
 * 绝对地址（开发环境 http://localhost:8080/api/v1）换成 ws(s) 协议；
 * 相对地址（生产同域 /api/v1）沿用当前页面的 host，vite 代理需要 ws: true。
 */
export function buildWsUrl(
  apiBase: string | undefined,
  ticket: string,
  loc: { protocol: string; host: string } = window.location,
): string {
  const base = (apiBase && apiBase.length > 0 ? apiBase : "/api/v1").replace(/\/+$/, "");
  const wsScheme = loc.protocol === "https:" ? "wss:" : "ws:";
  let origin: string;
  let path: string;
  if (/^https?:\/\//i.test(base)) {
    const url = new URL(base);
    origin = `${url.protocol === "https:" ? "wss:" : "ws:"}//${url.host}`;
    path = url.pathname.replace(/\/+$/, "");
  } else {
    origin = `${wsScheme}//${loc.host}`;
    path = base.startsWith("/") ? base : `/${base}`;
  }
  return `${origin}${path}/ws?ticket=${encodeURIComponent(ticket)}`;
}

/** 解析服务端消息；格式不认识的一律丢弃，不让脏数据进 store */
export function parseServerMessage(raw: unknown): ServerMessage | null {
  if (typeof raw !== "string") return null;
  let msg: unknown;
  try {
    msg = JSON.parse(raw);
  } catch {
    return null;
  }
  if (typeof msg !== "object" || msg === null) return null;
  const { type, data } = msg as { type?: unknown; data?: unknown };
  switch (type) {
    case "hello":
    case "pong":
    case "error":
      return msg as ServerMessage;
    case "task.updated": {
      if (typeof data !== "object" || data === null) return null;
      const view = data as Partial<TaskView>;
      if (
        (typeof view.id !== "number" && typeof view.id !== "string") ||
        typeof view.version !== "number" ||
        typeof view.status !== "string"
      ) {
        return null;
      }
      return msg as ServerMessage;
    }
    default:
      return null;
  }
}

/** 客户端用到的 WebSocket 最小接口，方便测试里换成假的 */
export interface SocketLike {
  onopen: ((event: Event) => unknown) | null;
  onmessage: ((event: MessageEvent) => unknown) | null;
  onclose: ((event: CloseEvent) => unknown) | null;
  onerror: ((event: Event) => unknown) | null;
  send(data: string): void;
  close(): void;
}

export type Timers = {
  setTimeout: (fn: () => void, ms: number) => unknown;
  clearTimeout: (handle: unknown) => void;
  setInterval: (fn: () => void, ms: number) => unknown;
  clearInterval: (handle: unknown) => void;
};

export type SocketClientDeps = {
  /** 用 JWT 换一次性 ticket */
  fetchTicket: () => Promise<string>;
  buildUrl: (ticket: string) => string;
  createSocket: (url: string) => SocketLike;
  /** 收到任务快照 */
  onTask: (view: TaskView) => void;
  /** 每次连接成功（包括重连）后调用，用来对账 */
  onOpen: () => void;
  onState: (state: ConnectionState) => void;
  /** 没登录就别连了 */
  canConnect?: () => boolean;
  /** 拿 ticket 时遇到这类错误（比如 401）没必要重试 */
  isFatal?: (error: unknown) => boolean;
  now?: () => number;
  random?: () => number;
  timers?: Timers;
};

/**
 * 用户级 WebSocket 的连接管理：拿 ticket -> 建连 -> 心跳 -> 断线指数退避重连。
 * 纯粹的通道，不认识任务业务；正确性靠 onOpen 里的 HTTP 对账兜底。
 */
export class TaskSocketClient {
  private socket: SocketLike | null = null;
  private state: ConnectionState = "idle";
  private stopped = true;
  private attempt = 0;
  private generation = 0;
  private everConnected = false;
  private reconnectTimer: unknown = null;
  private heartbeatTimer: unknown = null;
  private lastMessageAt = 0;
  private lastPingAt = 0;

  private readonly deps: SocketClientDeps;
  private readonly now: () => number;
  private readonly random: () => number;
  private readonly timers: Timers;

  constructor(deps: SocketClientDeps) {
    this.deps = deps;
    this.now = deps.now ?? Date.now;
    this.random = deps.random ?? Math.random;
    this.timers = deps.timers ?? {
      setTimeout: (fn, ms) => globalThis.setTimeout(fn, ms),
      clearTimeout: (handle) => globalThis.clearTimeout(handle as number),
      setInterval: (fn, ms) => globalThis.setInterval(fn, ms),
      clearInterval: (handle) => globalThis.clearInterval(handle as number),
    };
  }

  getState() {
    return this.state;
  }

  start() {
    if (!this.stopped) return;
    this.stopped = false;
    this.attempt = 0;
    this.everConnected = false;
    this.connect();
  }

  stop() {
    this.stopped = true;
    this.generation += 1;
    this.clearReconnect();
    this.stopHeartbeat();
    this.dropSocket();
    this.setState("closed");
  }

  /** 页面回到前台、网络恢复时调用：正在等退避的就立刻重连，已连上的不动 */
  reconnectNow() {
    if (this.stopped || this.state === "connected" || this.state === "connecting") return;
    this.clearReconnect();
    this.attempt = 0;
    this.connect();
  }

  private setState(state: ConnectionState) {
    if (this.state === state) return;
    this.state = state;
    this.deps.onState(state);
  }

  private clearReconnect() {
    if (this.reconnectTimer !== null) {
      this.timers.clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
  }

  private dropSocket() {
    const socket = this.socket;
    if (!socket) return;
    this.socket = null;
    socket.onopen = socket.onmessage = socket.onclose = socket.onerror = null;
    try {
      socket.close();
    } catch {
      // 已经关了
    }
  }

  private connect() {
    if (this.stopped) return;
    if (this.deps.canConnect && !this.deps.canConnect()) {
      this.stop();
      return;
    }
    this.setState(this.everConnected ? "reconnecting" : "connecting");
    const generation = ++this.generation;

    this.deps
      .fetchTicket()
      .then((ticket) => {
        if (generation !== this.generation || this.stopped) return;
        this.open(this.deps.buildUrl(ticket), generation);
      })
      .catch((error: unknown) => {
        if (generation !== this.generation || this.stopped) return;
        if (this.deps.isFatal?.(error)) {
          this.stop();
          return;
        }
        this.scheduleReconnect();
      });
  }

  private open(url: string, generation: number) {
    let socket: SocketLike;
    try {
      socket = this.deps.createSocket(url);
    } catch {
      this.scheduleReconnect();
      return;
    }
    this.socket = socket;

    socket.onopen = () => {
      if (generation !== this.generation) return;
      this.everConnected = true;
      this.lastMessageAt = this.lastPingAt = this.now();
      this.setState("connected");
      this.startHeartbeat();
      this.deps.onOpen();
    };
    socket.onmessage = (event) => {
      if (generation !== this.generation) return;
      this.lastMessageAt = this.now();
      const msg = parseServerMessage(event.data);
      if (!msg) return;
      // 服务端确认握手后才算稳定，之前的退避计数在这里清零
      if (msg.type === "hello") this.attempt = 0;
      if (msg.type === "task.updated") this.deps.onTask(msg.data);
    };
    socket.onclose = () => {
      if (generation !== this.generation) return;
      this.handleLost();
    };
    socket.onerror = () => {
      // 错误后浏览器一定会再触发 close，重连统一在 onclose 里做
    };
  }

  /** 连接丢了（对端关闭、心跳判死）：收拾现场，排一次退避重连 */
  private handleLost() {
    this.generation += 1;
    this.stopHeartbeat();
    this.dropSocket();
    if (!this.stopped) this.scheduleReconnect();
  }

  private scheduleReconnect() {
    if (this.stopped) return;
    this.setState("reconnecting");
    this.clearReconnect();
    const delay = backoffDelay(this.attempt, this.random);
    this.attempt += 1;
    this.reconnectTimer = this.timers.setTimeout(() => {
      this.reconnectTimer = null;
      this.connect();
    }, delay);
  }

  private startHeartbeat() {
    this.stopHeartbeat();
    this.heartbeatTimer = this.timers.setInterval(() => this.tick(), HEARTBEAT_TICK);
  }

  private stopHeartbeat() {
    if (this.heartbeatTimer !== null) {
      this.timers.clearInterval(this.heartbeatTimer);
      this.heartbeatTimer = null;
    }
  }

  private tick() {
    const now = this.now();
    if (now - this.lastMessageAt >= DEAD_AFTER) {
      this.handleLost();
      return;
    }
    if (now - this.lastPingAt >= HEARTBEAT_INTERVAL) {
      this.lastPingAt = now;
      try {
        this.socket?.send(JSON.stringify({ type: "ping" }));
      } catch {
        this.handleLost();
      }
    }
  }
}
