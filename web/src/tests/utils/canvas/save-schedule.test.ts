import { describe, expect, test } from "bun:test";

import {
  SAVE_IDLE_MS,
  SAVE_MAX_WAIT_MS,
  canKeepalive,
  nextSaveDelay,
} from "@/utils/canvas/save-schedule";

describe("nextSaveDelay", () => {
  test("刚改完：等满停手窗口", () => {
    expect(nextSaveDelay({ now: 1000, firstDirtyAt: 1000, lastChangeAt: 1000 })).toBe(SAVE_IDLE_MS);
  });

  test("持续编辑：每次改动都把停手窗口往后推", () => {
    expect(nextSaveDelay({ now: 5000, firstDirtyAt: 1000, lastChangeAt: 5000 })).toBe(SAVE_IDLE_MS);
  });

  test("连续编辑接近最长等待：被兜底压短", () => {
    // 首次标脏后 29s，最后一次改动就在此刻，停手窗口要 3s，但最长等待只剩 1s
    const now = 30_000;
    const delay = nextSaveDelay({ now, firstDirtyAt: now - 29_000, lastChangeAt: now });
    expect(delay).toBe(SAVE_MAX_WAIT_MS - 29_000);
  });

  test("已超过最长等待：立即保存，不出现负数", () => {
    const now = 60_000;
    expect(nextSaveDelay({ now, firstDirtyAt: 0, lastChangeAt: now })).toBe(0);
  });

  test("在途保存落地后：停手窗口已过就立即排，没过只补差额", () => {
    // 最后一次改动在 1s 前：还要再等 2s，而不是整整 3s，更不是立即补发
    expect(nextSaveDelay({ now: 11_000, firstDirtyAt: 9_000, lastChangeAt: 10_000 })).toBe(
      SAVE_IDLE_MS - 1000,
    );
    // 最后一次改动在 5s 前：窗口已过，立即保存
    expect(nextSaveDelay({ now: 15_000, firstDirtyAt: 9_000, lastChangeAt: 10_000 })).toBe(0);
  });

  test("参数可覆盖", () => {
    expect(
      nextSaveDelay({ now: 0, firstDirtyAt: 0, lastChangeAt: 0, idleMs: 100, maxWaitMs: 50 }),
    ).toBe(50);
  });
});

describe("canKeepalive", () => {
  test("小 payload 可以走 keepalive", () => {
    expect(canKeepalive({ nodes: [], edges: [], viewport: { x: 0, y: 0, zoom: 1 } })).toBe(true);
  });

  test("按字节算：多字节中文也不能超过上限", () => {
    const big = "字".repeat(30_000); // UTF-8 下约 90KB
    expect(canKeepalive({ big })).toBe(false);
  });
});
