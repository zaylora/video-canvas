import { MemoryStorage, type SetTimer } from "@earendil-works/pi-durable";
import { AgentEngine, type PublishedEvent } from "../src/engine/engine.ts";
import { createIdCodec } from "../src/ids.ts";
import { createPlatform } from "../src/platform-client.ts";
import { openStore, type AgentStore } from "../src/store/store.ts";
import type { Run } from "../src/store/types.ts";
import { fakeGo, type FakeGoOptions } from "./fake-go.ts";

export const TOKEN = "service-token-0123456789abcdef0123456789";
export const codec = createIdCodec("test-key");

export interface Env {
  platform: ReturnType<typeof createPlatform>;
  /** 把引擎的时钟往后拨（毫秒） */
  advanceClock(ms: number): void;
  go: Awaited<ReturnType<typeof fakeGo>>;
  store: AgentStore;
  engine: AgentEngine;
  events: PublishedEvent[];
  sessionId: string;
  start(over?: Partial<Parameters<AgentEngine["startRun"]>[0]>): Promise<Run>;
  types(): string[];
  persisted(type: string): Array<Record<string, any>>;
  statuses(): string[];
  run(id: string): Run;
}

export async function withEngine(go: FakeGoOptions, fn: (e: Env) => Promise<void>, opts: { approvalTtlMs?: number; canvasId?: string; autoTitle?: boolean; activitySummary?: boolean; piClock?: boolean; watchGraceMs?: number; watchPollMs?: number } = {}) {
  const g = await fakeGo(go);
  let skew = 0; // advanceClock 往后拨的量；平时用真实时间，时间戳才会随时间前进
  const clock = () => Date.now() + skew;
  const store = openStore(":memory:", { workerId: 1, now: clock });
  const events: PublishedEvent[] = [];
  // piClock：Pi 的时钟跟着 advanceClock 走，「睡一会儿」（10 秒以上）的定时器由它触发；短的定时器（进度提交、让出）照常用真实时间
  const sleepTimers: Array<{ at: number; cb: () => void; off: boolean }> = [];
  const setTimer: SetTimer = (cb, ms) => {
    if (ms >= 10_000) {
      const t = { at: clock() + ms, cb, off: false };
      sleepTimers.push(t);
      return () => { t.off = true; };
    }
    const h = setTimeout(cb, ms);
    return () => clearTimeout(h);
  };
  const platform = createPlatform({ baseUrl: g.url, serviceToken: TOKEN });
  const engine = await AgentEngine.open({
    store, platform, baseUrl: g.url, serviceToken: TOKEN,
    storage: new MemoryStorage(), codec, onEvent: (ev) => events.push(ev), now: clock, approvalTtlMs: opts.approvalTtlMs,
    ...(opts.piClock && { harness: { now: clock, setTimer } }), watchGraceMs: opts.watchGraceMs, watchPollMs: opts.watchPollMs,
    autoTitle: opts.autoTitle ?? false, // 默认不命名：命名多一次模型调用，会改变别的用例数的调用次数和用量；要测命名的用例自己打开
    activitySummary: opts.activitySummary ?? false, // 同理：活动说明也是额外的模型调用，要测的用例自己打开
  });
  const session = store.createSession({ userId: "1", canvasId: opts.canvasId ?? "7", title: "新对话", mode: "all", modelKey: "" });
  const env: Env = {
    platform, go: g, store, engine, events, sessionId: session.id, advanceClock: (ms) => {
      skew += ms;
      for (const t of sleepTimers.filter((x) => !x.off && x.at <= clock())) { t.off = true; t.cb(); }
    },
    start: (over = {}) => engine.startRun({ userId: "1", sessionId: session.id, message: "拆分镜", modelKey: "claude", ...over }),
    types: () => store.listEvents(session.id, 0, 1000).map((x) => x.type),
    persisted: (type) => store.listEvents(session.id, 0, 1000).filter((x) => x.type === type).map((x) => x.payload as Record<string, any>),
    statuses: () => store.listEvents(session.id, 0, 1000).filter((x) => x.type === "run.status").map((x) => (x.payload as { status: string }).status),
    run: (id) => store.getRun("1", id),
  };
  try {
    await fn(env);
  } finally {
    await engine.close();
    store.close();
    await g.close();
  }
}
