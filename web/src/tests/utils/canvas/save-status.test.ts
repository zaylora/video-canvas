import { describe, expect, test } from "bun:test";

import {
  CLOUD_FAIL_VISIBLE_AT,
  STATUS_SETTLE_MS,
  displayStatus,
  statusSettleDelay,
} from "@/utils/canvas/save-status";

const idle = {
  conflict: false,
  cloudFailCount: 0,
  localPending: false,
  lastLocalWriteAt: null,
  fallbackBusy: false,
  now: 10_000,
};

describe("displayStatus", () => {
  test("什么都没发生：已保存", () => {
    expect(displayStatus(idle)).toBe("saved");
  });

  test("有待写入的本地改动：立即显示正在保存", () => {
    expect(displayStatus({ ...idle, localPending: true })).toBe("saving");
  });

  test("最后一次写入后静默不满 0.8 秒：仍显示正在保存（防抖）", () => {
    const lastLocalWriteAt = 10_000 - (STATUS_SETTLE_MS - 1);
    expect(displayStatus({ ...idle, lastLocalWriteAt })).toBe("saving");
  });

  test("静默满 0.8 秒：切到已保存", () => {
    const lastLocalWriteAt = 10_000 - STATUS_SETTLE_MS;
    expect(displayStatus({ ...idle, lastLocalWriteAt })).toBe("saved");
  });

  test("云端第一次失败：不变色", () => {
    expect(displayStatus({ ...idle, cloudFailCount: CLOUD_FAIL_VISIBLE_AT - 1 })).toBe("saved");
  });

  test("云端第一次自动重试仍失败：变红", () => {
    expect(displayStatus({ ...idle, cloudFailCount: CLOUD_FAIL_VISIBLE_AT })).toBe("error");
    expect(CLOUD_FAIL_VISIBLE_AT).toBe(2);
  });

  test("冲突立即显示，优先于失败和保存中", () => {
    expect(displayStatus({ ...idle, conflict: true, cloudFailCount: 3, localPending: true })).toBe(
      "conflict",
    );
  });

  test("失败优先于保存中：红色不被防抖的转圈盖住", () => {
    expect(displayStatus({ ...idle, cloudFailCount: 2, localPending: true })).toBe("error");
  });

  test("没有本地草稿兜底时：云端有改动在途就显示正在保存", () => {
    expect(displayStatus({ ...idle, fallbackBusy: true })).toBe("saving");
  });
});

describe("statusSettleDelay", () => {
  test("没写过：不需要等", () => {
    expect(statusSettleDelay({ now: 10_000, lastLocalWriteAt: null, localPending: false })).toBe(0);
  });

  test("还有待写入：不排计时，等写完再算", () => {
    expect(statusSettleDelay({ now: 10_000, lastLocalWriteAt: 9_900, localPending: true })).toBe(0);
  });

  test("写完后还差多久静默满", () => {
    expect(statusSettleDelay({ now: 10_000, lastLocalWriteAt: 9_700, localPending: false })).toBe(
      STATUS_SETTLE_MS - 300,
    );
  });

  test("已经静默够了：0", () => {
    expect(statusSettleDelay({ now: 20_000, lastLocalWriteAt: 9_700, localPending: false })).toBe(
      0,
    );
  });
});
