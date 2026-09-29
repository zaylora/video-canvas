import { describe, expect, test } from "bun:test";

import type { TaskView } from "@/api/generation-task/type";
import type { ConnectionState } from "@/store/ws";

import {
  BACKOFF_MAX,
  DEAD_AFTER,
  HEARTBEAT_INTERVAL,
  HEARTBEAT_TICK,
  TaskSocketClient,
  backoffDelay,
  buildWsUrl,
  parseServerMessage,
  type SocketLike,
  type Timers,
} from "@/utils/ws/socket-client";

describe("backoffDelay：指数退避 1s -> 30s，加抖动", () => {
  test("无抖动（random=0.5）时严格翻倍并封顶", () => {
    const delays = [0, 1, 2, 3, 4, 5, 6, 10].map((attempt) => backoffDelay(attempt, () => 0.5));
    expect(delays).toEqual([1000, 2000, 4000, 8000, 16000, 30000, 30000, 30000]);
  });

  test("抖动在 ±20% 内，且永远不超过上限", () => {
    for (const attempt of [0, 1, 3, 8]) {
      const base = Math.min(BACKOFF_MAX, 1000 * 2 ** attempt);
      const low = backoffDelay(attempt, () => 0);
      const high = backoffDelay(attempt, () => 1);
      expect(low).toBeGreaterThanOrEqual(Math.round(base * 0.8));
      expect(high).toBeLessThanOrEqual(BACKOFF_MAX);
      expect(high).toBeGreaterThanOrEqual(low);
    }
  });
});

describe("buildWsUrl", () => {
  const loc = { protocol: "http:", host: "localhost:5173" };

  test("开发环境绝对地址：换成 ws 协议，直连后端", () => {
    expect(buildWsUrl("http://localhost:8080/api/v1", "t 1", loc)).toBe(
      "ws://localhost:8080/api/v1/ws?ticket=t%201",
    );
    expect(buildWsUrl("https://api.example.com/api/v1/", "x", loc)).toBe(
      "wss://api.example.com/api/v1/ws?ticket=x",
    );
  });

  test("相对地址：沿用页面 host，https 页面用 wss", () => {
    expect(buildWsUrl("/api/v1", "x", loc)).toBe("ws://localhost:5173/api/v1/ws?ticket=x");
    expect(buildWsUrl(undefined, "x", { protocol: "https:", host: "app.example.com" })).toBe(
      "wss://app.example.com/api/v1/ws?ticket=x",
    );
  });
});

describe("parseServerMessage", () => {
  test("认识的消息通过，脏数据丢弃", () => {
    expect(parseServerMessage('{"type":"pong"}')).toEqual({ type: "pong" });
    expect(parseServerMessage('{"type":"hello","data":{"server_time":"x"}}')?.type).toBe("hello");
    expect(parseServerMessage("not json")).toBeNull();
    expect(parseServerMessage('{"type":"whatever"}')).toBeNull();
    expect(parseServerMessage(new ArrayBuffer(1))).toBeNull();
    // task.updated 必须带 id / version / status
    expect(parseServerMessage('{"type":"task.updated","data":{"id":1}}')).toBeNull();
    expect(
      parseServerMessage('{"type":"task.updated","channel":"user:1","data":{"id":1,"version":2,"status":"running"}}')
        ?.type,
    ).toBe("task.updated");
  });
});

// ---- 假的 WebSocket 与定时器，让重连、心跳流程可以确定性地推进 ----

class FakeSocket implements SocketLike {
  onopen: SocketLike["onopen"] = null;
  onmessage: SocketLike["onmessage"] = null;
  onclose: SocketLike["onclose"] = null;
  onerror: SocketLike["onerror"] = null;
  sent: string[] = [];
  closed = false;
  constructor(readonly url: string) {}
  send(data: string) {
    this.sent.push(data);
  }
  close() {
    this.closed = true;
  }
  open() {
    this.onopen?.(new Event("open"));
  }
  message(payload: unknown) {
    this.onmessage?.({ data: typeof payload === "string" ? payload : JSON.stringify(payload) } as MessageEvent);
  }
  drop() {
    this.onclose?.({} as CloseEvent);
  }
}

function setup(options: { ticket?: () => Promise<string>; canConnect?: () => boolean; isFatal?: (e: unknown) => boolean } = {}) {
  let now = 0;
  let nextId = 1;
  const pending = new Map<number, { at: number; fn: () => void; every?: number }>();
  const timers: Timers = {
    setTimeout: (fn, ms) => {
      const id = nextId++;
      pending.set(id, { at: now + ms, fn });
      return id;
    },
    clearTimeout: (handle) => void pending.delete(handle as number),
    setInterval: (fn, ms) => {
      const id = nextId++;
      pending.set(id, { at: now + ms, fn, every: ms });
      return id;
    },
    clearInterval: (handle) => void pending.delete(handle as number),
  };
  /** 把假时钟推进 ms，按时间顺序触发到期的定时器 */
  const advance = async (ms: number) => {
    const end = now + ms;
    for (;;) {
      const due = [...pending.entries()]
        .filter(([, timer]) => timer.at <= end)
        .sort((a, b) => a[1].at - b[1].at)[0];
      if (!due) break;
      const [id, timer] = due;
      now = timer.at;
      if (timer.every) timer.at += timer.every;
      else pending.delete(id);
      timer.fn();
      await flush();
    }
    now = end;
  };
  const flush = async () => {
    for (let i = 0; i < 5; i++) await Promise.resolve();
  };

  const sockets: FakeSocket[] = [];
  const states: ConnectionState[] = [];
  const tasks: TaskView[] = [];
  let opens = 0;
  let tickets = 0;
  const client = new TaskSocketClient({
    fetchTicket:
      options.ticket ??
      (async () => {
        tickets += 1;
        return `ticket-${tickets}`;
      }),
    buildUrl: (ticket) => `ws://test/ws?ticket=${ticket}`,
    createSocket: (url) => {
      const socket = new FakeSocket(url);
      sockets.push(socket);
      return socket;
    },
    onTask: (view) => tasks.push(view),
    onOpen: () => {
      opens += 1;
    },
    onState: (state) => states.push(state),
    canConnect: options.canConnect,
    isFatal: options.isFatal,
    now: () => now,
    random: () => 0.5,
    timers,
  });
  return { client, sockets, states, tasks, advance, flush, opens: () => opens, pendingCount: () => pending.size };
}

