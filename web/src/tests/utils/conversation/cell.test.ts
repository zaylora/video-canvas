import { describe, expect, test } from "bun:test";

import type { RecordDto } from "@/api/conversation/type";
import { makeTask, succeeded } from "../tasks/fixtures";
import { deriveCell, recordActive, recordCredits } from "@/utils/conversation/cell";

/** 固定「现在」：任务创建 10 分钟后 */
const NOW = Date.parse("2026-09-29T10:10:00Z");

const record = (over: Partial<RecordDto> = {}): RecordDto => ({
  id: "r1",
  conversationId: "c1",
  kind: "image",
  modelId: "m",
  prompt: "p",
  input: {},
  count: 2,
  tasks: [null, null],
  submitErrors: [],
  quoteCredits: 0,
  createdAt: "2026-10-09T08:00:00Z",
  ...over,
});

describe("deriveCell：任务状态 → 格子状态", () => {
  test("排队、生成中、转存中", () => {
    expect(deriveCell(record(), 0, makeTask({ status: "pending" }), NOW).kind).toBe("queued");
    const running = deriveCell(record(), 0, makeTask({ status: "running", progress: 40 }), NOW);
    expect(running).toEqual({ kind: "running", elapsedMs: 10 * 60_000 - 0, progress: 40 });
    expect(deriveCell(record(), 0, makeTask({ status: "finalizing" }), NOW).kind).toBe(
      "finalizing",
    );
  });

  test("和画布节点同一套说法：queued（已提交给上游）也是生成中，耗时从提交那一刻起算", () => {
    const submitted = makeTask({ status: "queued", submitted_at: "2026-09-29T10:07:30Z" });
    expect(deriveCell(record(), 0, submitted, NOW)).toEqual({
      kind: "running",
      elapsedMs: 150_000,
      progress: null,
    });
  });

  test("成功：取第一个产出", () => {
    const cell = deriveCell(record(), 0, succeeded(), NOW);
    expect(cell.kind).toBe("done");
    if (cell.kind === "done") expect(cell.output.url).toBe("/files/a.mp4");
  });

  test("成功但没有产出：按失败处理", () => {
    const cell = deriveCell(record(), 0, makeTask({ status: "succeeded", outputs: [] }), NOW);
    expect(cell).toEqual({ kind: "failed", message: "没有生成出结果", taskRef: null });
  });

  test("失败的格子带上任务 ID，方便到日志里定位", () => {
    const failed = makeTask({
      id: 123,
      status: "failed",
      error_message: "平台繁忙",
      task_ref: "ab12",
    });
    expect(deriveCell(record(), 0, failed, NOW)).toEqual({
      kind: "failed",
      message: "平台繁忙",
      taskRef: "ab12",
    });
    expect(
      deriveCell(record(), 0, makeTask({ status: "expired", task_ref: "cd34" }), NOW),
    ).toMatchObject({ taskRef: "cd34" });
  });

  test("失败、过期：带原因；取消：单独一种", () => {
    expect(
      deriveCell(record(), 0, makeTask({ status: "failed", error_message: "内容审核未通过" }), NOW),
    ).toEqual({ kind: "failed", message: "内容审核未通过", taskRef: null });
    expect(deriveCell(record(), 0, makeTask({ status: "failed" }), NOW)).toEqual({
      kind: "failed",
      message: "生成失败",
      taskRef: null,
    });
    expect(deriveCell(record(), 0, makeTask({ status: "expired" }), NOW)).toEqual({
      kind: "failed",
      message: "生成超时",
      taskRef: null,
    });
    expect(deriveCell(record(), 0, makeTask({ status: "canceled" }), NOW).kind).toBe("canceled");
  });

  test("没有任务：有提交失败原因就显示它，否则是任务丢失", () => {
    const rec = record({
      submitErrors: [{ index: 1, status: 402, code: 40001, message: "积分不足" }],
    });
    expect(deriveCell(rec, 1, undefined, NOW)).toEqual({
      kind: "submit_error",
      message: "积分不足",
    });
    expect(deriveCell(rec, 0, undefined, NOW)).toEqual({ kind: "missing" });
  });
});

describe("recordActive / recordCredits", () => {
  const tasks = [
    makeTask({ id: 1, status: "running", credits: 3 }),
    succeeded({ id: 2, credits: 3, charged_credits: 3 }),
  ];

  test("任一格进行中就是进行中；全部终态或没有任务不是", () => {
    expect(recordActive(tasks)).toBe(true);
    expect(recordActive([succeeded({ id: 2 }), makeTask({ id: 3, status: "failed" })])).toBe(false);
    expect(recordActive([])).toBe(false);
  });

  test("进行中显示冻结合计；全部结束显示实际扣费合计，失败退回的不算", () => {
    expect(recordCredits(tasks)).toEqual({ credits: 6, frozen: true });
    const finished = [
      succeeded({ id: 2, credits: 3, charged_credits: 2 }),
      makeTask({ id: 3, status: "failed", credits: 3 }),
    ];
    expect(recordCredits(finished)).toEqual({ credits: 2, frozen: false });
  });

  test("成功但还没结算（charged 为 null）按冻结额算", () => {
    expect(recordCredits([succeeded({ id: 2, credits: 4, charged_credits: null })])).toEqual({
      credits: 4,
      frozen: false,
    });
  });
});
