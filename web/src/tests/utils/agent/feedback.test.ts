import { afterEach, beforeEach, describe, expect, mock, spyOn, test } from "bun:test";
import { toast } from "sonner";

import type { AgentEventDto, AgentFeedbackDto, SetAgentFeedbackReq } from "@/api/agent/type";
import {
  FEEDBACK_SAVE_FAILED,
  feedbackNote,
  normalizeFeedback,
  thumbAction,
} from "@/utils/agent/feedback";
import { applyAgentEvent, emptySession } from "@/utils/agent/session-state";
import { buildTimeline } from "@/utils/agent/timeline";

let seq = 0;
const ev = (
  type: string,
  data: Record<string, unknown> = {},
  run: string | null = "r1",
  s = ++seq,
): AgentEventDto => ({
  session_id: "s1",
  canvas_id: "c1",
  run_id: run,
  seq: s,
  type,
  data,
  created_at: "2026-10-21T12:00:00Z",
});
const fold = (events: AgentEventDto[]) => events.reduce(applyAgentEvent, emptySession());
const reset = () => (seq = 0);

describe("反馈事件进入会话状态", () => {
  test("feedback.set 记下这一轮的评价、原因和说明", () => {
    reset();
    const s = fold([
      ev("feedback.set", {
        run_id: "r1",
        rating: -1,
        reasons: ["misread", "slow"],
        comment: "太慢了",
      }),
    ]);
    expect(s.feedback.r1).toEqual({ rating: -1, reasons: ["misread", "slow"], comment: "太慢了" });
  });

  test("feedback.cleared 去掉这一轮的评价，别的轮不受影响", () => {
    reset();
    const s = fold([
      ev("feedback.set", { run_id: "r1", rating: 1, reasons: [], comment: "" }),
      ev("feedback.set", { run_id: "r2", rating: 1, reasons: [], comment: "" }, "r2"),
      ev("feedback.cleared", { run_id: "r1" }),
    ]);
    expect(s.feedback.r1).toBeUndefined();
    expect(s.feedback.r2?.rating).toBe(1);
  });

  test("run_id 取事件 data 里的，data 没有时取事件自己的", () => {
    reset();
    const s = fold([ev("feedback.set", { rating: 1 }, "r9")]);
    expect(s.feedback.r9?.rating).toBe(1);
  });

  test("同一条事件重复到达是幂等的：状态引用都不变", () => {
    reset();
    const e = ev("feedback.set", { run_id: "r1", rating: 1, reasons: [], comment: "" });
    const once = applyAgentEvent(emptySession(), e);
    expect(applyAgentEvent(once, e)).toBe(once);
  });

  test("相同内容的新事件再次到达，结果和一次相同", () => {
    reset();
    const data = { run_id: "r1", rating: -1, reasons: ["other"], comment: "x" };
    const s = fold([ev("feedback.set", data), ev("feedback.set", data)]);
    expect(s.feedback.r1).toEqual({ rating: -1, reasons: ["other"], comment: "x" });
  });

  test("刷新后回放：按序号重放得到最后一次的结果（改选、取消、再评价）", () => {
    reset();
    const s = fold([
      ev("feedback.set", { run_id: "r1", rating: 1 }),
      ev("feedback.cleared", { run_id: "r1" }),
      ev("feedback.set", { run_id: "r1", rating: -1, reasons: ["wrong_edit"], comment: "" }),
    ]);
    expect(s.feedback.r1).toEqual({ rating: -1, reasons: ["wrong_edit"], comment: "" });
  });

  test("回放补来的旧事件不会盖掉新状态（取消在先到、评价在后到）", () => {
    reset();
    const s = fold([
      ev("feedback.cleared", { run_id: "r1" }, "r1", 6),
      ev("feedback.set", { run_id: "r1", rating: 1 }, "r1", 5),
    ]);
    expect(s.feedback.r1).toBeUndefined();
    expect(s.events.map((e) => e.seq)).toEqual([5, 6]);
  });

  test("无效的评价值、不认识的原因：无效评价忽略，不认识的原因丢掉", () => {
    reset();
    const s = fold([
      ev("feedback.set", { run_id: "r1", rating: 5 }),
      ev("feedback.set", { run_id: "r2", rating: -1, reasons: ["slow", "nope", "slow"] }, "r2"),
    ]);
    expect(s.feedback.r1).toBeUndefined();
    expect(s.feedback.r2?.reasons).toEqual(["slow"]);
  });
});

