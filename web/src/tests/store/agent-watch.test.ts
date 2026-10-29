import { beforeEach, describe, expect, mock, test } from "bun:test";

import type { AgentEventDto, AgentWatchBatchDto } from "@/api/agent/type";

const batch = (over: Partial<AgentWatchBatchDto> = {}): AgentWatchBatchDto => ({
  batch_id: "b1",
  state: "waiting",
  total: 1,
  settled: 0,
  tasks: [{ task_id: "101", node_id: "n1", label: "镜头 1", status: "pending" }],
  ...over,
});

let served: AgentWatchBatchDto[] = [];
let canceled: AgentWatchBatchDto[] = [];
const cancelCalls: string[] = [];
mock.module("@/api/agent", () => ({
  listAgentSessions: async () => [],
  getAgentHistory: async () => ({ events: [], has_more: false }),
  getAgentEvents: async () => [],
  setAgentFeedback: async () => ({ rating: 1, reasons: [], comment: "" }),
  deleteAgentFeedback: async () => {},
  getAgentWatches: async () => served,
  cancelAgentWatches: async (sid: string) => {
    cancelCalls.push(sid);
    return canceled;
  },
}));

const { useAgentStore, subscribeLiveAgentEvents } = await import("@/store/agent");

const ev = (seq: number, type: string, data: Record<string, unknown>): AgentEventDto => ({
  session_id: "s1",
  canvas_id: "c1",
  run_id: "r1",
  seq,
  type,
  data,
  created_at: "2026-10-28T12:00:00Z",
});

beforeEach(() => {
  useAgentStore.setState({ sessions: {}, sessionsByCanvas: {} });
  served = [];
  canceled = [];
  cancelCalls.length = 0;
});

describe("后台等待：初始状态与取消", () => {
  test("loadWatches：把服务端进行中的批并进会话；事件里更新的批不被旧快照覆盖", async () => {
    useAgentStore
      .getState()
      .handleEvent(ev(9, "watch.updated", batch({ batch_id: "b2", settled: 1, total: 1 })));
    served = [batch({ batch_id: "b1" }), batch({ batch_id: "b2", settled: 0 })];
    await useAgentStore.getState().loadWatches("s1");
    const { watches } = useAgentStore.getState().sessions.s1;
    expect(watches.b1.state).toBe("waiting");
    expect(watches.b2.settled).toBe(1);
  });

  test("cancelWatches：调接口，被取消的批标成 canceled", async () => {
    useAgentStore.getState().handleEvent(ev(1, "watch.updated", batch()));
    canceled = [batch({ state: "canceled" })];
    await useAgentStore.getState().cancelWatches("s1");
    expect(cancelCalls).toEqual(["s1"]);
    expect(useAgentStore.getState().sessions.s1.watches.b1.state).toBe("canceled");
  });
});

describe("实时事件：只有 WebSocket 推来的才通知监听者，回放不通知", () => {
  test("handleEvent(live) 通知；回放（不带 live）不通知", () => {
    const seen: string[] = [];
    const off = subscribeLiveAgentEvents((e) => seen.push(`${e.seq}:${e.type}`));
    useAgentStore
      .getState()
      .handleEvent(ev(1, "tasks.settled", { batch_ids: ["b1"], steer: false, tasks: [] }));
    useAgentStore
      .getState()
      .handleEvent(ev(2, "tasks.settled", { batch_ids: ["b2"], steer: false, tasks: [] }), {
        live: true,
      });
    off();
    useAgentStore
      .getState()
      .handleEvent(ev(3, "tasks.settled", { batch_ids: ["b3"], steer: false, tasks: [] }), {
        live: true,
      });
    expect(seen).toEqual(["2:tasks.settled"]);
  });

  test("重复到达的事件（实时推送和回放交错）只通知一次", () => {
    const seen: number[] = [];
    const off = subscribeLiveAgentEvents((e) => seen.push(e.seq));
    const e = ev(1, "tasks.settled", { batch_ids: ["b1"], steer: false, tasks: [] });
    useAgentStore.getState().handleEvent(e, { live: true });
    useAgentStore.getState().handleEvent(e, { live: true });
    off();
    expect(seen).toEqual([1]);
  });
});
