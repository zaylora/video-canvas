import { describe, expect, test } from "bun:test";

import { pendingCopy } from "@/utils/tasks/pending-copy";

describe("pendingCopy：占位上的文案", () => {
  test("排队中：说明要等前面的任务", () => {
    expect(pendingCopy({ phase: "queued" }, "image")).toEqual({
      title: "排队中",
      detail: "前面的任务处理完后会自动开始",
      progress: null,
    });
  });

  test("生成中：显示已耗时和进度", () => {
    expect(pendingCopy({ phase: "running", elapsedMs: 65_000, progress: 40 }, "video")).toEqual({
      title: "生成中",
      detail: "已耗时 1分05秒",
      progress: 40,
    });
  });

  test("生成中但耗时未知：提示正在同步任务状态", () => {
    const copy = pendingCopy({ phase: "running", elapsedMs: null, progress: null }, "image");
    expect(copy.detail).toBe("正在同步任务状态…");
    expect(copy.progress).toBeNull();
  });

  test("即将完成：按素材种类说正在保存什么", () => {
    expect(pendingCopy({ phase: "finalizing" }, "image").detail).toBe("正在保存图片");
    expect(pendingCopy({ phase: "finalizing" }, "video").detail).toBe("正在保存视频");
    expect(pendingCopy({ phase: "finalizing" }, "audio").detail).toBe("正在保存音频");
    expect(pendingCopy({ phase: "finalizing" }, "image").title).toBe("即将完成");
  });
});