describe("normalizeFeedback", () => {
  test("缺省的原因补成空数组，缺省的说明补成空串", () => {
    expect(normalizeFeedback({ rating: 1 })).toEqual({ rating: 1, reasons: [], comment: "" });
    expect(normalizeFeedback({ rating: -1, reasons: null, comment: null })).toEqual({
      rating: -1,
      reasons: [],
      comment: "",
    });
  });

  test("不是对象或评价无效返回 null", () => {
    expect(normalizeFeedback(null)).toBeNull();
    expect(normalizeFeedback({ rating: 0 })).toBeNull();
  });
});

describe("点击👍/👎的语义", () => {
  const up: AgentFeedbackDto = { rating: 1, reasons: [], comment: "" };
  const down: AgentFeedbackDto = { rating: -1, reasons: [], comment: "" };

  test("没评价：👍 直接提交，👎 展开原因", () => {
    expect(thumbAction(undefined, 1)).toBe("submit-up");
    expect(thumbAction(undefined, -1)).toBe("toggle-form");
  });

  test("再点已选中的：取消", () => {
    expect(thumbAction(up, 1)).toBe("cancel");
    expect(thumbAction(down, -1)).toBe("cancel");
  });

  test("改选：点赞改点踩要展开原因，点踩改点赞直接提交", () => {
    expect(thumbAction(up, -1)).toBe("toggle-form");
    expect(thumbAction(down, 1)).toBe("submit-up");
  });
});

describe("评价后的小字", () => {
  test("点赞是感谢", () => {
    expect(feedbackNote({ rating: 1, reasons: [], comment: "" })).toBe("感谢反馈");
  });

  test("点踩列出原因；没选原因写未选原因；不认识的原因不显示", () => {
    expect(feedbackNote({ rating: -1, reasons: ["misread", "slow"], comment: "" })).toBe(
      "已记录：没理解我的需求、步骤太多或太慢",
    );
    expect(feedbackNote({ rating: -1, reasons: [], comment: "有话说" })).toBe("已记录：未选原因");
  });
});

describe("每轮运行末尾的反馈位", () => {
  test("运行中不显示；结束后出现在这一轮的最后", () => {
    reset();
    const running = buildTimeline(
      fold([ev("message.user", { text: "拆分镜" }), ev("run.status", { status: "running" })]),
    );
    expect(running.map((i) => i.type)).toEqual(["user"]);
    // 等用户批准、等用户回答也还没结束
    for (const status of ["queued", "waiting_approval", "waiting_input"]) {
      const waiting = buildTimeline(
        fold([ev("message.user", { text: "拆分镜" }), ev("run.status", { status })]),
      );
      expect(waiting.some((i) => i.type === "run-feedback")).toBe(false);
    }
    const ended = buildTimeline(
      fold([
        ev("message.user", { text: "拆分镜" }),
        ev("run.status", { status: "running" }),
        ev("message.done", { text: "好了" }),
        ev("run.status", { status: "succeeded" }),
      ]),
    );
    expect(ended.map((i) => i.type)).toEqual(["user", "assistant", "run-feedback"]);
    expect(ended.at(-1)).toMatchObject({ type: "run-feedback", runId: "r1" });
  });

  test("放在改动摘要和失败卡片之后", () => {
    reset();
    const items = buildTimeline(
      fold([
        ev("message.user", { text: "改" }),
        ev("run.status", { status: "running" }),
        ev("tool.start", { id: "t", name: "canvas_apply_ops" }),
        ev("tool.end", { id: "t", name: "canvas_apply_ops", created: ["n1"] }),
        ev("run.status", { status: "failed", error: "炸了" }),
      ]),
    );
    expect(items.map((i) => i.type)).toEqual([
      "user",
      "activity",
      "status",
      "run-summary",
      "run-feedback",
    ]);
  });

  test("多轮运行各一个，位置不错位；暂停可继续的状态也算已结束", () => {
    reset();
    const items = buildTimeline(
      fold([
        ev("message.user", { text: "一" }, "r1"),
        ev("run.status", { status: "succeeded" }, "r1"),
        ev("message.user", { text: "二" }, "r2"),
        ev("run.status", { status: "step_limit" }, "r2"),
      ]),
    );
    const feedback = items.filter((i) => i.type === "run-feedback");
    expect(feedback.map((i) => i.runId)).toEqual(["r1", "r2"]);
    expect(items.map((i) => i.type)).toEqual([
      "user",
      "run-feedback",
      "user",
      "status",
      "run-feedback",
    ]);
  });
});

