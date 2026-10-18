import { describe, expect, test } from "bun:test";

import { cancelableTaskIds, isCancelableTask, outputText } from "@/utils/tasks/status";
import { makeTask, succeeded, textSucceeded } from "./fixtures";

describe("outputText：文本产出提取", () => {
  test("取第一份带正文的产出，原样保留换行与首尾空白", () => {
    expect(outputText(textSucceeded("  你好\n世界 "))).toBe("  你好\n世界 ");
  });

  test("跳过没有 text 的产出", () => {
    const view = textSucceeded("x", {
      outputs: [
        { media_type: "text", text: " " },
        { media_type: "text", text: "正文" },
      ],
    });
    expect(outputText(view)).toBe("正文");
  });

  test("没有文本产出（null、空数组、媒体产出）返回 undefined", () => {
    expect(outputText(makeTask())).toBeUndefined();
    expect(outputText(textSucceeded("x", { outputs: [] }))).toBeUndefined();
    expect(outputText(succeeded())).toBeUndefined();
  });
});

describe("isCancelableTask：只有还没提交给上游的才能取消", () => {
  test("pending 且没有提交时间（界面上的「排队中」）：能取消", () => {
    expect(isCancelableTask(makeTask({ status: "pending", submitted_at: null }))).toBe(true);
  });

  test("已经调用上游就不能取消：同步请求已发出（pending 但有提交时间）、平台已受理、生成中、转存中", () => {
    const at = "2026-09-29T10:00:05Z";
    expect(isCancelableTask(makeTask({ status: "pending", submitted_at: at }))).toBe(false);
    expect(isCancelableTask(makeTask({ status: "queued", submitted_at: at }))).toBe(false);
    expect(isCancelableTask(makeTask({ status: "running", submitted_at: at }))).toBe(false);
    expect(isCancelableTask(makeTask({ status: "finalizing", submitted_at: at }))).toBe(false);
  });

  test("终态不能取消", () => {
    for (const status of ["succeeded", "failed", "canceled", "expired"] as const) {
      expect(isCancelableTask(makeTask({ status }))).toBe(false);
    }
  });
});

describe("cancelableTaskIds：从一批任务 id 里挑出该去取消的", () => {
  test("已知且已提交给上游的跳过；已知排队中的、本地还没有快照的都留下（后端会再判断一次）", () => {
    const tasks = {
      "1": makeTask({ id: 1, status: "pending", submitted_at: null }),
      "2": makeTask({ id: 2, status: "running", submitted_at: "2026-09-29T10:00:05Z" }),
    };
    expect(cancelableTaskIds([1, 2, 3], tasks)).toEqual([1, 3]);
    expect(cancelableTaskIds([], tasks)).toEqual([]);
  });
});
