import { describe, expect, test } from "bun:test";

import { deriveVideoNodeView, formatElapsed, normalizeProgress } from "@/utils/tasks/node-view";
import { makeTask } from "./fixtures";

const T0 = Date.parse("2026-09-29T10:00:00Z");

describe("deriveVideoNodeView：节点状态 -> 展示状态（设计 6.3）", () => {
  test("没跑过：idle；上传来的素材直接展示", () => {
    expect(deriveVideoNodeView({}, undefined, T0)).toEqual({ phase: "idle" });
    expect(deriveVideoNodeView({ status: "idle", src: "blob:x" }, undefined, T0)).toEqual({
      phase: "done",
      src: "blob:x",
    });
  });

  test("排队中：只有 pending（还没调用上游，在等执行名额 / 渠道并发）", () => {
    expect(
      deriveVideoNodeView({ status: "running", taskId: "1" }, makeTask({ status: "pending" }), T0),
    ).toEqual({ phase: "queued" });
  });

  test("生成中：queued 和 running 都算（已经调用上游），平台给进度就带进度", () => {
    for (const status of ["queued", "running"] as const) {
      const view = deriveVideoNodeView(
        { status: "running", taskId: "1" },
        makeTask({ status, progress: 42, submitted_at: "2026-09-29T10:00:00Z" }),
        T0 + 65_000,
      );
      expect(view).toEqual({ phase: "running", elapsedMs: 65_000, progress: 42 });
    }
  });

  test("耗时从提交给上游那一刻起算，排队的时间不算；没有提交时间退回创建时间", () => {
    const task = makeTask({ status: "queued", submitted_at: "2026-09-29T10:00:40Z" });
    expect(
      deriveVideoNodeView({ status: "running", taskId: "1" }, task, T0 + 65_000),
    ).toMatchObject({
      phase: "running",
      elapsedMs: 25_000,
    });
    const legacy = makeTask({ status: "running", submitted_at: null });
    expect(
      deriveVideoNodeView({ status: "running", taskId: "1" }, legacy, T0 + 65_000),
    ).toMatchObject({
      elapsedMs: 65_000,
    });
  });

  test("时钟比服务端慢时耗时不会出现负数", () => {
    const view = deriveVideoNodeView(
      { status: "running", taskId: "1" },
      makeTask({ status: "running" }),
      T0 - 5_000,
    );
    expect(view).toMatchObject({ phase: "running", elapsedMs: 0 });
  });

  test("任务快照还没到：显示生成中，耗时未知", () => {
    expect(deriveVideoNodeView({ status: "running", taskId: "1" }, undefined, T0)).toEqual({
      phase: "running",
      elapsedMs: null,
      progress: null,
    });
  });

  test("转存中与终态未回填的一瞬间：即将完成", () => {
    for (const status of ["finalizing", "succeeded"] as const) {
      expect(
        deriveVideoNodeView({ status: "running", taskId: "1" }, makeTask({ status }), T0),
      ).toEqual({ phase: "finalizing" });
    }
  });

  test("成功：可播放", () => {
    expect(deriveVideoNodeView({ status: "done", src: "/files/a.mp4" }, undefined, T0)).toEqual({
      phase: "done",
      src: "/files/a.mp4",
    });
  });

  test("失败：任务失败标注积分已退回，本地错误（如上传失败）不标", () => {
    expect(
      deriveVideoNodeView({ status: "error", error: "内容未通过审核", taskId: "1" }, undefined, T0),
    ).toEqual({ phase: "failed", message: "内容未通过审核", refunded: true });
    expect(
      deriveVideoNodeView({ status: "error", error: "上传失败，请重试" }, undefined, T0),
    ).toEqual({
      phase: "failed",
      message: "上传失败，请重试",
      refunded: false,
    });
  });
});

describe("normalizeProgress / formatElapsed", () => {
  test("进度取整并夹在 0-100，非法值当没有", () => {
    expect(normalizeProgress(41.6)).toBe(42);
    expect(normalizeProgress(-3)).toBe(0);
    expect(normalizeProgress(180)).toBe(100);
    expect(normalizeProgress(null)).toBeNull();
    expect(normalizeProgress(Number.NaN)).toBeNull();
  });

  test("耗时格式", () => {
    expect(formatElapsed(0)).toBe("0秒");
    expect(formatElapsed(45_900)).toBe("45秒");
    expect(formatElapsed(65_000)).toBe("1分05秒");
    expect(formatElapsed(3_725_000)).toBe("62分05秒");
  });
});
