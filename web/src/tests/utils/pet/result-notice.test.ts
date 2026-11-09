import { describe, expect, test } from "bun:test";

import type { TaskView } from "@/api/generation-task/type";
import type { BackfillEvent } from "@/utils/tasks/backfill";
import {
  NodeIdAccumulator,
  buildResultNotices,
  isFreshTerminal,
  resultText,
} from "@/utils/pet/result-notice";

const view = (patch: Partial<TaskView> & { id: number | string }): TaskView =>
  ({
    canvas_id: "c1",
    node_id: "n1",
    kind: "image",
    model_id: "m",
    status: "succeeded",
    progress: null,
    outputs: null,
    error_code: null,
    error_message: null,
    credits: 1,
    charged_credits: null,
    version: 1,
    deadline_at: null,
    created_at: "2026-11-06T10:00:00Z",
    submitted_at: null,
    finished_at: null,
    ...patch,
  }) as TaskView;
const ev = (nodeId: string, v: TaskView, patch: BackfillEvent["patch"] = {}): BackfillEvent => ({
  nodeId,
  view: v,
  patch,
});
const fresh = () => true;

describe("resultText", () => {
  test("按任务种类选量词", () => {
    expect(resultText("image")).toBe("{n} 张图好了");
    expect(resultText("video")).toBe("{n} 个视频好了");
    expect(resultText("audio")).toBe("{n} 段声音好了");
    expect(resultText("text")).toBe("{n} 段文字好了");
    expect(resultText("other")).toBe("{n} 个结果好了");
  });
});

describe("isFreshTerminal：只对本次会话新变成终态的任务冒泡", () => {
  const opened = Date.parse("2026-11-06T10:00:00Z");
  test("本次会话里见过它在进行中：新鲜", () => {
    const v = view({ id: 1, finished_at: "2026-11-06T09:00:00Z" });
    expect(isFreshTerminal(v, new Set(["1"]), opened)).toBe(true);
  });
  test("没见过进行中，但结束时间在打开画布之后：新鲜", () => {
    const v = view({ id: 2, finished_at: "2026-11-06T10:00:05Z" });
    expect(isFreshTerminal(v, new Set(), opened)).toBe(true);
  });
  test("没见过进行中，且打开画布前就结束了：不新鲜", () => {
    const v = view({ id: 3, finished_at: "2026-11-06T09:59:00Z" });
    expect(isFreshTerminal(v, new Set(), opened)).toBe(false);
  });
  test("没有结束时间也没见过进行中：不新鲜", () => {
    expect(isFreshTerminal(view({ id: 4 }), new Set(), opened)).toBe(false);
  });
});

describe("NodeIdAccumulator：合并窗口内累加节点 id", () => {
  test("窗口内共用同一个集合并累加，去重", () => {
    const acc = new NodeIdAccumulator(2000);
    const a = acc.add("k", ["n1"], 0);
    const b = acc.add("k", ["n2", "n1"], 1500);
    expect(b).toBe(a);
    expect([...a]).toEqual(["n1", "n2"]);
  });
  test("超过窗口开新集合，旧气泡的集合不再被污染", () => {
    const acc = new NodeIdAccumulator(2000);
    const a = acc.add("k", ["n1"], 0);
    const b = acc.add("k", ["n2"], 2500);
    expect(b).not.toBe(a);
    expect([...a]).toEqual(["n1"]);
    expect([...b]).toEqual(["n2"]);
  });
  test("窗口从最近一次累加算起", () => {
    const acc = new NodeIdAccumulator(2000);
    const a = acc.add("k", ["n1"], 0);
    acc.add("k", ["n2"], 1800);
    expect(acc.add("k", ["n3"], 3500)).toBe(a);
  });
  test("不同 key 互不影响", () => {
    const acc = new NodeIdAccumulator(2000);
    expect(acc.add("a", ["n1"], 0)).not.toBe(acc.add("b", ["n1"], 0));
  });
});

describe("buildResultNotices", () => {
  test("成功：每个任务一条，按种类分 key，只含新鲜的", () => {
    const events = [
      ev("n1", view({ id: 1, kind: "image" })),
      ev("n2", view({ id: 2, kind: "video" })),
      ev("n3", view({ id: 3, kind: "image" })),
    ];
    const notices = buildResultNotices(events, "c1", (v) => String(v.id) !== "3");
    expect(notices).toHaveLength(2);
    expect(notices[0]).toMatchObject({
      level: "success",
      text: "{n} 张图好了",
      id: "result:success:1",
      key: "result:success:c1:image",
      petOnly: true,
      nodeIds: ["n1"],
    });
    expect(notices[1]).toMatchObject({ text: "{n} 个视频好了", key: "result:success:c1:video" });
  });

  test("成功但节点被写成失败（结果为空）按失败处理", () => {
    const events = [ev("n1", view({ id: 1 }), { status: "error", error: "生成结果为空，请重试" })];
    const [n] = buildResultNotices(events, "c1", fresh);
    expect(n).toMatchObject({ level: "error", petOnly: true, nodeIds: ["n1"] });
    expect(n!.text).toBe("生成失败，积分已退回：生成结果为空，请重试");
  });

  test("整个节点失败：带原因，没有原因给默认文案；过期是「生成超时」", () => {
    const failed = ev("n1", view({ id: 1, status: "failed", error_message: "额度不足" }), {
      status: "error",
    });
    const expired = ev("n2", view({ id: 2, status: "expired" }), { status: "error" });
    const [a, b] = buildResultNotices([failed, expired], "c1", fresh);
    expect(a!.text).toBe("生成失败，积分已退回：额度不足");
    expect(b!.text).toBe("生成失败，积分已退回：生成超时");
  });

  test("不新鲜的成功和整体失败都不冒", () => {
    const events = [
      ev("n1", view({ id: 1 })),
      ev("n2", view({ id: 2, status: "failed" }), { status: "error" }),
    ];
    expect(buildResultNotices(events, "c1", () => false)).toEqual([]);
  });

  test("取消：沿用原 toast 文案和 id，不受新鲜度限制，不是仅桌宠", () => {
    const refunded = ev("n1", view({ id: 5, status: "canceled", charged_credits: null }));
    const kept = ev("n2", view({ id: 6, status: "canceled", charged_credits: 3 }));
    const [a, b] = buildResultNotices([refunded, kept], "c1", () => false);
    expect(a).toMatchObject({
      level: "info",
      text: "已取消生成，积分已退回",
      id: "canceled:5",
      petOnly: false,
    });
    expect(b!.text).toBe("已取消生成，已进入生成的积分不退回");
  });

  test("批次部分失败：沿用原 toast 文案，取第一条原因", () => {
    const events = [
      ev("n1", view({ id: 7, status: "failed", error_message: "审核未通过" }), {
        pendingTaskIds: ["8"],
      }),
      ev("n1", view({ id: 9, status: "expired" }), { pendingTaskIds: undefined }),
    ];
    const notices = buildResultNotices(events, "c1", () => false);
    expect(notices).toHaveLength(1);
    expect(notices[0]).toMatchObject({
      level: "warning",
      text: "有 2 张生成失败，积分已退回：审核未通过",
      id: "batch-failed:n1:7",
      petOnly: false,
      nodeIds: ["n1"],
    });
  });
});
