import { describe, expect, test } from "bun:test";

import type { AgentEventDto } from "@/api/agent/type";
import {
  activeRunId,
  applyAgentEvent,
  applyHistoryPage,
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

  test("run.usage：记下本轮已花，和会话当前的上下文用量；之后的 run.status 不把它们冲掉", () => {
    let s = fold([
      ev(1, "run.status", { status: "running" }),
      ev(2, "run.usage", { spent_credits: 6, context_tokens: 12_000 }),
    ]);
    expect(s.runs.r1).toMatchObject({ status: "running", spent: 6 });
    expect(s.contextTokens).toBe(12_000);
    s = applyAgentEvent(s, ev(3, "run.status", { status: "waiting_approval" }));
    expect(s.runs.r1).toMatchObject({ status: "waiting_approval", spent: 6 });
    expect(s.contextTokens).toBe(12_000);
  });

  test("上下文用量取最近一次：压缩之后变小，界面跟着降下来", () => {
    const s = fold([
      ev(1, "run.status", { status: "running" }),
      ev(2, "run.usage", { spent_credits: 1, context_tokens: 180_000 }),
      ev(3, "run.usage", { spent_credits: 2, context_tokens: 40_000 }),
    ]);
    expect(s.contextTokens).toBe(40_000);
  });

  test("旧事件没带 context_tokens：不改上下文用量；回放补来的更旧事件也不把它倒回去", () => {
    let s = fold([
      ev(1, "run.status", { status: "running" }),
      ev(3, "run.usage", { spent_credits: 2, context_tokens: 40_000 }),
    ]);
    s = applyAgentEvent(s, ev(4, "run.usage", { spent_credits: 3 }));
    expect(s.contextTokens).toBe(40_000);
    s = applyAgentEvent(s, ev(2, "run.usage", { spent_credits: 1, context_tokens: 999 }));
    expect(s.contextTokens).toBe(40_000);
  });

  test("新会话还没有用量：上下文是 0", () => {
    expect(fold([ev(1, "run.status", { status: "running" })]).contextTokens).toBe(0);
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

describe("聊天记录分页：从最新往回加载", () => {
  const turn = (from: number, run: string, status = "succeeded") => [
    ev(from, "message.user", { text: run }, run),
    ev(from + 1, "run.status", { status: "queued" }, run),
    ev(from + 2, "run.usage", { spent_credits: 3, context_tokens: from * 100 }, run),
    ev(from + 3, "run.status", { status }, run),
  ];
  const seqs = (s: AgentSessionState) => s.events.map((e) => e.seq);

  test("第一页：连续区间从这一页的第一条算起，不要求从 1 开始；记下还有没有更早的", () => {
    const s = applyHistoryPage(emptySession(), [...turn(11, "r3")], true);
    expect(seqs(s)).toEqual([11, 12, 13, 14]);
    expect([s.baseSeq, s.contiguousSeq, s.lastSeq]).toEqual([10, 14, 14]);
    expect([s.historyReady, s.hasMoreOlder, hasGap(s)]).toEqual([true, true, false]);
    expect(s.runs.r3.status).toBe("succeeded");
  });

  test("页到达之前实时推送已经来了几条：并进去，连续位置接着算", () => {
    let s = applyAgentEvent(emptySession(), ev(15, "run.status", { status: "queued" }, "r4"));
    expect(s.contiguousSeq).toBe(0); // 前面还没加载，接不上
    s = applyHistoryPage(s, turn(11, "r3"), false);
    expect(seqs(s)).toEqual([11, 12, 13, 14, 15]);
    expect(s.contiguousSeq).toBe(15);
    expect(s.hasMoreOlder).toBe(false);
  });

  test("加载之后实时推送照常接在后面", () => {
    let s = applyHistoryPage(emptySession(), turn(11, "r3"), true);
    s = applyAgentEvent(s, ev(15, "message.user", { text: "新" }, "r4"));
    expect([s.contiguousSeq, hasGap(s)]).toEqual([15, false]);
  });

  test("往上翻：更早的一页接在前面，连续位置不变，起点前移；更早的状态补进来，新的不被倒回去", () => {
    // r1 的 queued 在旧页，succeeded 在新页（页对齐到一轮开头，但保险起见新旧同键时新的为准）
    let s = applyHistoryPage(
      emptySession(),
      [ev(21, "run.status", { status: "succeeded" }, "r1"), ...turn(22, "r3")],
      true,
    );
    s = applyHistoryPage(
      s,
      [
        ev(19, "run.status", { status: "queued" }, "r1"),
        ev(20, "run.status", { status: "running" }, "r1"),
      ],
      true,
    );
    expect(seqs(s).slice(0, 4)).toEqual([19, 20, 21, 22]);
    expect([s.baseSeq, s.contiguousSeq]).toEqual([18, 25]);
    expect(s.runs.r1.status).toBe("succeeded");
    expect(s.runs.r3.status).toBe("succeeded");
  });

  test("更早一页里的运行状态、计划、审批都折进来；没有更早的了就清掉标记", () => {
    let s = applyHistoryPage(emptySession(), turn(11, "r3"), true);
    s = applyHistoryPage(
      s,
      [
        ev(5, "run.status", { status: "failed", error: "炸了" }, "r2"),
        ev(6, "plan.updated", { steps: [{ title: "a", status: "done" }] }, "r2"),
        ev(7, "approval.created", { id: "ap1", status: "pending" }, "r2"),
      ],
      false,
    );
    expect(s.runs.r2).toMatchObject({ status: "failed", error: "炸了" });
    expect(s.plans.r2).toEqual([{ title: "a", status: "done" }]);
    expect(s.approvals.ap1).toMatchObject({ id: "ap1" });
    expect(s.hasMoreOlder).toBe(false);
  });

  test("旧页里的评价已在新页里被取消：不会复活", () => {
    let s = applyHistoryPage(
      emptySession(),
      [ev(30, "feedback.cleared", { run_id: "r1" }, "r1"), ...turn(31, "r3")],
      true,
    );
    s = applyHistoryPage(s, [ev(10, "feedback.set", { run_id: "r1", rating: -1 }, "r1")], true);
    expect(s.feedback.r1).toBeUndefined();
  });

  test("上下文用量取最新的那条：旧页不覆盖", () => {
    let s = applyHistoryPage(emptySession(), turn(31, "r3"), true);
    const latest = s.contextTokens;
    s = applyHistoryPage(s, turn(11, "r2"), true);
    expect(s.contextTokens).toBe(latest);
  });

  test("空页：也算加载完成", () => {
    const s = applyHistoryPage(emptySession(), [], false);
    expect([s.historyReady, s.hasMoreOlder, s.events.length]).toEqual([true, false, 0]);
  });

  test("流式文字和已知的运行花费不被重新折叠冲掉", () => {
    let s = applyAgentEvent(emptySession(), ev(0, "message.delta", { text: "你好" }, "r4"));
    s = { ...s, runs: { r4: { status: "running", error: "", spent: 9 } } };
    s = applyHistoryPage(s, [ev(11, "run.status", { status: "running" }, "r4")], false);
    expect(s.stream.text).toBe("你好");
    expect(s.runs.r4.spent).toBe(9);
  });
});

describe("压缩中状态", () => {
  const ev = (type: string, data: Record<string, unknown>, seq: number, run = "r1") => ({
    session_id: "s1",
    canvas_id: "c1",
    run_id: run,
    seq,
    type,
    data,
    created_at: "",
  });
  const running = [ev("run.status", { status: "running" }, 1)];
  const fold = (events: ReturnType<typeof ev>[]) => events.reduce(applyAgentEvent, emptySession());

  test("收到临时事件 context.compacting 置位；context.compacted 清除", () => {
    const compacting = applyAgentEvent(fold(running), ev("context.compacting", {}, 0));
    expect(compacting.compacting).toBe("r1");
    expect(compacting.events).toHaveLength(1); // 临时事件不进事件列表
    expect(
      applyAgentEvent(compacting, ev("context.compacted", { before: 1 }, 2)).compacting,
    ).toBeNull();
  });

  test("运行结束（停止、失败）也清除，不会一直显示「正在整理」", () => {
    const compacting = applyAgentEvent(fold(running), ev("context.compacting", {}, 0));
    expect(
      applyAgentEvent(compacting, ev("run.status", { status: "canceled" }, 2)).compacting,
    ).toBeNull();
  });

  test("加载历史时：运行已经结束的残留标记被清掉", () => {
    const compacting = applyAgentEvent(fold(running), ev("context.compacting", {}, 0));
    const page = applyHistoryPage(
      compacting,
      [...running, ev("run.status", { status: "failed" }, 2)],
      false,
    );
    expect(page.compacting).toBeNull();
  });
});
