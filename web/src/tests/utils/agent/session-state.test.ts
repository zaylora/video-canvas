import { describe, expect, test } from "bun:test";

import type { AgentEventDto } from "@/api/agent/type";
import {
  activeRunId,
  applyAgentEvent,
  emptySession,
  hasGap,
  type AgentSessionState,
} from "@/utils/agent/session-state";

const ev = (
  seq: number,
  type: string,
  data: Record<string, unknown> = {},
  run: string | null = "r1",
): AgentEventDto => ({
  session_id: "s1",
  canvas_id: "c1",
  run_id: run,
  seq,
  type,
  data,
  created_at: "2026-10-07T12:00:00Z",
});

const fold = (events: AgentEventDto[], from: AgentSessionState = emptySession()) =>
  events.reduce(applyAgentEvent, from);

describe("持久化事件：去重、按序号排好、跟踪连续收齐的位置", () => {
  test("按序到达", () => {
    const s = fold([ev(1, "message.user"), ev(2, "run.status", { status: "running" })]);
    expect(s.events.map((e) => e.seq)).toEqual([1, 2]);
    expect([s.lastSeq, s.contiguousSeq]).toEqual([2, 2]);
    expect(hasGap(s)).toBe(false);
  });

  test("重复的事件被忽略，状态原样返回", () => {
    const s = fold([ev(1, "message.user")]);
    expect(applyAgentEvent(s, ev(1, "message.user"))).toBe(s);
  });

  test("漏了一条：标出缺口；补来之后缺口合上，事件按序排好", () => {
    let s = fold([ev(1, "message.user"), ev(3, "tool.start")]);
    expect([s.lastSeq, s.contiguousSeq, hasGap(s)]).toEqual([3, 1, true]);
    s = applyAgentEvent(s, ev(2, "run.status", { status: "running" }));
    expect(s.events.map((e) => e.seq)).toEqual([1, 2, 3]);
    expect([s.contiguousSeq, hasGap(s)]).toEqual([3, false]);
  });
});

describe("衍生状态", () => {
  test("run.status：记下每轮运行的状态和原因；进行中的那轮就是 activeRunId", () => {
    let s = fold([ev(1, "run.status", { status: "running" })]);
    expect(activeRunId(s)).toBe("r1");
    s = applyAgentEvent(s, ev(2, "run.status", { status: "failed", error: "模型不可用" }));
    expect(s.runs.r1).toEqual({ status: "failed", error: "模型不可用" });
    expect(activeRunId(s)).toBeNull();
  });

  test("等待批准和等待回答也算占着画布", () => {
    for (const status of ["queued", "waiting_approval", "waiting_input"]) {
      expect(activeRunId(fold([ev(1, "run.status", { status })]))).toBe("r1");
    }
    for (const status of [
      "interrupted",
      "step_limit",
      "budget_exhausted",
      "canceled",
      "succeeded",
    ]) {
      expect(activeRunId(fold([ev(1, "run.status", { status })]))).toBeNull();
    }
  });

  test("审批：创建和决定都按 id 覆盖成最新状态", () => {
    let s = fold([ev(1, "approval.created", { id: "a1", status: "pending", kind: "delete" })]);
    expect(s.approvals.a1.status).toBe("pending");
    s = applyAgentEvent(
      s,
      ev(2, "approval.decided", { id: "a1", status: "executed", kind: "delete" }),
    );
    expect(s.approvals.a1.status).toBe("executed");
  });

  test("计划：每轮运行保留最新一版", () => {
    const steps = [
      { title: "读剧本", status: "done" },
      { title: "建角色", status: "doing" },
    ];
    const s = fold([
      ev(1, "plan.updated", { steps: [{ title: "读剧本", status: "doing" }] }),
      ev(2, "plan.updated", { steps }),
    ]);
    expect(s.plans.r1).toEqual(steps);
  });

  test("run.usage：记下本轮已花和预算；之后的 run.status 不把它们冲掉", () => {
    let s = fold([
      ev(1, "run.status", { status: "running" }),
      ev(2, "run.usage", { spent_credits: 6, budget_credits: 50 }),
    ]);
    expect(s.runs.r1).toMatchObject({ status: "running", spent: 6, budget: 50 });
    s = applyAgentEvent(s, ev(3, "run.status", { status: "waiting_approval" }));
    expect(s.runs.r1).toMatchObject({ status: "waiting_approval", spent: 6, budget: 50 });
  });

  test("回放补来的旧事件不会把新状态倒回去", () => {
    let s = fold([ev(1, "message.user"), ev(3, "run.status", { status: "succeeded" })]);
    s = applyAgentEvent(s, ev(2, "run.status", { status: "running" }));
    expect(s.runs.r1.status).toBe("succeeded");
    expect(s.events.map((e) => e.seq)).toEqual([1, 2, 3]);
  });
});

describe("流式文字", () => {
  test("临时事件（seq 0）只累加文字，不进事件列表", () => {
    const s = fold([
      ev(0, "message.delta", { text: "你" }),
      ev(0, "message.delta", { text: "好" }),
      ev(0, "thinking.delta", { text: "想" }),
    ]);
    expect(s.stream).toEqual({ runId: "r1", text: "你好", thinking: "想" });
    expect(s.events).toHaveLength(0);
    expect(s.lastSeq).toBe(0);
  });

  test("message.done 带着完整内容落库，流式文字清掉", () => {
    const s = fold([
      ev(0, "message.delta", { text: "你好" }),
      ev(1, "message.done", { text: "你好，我来帮你" }),
    ]);
    expect(s.stream.text).toBe("");
    expect(s.events[0].type).toBe("message.done");
  });

  test("运行结束时残余的流式文字也清掉", () => {
    const s = fold([
      ev(0, "message.delta", { text: "写到一半" }),
      ev(1, "run.status", { status: "canceled" }),
    ]);
    expect(s.stream.text).toBe("");
  });

  test("不认识的临时事件忽略", () => {
    const s = emptySession();
    expect(applyAgentEvent(s, ev(0, "something.else"))).toBe(s);
  });
});