/** 服务端的假实现：按需让提交失败 */
let failNext = false;
const calls: string[] = [];
mock.module("@/api/agent", () => ({
  listAgentSessions: async () => [],
  getAgentEvents: async () => [],
  setAgentFeedback: async (runId: string, req: SetAgentFeedbackReq) => {
    calls.push(`PUT ${runId} ${req.rating}`);
    if (failNext) throw new Error("boom");
    return { rating: req.rating, reasons: req.reasons ?? [], comment: req.comment ?? "" };
  },
  deleteAgentFeedback: async (runId: string) => {
    calls.push(`DELETE ${runId}`);
    if (failNext) throw new Error("boom");
  },
}));
const { useAgentStore } = await import("@/store/agent");

const feedbackOf = () => useAgentStore.getState().sessions.s1?.feedback.r1;

describe("agent store：提交与取消反馈", () => {
  let errorSpy: ReturnType<typeof spyOn>;
  beforeEach(() => {
    useAgentStore.setState({ sessions: {}, sessionsByCanvas: {} });
    useAgentStore.getState().handleEvent(ev("run.status", { status: "succeeded" }, "r1", 1));
    failNext = false;
    calls.length = 0;
    errorSpy = spyOn(toast, "error").mockImplementation(() => "");
  });
  afterEach(() => errorSpy.mockRestore());

  test("提交成功：状态以服务端返回为准", async () => {
    const ok = await useAgentStore
      .getState()
      .submitFeedback("s1", "r1", { rating: -1, reasons: ["slow"], comment: "慢" });
    expect(ok).toBe(true);
    expect(calls).toEqual(["PUT r1 -1"]);
    expect(feedbackOf()).toEqual({ rating: -1, reasons: ["slow"], comment: "慢" });
    expect(errorSpy).not.toHaveBeenCalled();
  });

  test("请求发出时就已乐观更新", async () => {
    const pending = useAgentStore.getState().submitFeedback("s1", "r1", { rating: 1 });
    expect(feedbackOf()?.rating).toBe(1);
    await pending;
  });

  test("提交失败：回到提交前的状态并提示，不弹别的", async () => {
    failNext = true;
    const ok = await useAgentStore.getState().submitFeedback("s1", "r1", { rating: 1 });
    expect(ok).toBe(false);
    expect(feedbackOf()).toBeUndefined();
    expect(errorSpy).toHaveBeenCalledTimes(1);
    expect(errorSpy.mock.calls[0][0]).toBe(FEEDBACK_SAVE_FAILED);
  });

  test("改选失败：回到原来的评价而不是清空", async () => {
    await useAgentStore.getState().submitFeedback("s1", "r1", { rating: 1 });
    failNext = true;
    await useAgentStore.getState().submitFeedback("s1", "r1", { rating: -1, reasons: ["other"] });
    expect(feedbackOf()).toEqual({ rating: 1, reasons: [], comment: "" });
  });

  test("取消：调用 DELETE 并清掉；失败则恢复原评价并提示", async () => {
    await useAgentStore.getState().submitFeedback("s1", "r1", { rating: 1 });
    failNext = true;
    expect(await useAgentStore.getState().cancelFeedback("s1", "r1")).toBe(false);
    expect(feedbackOf()?.rating).toBe(1);
    expect(errorSpy).toHaveBeenCalledTimes(1);

    failNext = false;
    expect(await useAgentStore.getState().cancelFeedback("s1", "r1")).toBe(true);
    expect(feedbackOf()).toBeUndefined();
    expect(calls.at(-1)).toBe("DELETE r1");
  });

  test("服务端随后推来的 feedback.set 事件与乐观结果一致，重复无副作用", async () => {
    await useAgentStore.getState().submitFeedback("s1", "r1", { rating: 1 });
    const before = useAgentStore.getState().sessions.s1.feedback.r1;
    useAgentStore.getState().handleEvent(ev("feedback.set", { run_id: "r1", rating: 1 }, "r1", 2));
    useAgentStore.getState().handleEvent(ev("feedback.set", { run_id: "r1", rating: 1 }, "r1", 2));
    expect(useAgentStore.getState().sessions.s1.feedback.r1).toEqual(before);
  });
});
