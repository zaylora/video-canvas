import { afterEach, beforeEach, describe, expect, test } from "bun:test";

import type { AgentEventDto, AgentSessionDto } from "@/api/agent/type";
import { ADMIN_SESSION_SCOPE, useAgentStore } from "@/store/agent";
import { ADMIN_CONFIG_CHANGED } from "@/utils/admin/config-changed";

const decided = (seq: number, kind = "admin_change"): AgentEventDto => ({
  session_id: "as1",
  canvas_id: "0",
  run_id: "r1",
  seq,
  type: "approval.decided",
  data: {
    id: "ap1",
    run_id: "r1",
    kind,
    status: "executed",
    payload: {
      order_no: "CO-1",
      title: "t",
      reason: "r",
      items: [{ target: "pricing_settings", key: "pricing" }],
    },
    quote_credits: 0,
    decision: null,
    result: {
      items: [{ index: 0, status: "applied" }],
      applied_by: "zhang",
      applied_at: "2026-11-02T06:00:00Z",
      audit_ids: [3],
    },
    expires_at: "",
  },
  created_at: "2026-11-02T06:00:00Z",
});

const session = (id: string, title: string): AgentSessionDto => ({
  id,
  canvas_id: "0",
  title,
  mode: "admin",
  model_key: "m",
  last_seq: 0,
  created_at: "2026-11-02T06:00:00Z",
  updated_at: "2026-11-02T06:00:00Z",
});

const g = globalThis as unknown as { window?: EventTarget };
let saved: EventTarget | undefined;
let fired: unknown[];

beforeEach(() => {
  saved = g.window;
  g.window = new EventTarget();
  fired = [];
  g.window.addEventListener(ADMIN_CONFIG_CHANGED, (e) => fired.push((e as CustomEvent).detail));
  useAgentStore.setState({ sessions: {}, sessionsByCanvas: {} });
});
afterEach(() => {
  g.window = saved;
});

describe("后台助手：变更单应用后通知页面刷新", () => {
  test("实时收到已应用的 admin_change：派发 admin-config-changed", () => {
    useAgentStore.getState().handleEvent(decided(5), { live: true });
    expect(fired).toEqual([{ targets: ["pricing_settings"] }]);
  });

  test("回放补来的旧事件不派发（页面刷新时不该再刷一遍）", () => {
    useAgentStore.getState().handleEvent(decided(5));
    expect(fired).toEqual([]);
  });

  test("同一条事件重复到达只派发一次", () => {
    const store = useAgentStore.getState();
    store.handleEvent(decided(5), { live: true });
    store.handleEvent(decided(5), { live: true });
    expect(fired).toHaveLength(1);
  });

  test("其他审批种类的决定不派发，画布 Agent 不受影响", () => {
    useAgentStore.getState().handleEvent(decided(5, "generate"), { live: true });
    expect(fired).toEqual([]);
  });

  test("审批照常进会话状态，带上 result", () => {
    useAgentStore.getState().handleEvent(decided(5), { live: true });
    const approval = useAgentStore.getState().sessions["as1"].approvals["ap1"];
    expect(approval.status).toBe("executed");
    expect(approval.result).toMatchObject({ applied_by: "zhang" });
  });
});

describe("后台助手：会话列表放在固定作用域下", () => {
  test("作用域 key 是 0，新建、改名都落在这里", () => {
    expect(ADMIN_SESSION_SCOPE).toBe("0");
    const store = useAgentStore.getState();
    store.upsertSession(session("a", "新会话"));
    store.upsertSession(session("b", "调价"));
    store.upsertSession(session("a", "改名了"));
    expect(
      useAgentStore.getState().sessionsByCanvas[ADMIN_SESSION_SCOPE].map((s) => [s.id, s.title]),
    ).toEqual([
      ["a", "改名了"],
      ["b", "调价"],
    ]);
  });

  test("会话标题事件改的是这份列表", () => {
    const store = useAgentStore.getState();
    store.upsertSession(session("as1", "新会话"));
    store.handleEvent({
      session_id: "as1",
      canvas_id: "0",
      run_id: "r1",
      seq: 1,
      type: "session.titled",
      data: { title: "视频模型调价" },
      created_at: "2026-11-02T06:00:00Z",
    });
    expect(useAgentStore.getState().sessionsByCanvas["0"][0].title).toBe("视频模型调价");
  });

  test("Go 推送的后台会话事件 canvas_id 是 null（零画布按 idcodec 编码）：照样改到后台列表", () => {
    const store = useAgentStore.getState();
    store.upsertSession(session("as2", "新会话"));
    store.handleEvent({
      session_id: "as2",
      canvas_id: null as unknown as string,
      run_id: "r2",
      seq: 1,
      type: "session.titled",
      data: { title: "默认加价率调整" },
      created_at: "2026-11-02T06:00:00Z",
    });
    expect(
      useAgentStore.getState().sessionsByCanvas[ADMIN_SESSION_SCOPE].find((s) => s.id === "as2")
        ?.title,
    ).toBe("默认加价率调整");
  });

  test("删除会话只影响这份列表", () => {
    const store = useAgentStore.getState();
    store.upsertSession(session("a", "x"));
    store.removeSession(ADMIN_SESSION_SCOPE, "a");
    expect(useAgentStore.getState().sessionsByCanvas["0"]).toEqual([]);
  });
});