describe("TaskSocketClient", () => {
  test("先拿 ticket 再建连；连上后进入 connected 并触发对账回调", async () => {
    const t = setup();
    t.client.start();
    expect(t.states).toEqual(["connecting"]);
    await t.flush();
    expect(t.sockets[0].url).toBe("ws://test/ws?ticket=ticket-1");
    t.sockets[0].open();
    expect(t.client.getState()).toBe("connected");
    expect(t.opens()).toBe(1);
  });

  test("收到 task.updated 交给回调，其他消息不影响", async () => {
    const t = setup();
    t.client.start();
    await t.flush();
    t.sockets[0].open();
    t.sockets[0].message({ type: "hello", data: {} });
    t.sockets[0].message({ type: "task.updated", channel: "user:1", data: { id: 1, version: 2, status: "running" } });
    t.sockets[0].message("garbage");
    expect(t.tasks.map((task) => task.version)).toEqual([2]);
  });

  test("断线后按退避重连（1s、2s、4s…），每次重新拿 ticket，重连成功再次对账", async () => {
    const t = setup();
    t.client.start();
    await t.flush();
    t.sockets[0].open();
    t.sockets[0].message({ type: "hello" });

    t.sockets[0].drop();
    expect(t.client.getState()).toBe("reconnecting");
    await t.advance(999);
    expect(t.sockets.length).toBe(1);
    await t.advance(1);
    expect(t.sockets.length).toBe(2);
    expect(t.sockets[1].url).toContain("ticket-2");

    // 第二次连接失败（还没 open 就关了，没有 hello）：退避继续增长
    t.sockets[1].drop();
    await t.advance(1999);
    expect(t.sockets.length).toBe(2);
    await t.advance(1);
    expect(t.sockets.length).toBe(3);

    t.sockets[2].open();
    expect(t.client.getState()).toBe("connected");
    expect(t.opens()).toBe(2);
  });

  test("收到 hello 后退避计数清零", async () => {
    const t = setup();
    t.client.start();
    await t.flush();
    t.sockets[0].open();
    t.sockets[0].message({ type: "hello" });
    t.sockets[0].drop();
    await t.advance(1000);
    t.sockets[1].open();
    t.sockets[1].message({ type: "hello" });
    t.sockets[1].drop();
    await t.advance(1000); // 又是 1s，而不是 2s
    expect(t.sockets.length).toBe(3);
  });

  test("心跳：每 25s 发 ping；60s 没收到任何消息判死并重连", async () => {
    const t = setup();
    t.client.start();
    await t.flush();
    t.sockets[0].open();

    await t.advance(HEARTBEAT_INTERVAL);
    expect(t.sockets[0].sent).toEqual([JSON.stringify({ type: "ping" })]);

    // 收到 pong 会续命，不判死
    t.sockets[0].message({ type: "pong" });
    await t.advance(DEAD_AFTER - HEARTBEAT_TICK * 2);
    expect(t.client.getState()).toBe("connected");

    // 之后再也没有消息：到 60s 判死
    await t.advance(DEAD_AFTER);
    expect(t.sockets[0].closed).toBe(true);
    expect(t.client.getState()).toBe("reconnecting");
  });

  test("拿 ticket 失败也走退避重连；401 这类致命错误直接停", async () => {
    let attempts = 0;
    const flaky = setup({
      ticket: async () => {
        attempts += 1;
        if (attempts < 3) throw new Error("boom");
        return "ok";
      },
    });
    flaky.client.start();
    await flaky.flush();
    expect(flaky.client.getState()).toBe("reconnecting");
    await flaky.advance(1000);
    await flaky.advance(2000);
    expect(flaky.sockets.length).toBe(1);

    const fatal = setup({
      ticket: async () => {
        throw { status: 401 };
      },
      isFatal: (error) => (error as { status?: number }).status === 401,
    });
    fatal.client.start();
    await fatal.flush();
    expect(fatal.client.getState()).toBe("closed");
    expect(fatal.pendingCount()).toBe(0);
  });

  test("没登录（canConnect=false）不建连", async () => {
    const t = setup({ canConnect: () => false });
    t.client.start();
    await t.flush();
    expect(t.sockets.length).toBe(0);
    expect(t.client.getState()).toBe("closed");
  });

  test("reconnectNow：正在等退避时立刻重连；已连上则不动", async () => {
    const t = setup();
    t.client.start();
    await t.flush();
    t.sockets[0].open();
    t.client.reconnectNow();
    expect(t.sockets.length).toBe(1);

    t.sockets[0].drop();
    t.client.reconnectNow();
    await t.flush();
    expect(t.sockets.length).toBe(2);
  });

  test("stop：关闭连接、清掉所有定时器，晚到的 ticket 不会再建连", async () => {
    const t = setup();
    t.client.start();
    await t.flush();
    t.sockets[0].open();
    t.client.stop();
    expect(t.sockets[0].closed).toBe(true);
    expect(t.client.getState()).toBe("closed");
    expect(t.pendingCount()).toBe(0);
    await t.advance(120_000);
    expect(t.sockets.length).toBe(1);
  });
});
