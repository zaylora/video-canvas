import { describe, expect, test } from "bun:test";

import {
  cancelableTaskIds,
  cancelRefunds,
  canceledRefunded,
  isCancelableTask,
  outputText,
} from "@/utils/tasks/status";
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

describe("isCancelableTask：转存前都能取消，转存中和终态不能", () => {
  const at = "2026-09-29T10:00:05Z";

  test("排队中、同步请求已发出、平台已受理、生成中：能取消", () => {
    expect(isCancelableTask(makeTask({ status: "pending", submitted_at: null }))).toBe(true);
    expect(isCancelableTask(makeTask({ status: "pending", submitted_at: at }))).toBe(true);
    expect(isCancelableTask(makeTask({ status: "queued", submitted_at: at }))).toBe(true);
    expect(isCancelableTask(makeTask({ status: "running", submitted_at: at }))).toBe(true);
  });

  test("转存中结果已到手，不能取消", () => {
    expect(isCancelableTask(makeTask({ status: "finalizing", submitted_at: at }))).toBe(false);
  });

  test("终态不能取消", () => {
    for (const status of ["succeeded", "failed", "canceled", "expired"] as const) {
      expect(isCancelableTask(makeTask({ status }))).toBe(false);
    }
  });
});

describe("cancelRefunds：取消后积分退不退", () => {
  const at = "2026-09-29T10:00:05Z";

  test("还没提交给上游（排队中）退，已经进入生成不退", () => {
    expect(cancelRefunds(makeTask({ status: "pending", submitted_at: null }))).toBe(true);
    expect(cancelRefunds(makeTask({ status: "pending", submitted_at: at }))).toBe(false);
    expect(cancelRefunds(makeTask({ status: "running", submitted_at: at }))).toBe(false);
  });
});

describe("canceledRefunded：已取消的任务积分有没有退", () => {
  test("没有实扣积分就是退了，有实扣积分就是没退", () => {
    expect(canceledRefunded(makeTask({ status: "canceled", charged_credits: null }))).toBe(true);
    expect(canceledRefunded(makeTask({ status: "canceled", charged_credits: 0 }))).toBe(true);
    expect(canceledRefunded(makeTask({ status: "canceled", charged_credits: 8 }))).toBe(false);
  });
});

describe("cancelableTaskIds：从一批任务 id 里挑出该去取消的", () => {
  test("已知且转存中 / 已结束的跳过；已知在排队或生成的、本地还没有快照的都留下（后端会再判断一次）", () => {
    const tasks = {
      "1": makeTask({ id: 1, status: "pending", submitted_at: null }),
      "2": makeTask({ id: 2, status: "running", submitted_at: "2026-09-29T10:00:05Z" }),
      "3": makeTask({ id: 3, status: "finalizing", submitted_at: "2026-09-29T10:00:05Z" }),
    };
    expect(cancelableTaskIds([1, 2, 3, 4], tasks)).toEqual([1, 2, 4]);
    expect(cancelableTaskIds([], tasks)).toEqual([]);
  });
});
