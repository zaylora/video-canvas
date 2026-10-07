import { beforeEach, describe, expect, mock, test } from "bun:test";

import type { AgentEventDto } from "@/api/agent/type";

const ev = (
  seq: number,
  type = "run.status",
  data: Record<string, unknown> = { status: "running" },
): AgentEventDto => ({
  session_id: "s1",
  canvas_id: "c1",
  run_id: "r1",
  seq,
  type,
  data,
  created_at: "2026-10-07T12:00:00Z",
});

/** 服务端有 1..total 条事件；按 after 分页，每页最多 pageSize 条 */
let total = 0;
let pageSize = 200;
const calls: number[] = [];
mock.module("@/api/agent", () => ({
  listAgentSessions: async () => [],
  getAgentEvents: async (_sid: string, after: number) => {
    calls.push(after);
    const out: AgentEventDto[] = [];
    for (let seq = after + 1; seq <= total && out.length < pageSize; seq++) out.push(ev(seq));
    return out;
  },
}));

const { useAgentStore } = await import("@/store/agent");

beforeEach(() => {
  useAgentStore.setState({ sessions: {}, sessionsByCanvas: {} });
  calls.length = 0;
  pageSize = 200;
});

describe("agent store：回放", () => {
  test("从连续收齐的序号之后分页拉到没有为止", async () => {
    total = 5;
    pageSize = 2;
    await useAgentStore.getState().replay("s1");
    expect(calls).toEqual([0, 2, 4, 5]);
    expect(useAgentStore.getState().sessions.s1.contiguousSeq).toBe(5);
  });

  test("实时推送漏了一条：回放只补缺口，不重复拉已有的", async () => {
    total = 4;
    useAgentStore.getState().handleEvent(ev(1));
    useAgentStore.getState().handleEvent(ev(3)); // 2 丢了
    await useAgentStore.getState().replay("s1");
    expect(calls[0]).toBe(1);
    const s = useAgentStore.getState().sessions.s1;
    expect(s.events.map((e) => e.seq)).toEqual([1, 2, 3, 4]);
    expect(s.contiguousSeq).toBe(4);
  });

  test("服务端给的都是已有的：不死循环", async () => {
    total = 0;
    useAgentStore.getState().handleEvent(ev(1));
    total = 1;
    await useAgentStore.getState().replay("s1");
    expect(calls.length).toBeLessThanOrEqual(2);
  });
});

describe("agent store：事件与会话列表", () => {
  test("handleEvent 为没见过的会话建立状态；重复事件不改变引用", () => {
    useAgentStore.getState().handleEvent(ev(1));
    const first = useAgentStore.getState().sessions;
    useAgentStore.getState().handleEvent(ev(1));
    expect(useAgentStore.getState().sessions).toBe(first);
  });

  test("upsertSession 放到最前；removeSession 连状态一起清掉", () => {
    const session = (id: string) => ({
      id,
      canvas_id: "c1",
      title: id,
      mode: "all" as const,
      model_key: "m",
      last_seq: 0,
      created_at: "",
      updated_at: "",
    });
    useAgentStore.getState().upsertSession(session("a"));
    useAgentStore.getState().upsertSession(session("b"));
    useAgentStore.getState().upsertSession(session("a"));
    expect(useAgentStore.getState().sessionsByCanvas.c1.map((s) => s.id)).toEqual(["a", "b"]);
    useAgentStore.getState().handleEvent({ ...ev(1), session_id: "a" });
    useAgentStore.getState().removeSession("c1", "a");
    expect(useAgentStore.getState().sessionsByCanvas.c1.map((s) => s.id)).toEqual(["b"]);
    expect(useAgentStore.getState().sessions.a).toBeUndefined();
  });

  test("ingestRun 把 HTTP 返回的运行状态并进会话", () => {
    useAgentStore.getState().ingestRun({
      id: "r9",
      session_id: "s1",
      canvas_id: "c1",
      status: "running",
      mode: "all",
      budget_credits: 50,
      spent_credits: 0,
      steps: 0,
      max_steps: 40,
      error_code: "",
      error_message: "",
      created_at: "",
      ended_at: null,
    });
    expect(useAgentStore.getState().sessions.s1.runs.r9.status).toBe("running");
  });
});
