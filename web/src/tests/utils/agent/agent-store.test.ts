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
/** 聊天记录接口：每页最多 historyPage 条，before 之前最新的一页；failNext 让下一次请求失败；gate 让请求停住等放行 */
let historyPage = 3;
const historyCalls: number[] = [];
let failNext = false;
let gate: Promise<void> | null = null;
mock.module("@/api/agent", () => ({
  listAgentSessions: async () => [],
  getAgentHistory: async (_sid: string, before: number) => {
    historyCalls.push(before);
    if (gate) await gate;
    if (failNext) {
      failNext = false;
      throw new Error("boom");
    }
    const end = before > 0 ? before - 1 : total;
    const start = Math.max(1, end - historyPage + 1);
    const events: AgentEventDto[] = [];
    for (let seq = start; seq <= end; seq++) events.push(ev(seq));
    return { events, has_more: start > 1 };
  },
  getAgentEvents: async (_sid: string, after: number) => {
    calls.push(after);
    const out: AgentEventDto[] = [];
    for (let seq = after + 1; seq <= total && out.length < pageSize; seq++) out.push(ev(seq));
    return out;
  },
  setAgentFeedback: async () => ({ rating: 1, reasons: [], comment: "" }),
  deleteAgentFeedback: async () => {},
}));

const { useAgentStore } = await import("@/store/agent");

beforeEach(() => {
  useAgentStore.setState({ sessions: {}, sessionsByCanvas: {} });
  calls.length = 0;
  historyCalls.length = 0;
  pageSize = 200;
  historyPage = 3;
  failNext = false;
  gate = null;
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

describe("agent store：聊天记录从最新往回加载", () => {
  const session = () => useAgentStore.getState().sessions.s1;

  test("打开会话：只取最新一页；已经加载过就不再请求", async () => {
    total = 10;
    await useAgentStore.getState().loadLatest("s1");
    expect(historyCalls).toEqual([0]);
    expect(session().events.map((e) => e.seq)).toEqual([8, 9, 10]);
    expect([session().historyReady, session().hasMoreOlder, session().loading]).toEqual([
      true,
      true,
      null,
    ]);
    await useAgentStore.getState().loadLatest("s1");
    expect(historyCalls).toEqual([0]);
  });

  test("请求期间标记为正在加载最新一页，重复打开不重复请求", async () => {
    total = 10;
    let release!: () => void;
    gate = new Promise<void>((r) => (release = r));
    const first = useAgentStore.getState().loadLatest("s1");
    useAgentStore.getState().loadLatest("s1");
    expect(session().loading).toBe("latest");
    release();
    await first;
    expect(historyCalls).toEqual([0]);
    expect(session().loading).toBeNull();
  });

  test("往上翻：before 取已有的最小序号，加载到头后不再请求", async () => {
    total = 7;
    await useAgentStore.getState().loadLatest("s1"); // 5..7
    await useAgentStore.getState().loadOlder("s1"); // 2..4
    expect(historyCalls).toEqual([0, 5]);
    expect(session().events.map((e) => e.seq)).toEqual([2, 3, 4, 5, 6, 7]);
    expect(session().hasMoreOlder).toBe(true);
    await useAgentStore.getState().loadOlder("s1"); // 1
    expect(session().events[0].seq).toBe(1);
    expect(session().hasMoreOlder).toBe(false);
    await useAgentStore.getState().loadOlder("s1");
    expect(historyCalls).toEqual([0, 5, 2]);
  });

  test("往上翻正在进行时，再次触发不重复请求", async () => {
    total = 10;
    await useAgentStore.getState().loadLatest("s1");
    let release!: () => void;
    gate = new Promise<void>((r) => (release = r));
    const a = useAgentStore.getState().loadOlder("s1");
    void useAgentStore.getState().loadOlder("s1");
    expect(session().loading).toBe("older");
    release();
    await a;
    expect(historyCalls).toEqual([0, 8]);
  });

  test("还没加载最新一页时不能往上翻", async () => {
    total = 10;
    await useAgentStore.getState().loadOlder("s1");
    expect(historyCalls).toEqual([]);
  });

  test("加载失败：留下失败标记，不抛出；再次加载会重试", async () => {
    total = 10;
    failNext = true;
    await useAgentStore.getState().loadLatest("s1");
    expect([session().loadError, session().loading, session().historyReady]).toEqual([
      true,
      null,
      false,
    ]);
    await useAgentStore.getState().loadLatest("s1");
    expect([session().loadError, session().historyReady]).toEqual([false, true]);

    failNext = true;
    await useAgentStore.getState().loadOlder("s1");
    expect([session().loadError, session().hasMoreOlder]).toEqual([true, true]);
    await useAgentStore.getState().loadOlder("s1");
    expect(session().loadError).toBe(false);
    expect(session().events[0].seq).toBe(5);
  });

  test("断线重连对账 sync：加载过的补尾巴，没加载过的取最新一页（不从头拉）", async () => {
    total = 10;
    await useAgentStore.getState().sync("s1");
    expect(historyCalls).toEqual([0]);
    expect(calls).toEqual([]);
    total = 12; // 断线期间服务端又有了 11、12
    await useAgentStore.getState().sync("s1");
    expect(historyCalls).toEqual([0]);
    expect(calls).toEqual([10, 12]);
    expect(session().events.map((e) => e.seq)).toEqual([8, 9, 10, 11, 12]);
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
      spent_credits: 0,
      context_tokens: 35_000,
      steps: 0,
      max_steps: 40,
      error_code: "",
      error_message: "",
      created_at: "",
      ended_at: null,
    });
    const session = useAgentStore.getState().sessions.s1;
    expect(session.runs.r9).toMatchObject({ status: "running", spent: 0 });
    expect(session.contextTokens).toBe(35_000);
  });

  test("ingestRun：新一轮刚发起时上下文用量是 0，不能把会话已有的用量冲成 0", () => {
    const run = {
      id: "r10",
      session_id: "s1",
      canvas_id: "c1",
      status: "queued" as const,
      mode: "all" as const,
      spent_credits: 0,
      context_tokens: 0,
      steps: 0,
      max_steps: 40,
      error_code: "",
      error_message: "",
      created_at: "",
      ended_at: null,
    };
    useAgentStore
      .getState()
      .ingestRun({ ...run, id: "r9", status: "running", context_tokens: 35_000 });
    useAgentStore.getState().ingestRun(run);
    expect(useAgentStore.getState().sessions.s1.contextTokens).toBe(35_000);
  });
});
