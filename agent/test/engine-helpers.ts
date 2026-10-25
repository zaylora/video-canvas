import { MemoryStorage } from "@earendil-works/pi-durable";
import { AgentEngine, type PublishedEvent } from "../src/engine/engine.ts";
import { createIdCodec } from "../src/ids.ts";
import { createPlatform } from "../src/platform-client.ts";
import { openStore, type AgentStore } from "../src/store/store.ts";
import type { Run } from "../src/store/types.ts";
import { fakeGo, type FakeGoOptions } from "./fake-go.ts";

export const TOKEN = "service-token-0123456789abcdef0123456789";
export const codec = createIdCodec("test-key");

export interface Env {
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

export async function withEngine(go: FakeGoOptions, fn: (e: Env) => Promise<void>, opts: { approvalTtlMs?: number; canvasId?: string } = {}) {
  const g = await fakeGo(go);
  let clock = Date.now();
  const store = openStore(":memory:", { workerId: 1, now: () => clock });
  const events: PublishedEvent[] = [];
  const engine = await AgentEngine.open({
    store, platform: createPlatform({ baseUrl: g.url, serviceToken: TOKEN }), baseUrl: g.url, serviceToken: TOKEN,
    storage: new MemoryStorage(), codec, onEvent: (ev) => events.push(ev), now: () => clock, approvalTtlMs: opts.approvalTtlMs,
  });
  const session = store.createSession({ userId: "1", canvasId: opts.canvasId ?? "7", title: "新对话", mode: "all", modelKey: "" });
  const env: Env = {
    go: g, store, engine, events, sessionId: session.id, advanceClock: (ms) => { clock += ms; },
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
