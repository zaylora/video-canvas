import { describe, expect, test } from "bun:test";

import { FLOATING_FRAME, fitFrame, floatingFrame } from "@/utils/director/layout";

describe("取景框尺寸：在给定的空间里放下最大的、保持画幅的矩形", () => {
  test("空间比画幅更扁：高度顶满，两边留白", () => {
    expect(fitFrame(1000, 300, 16 / 9)).toEqual({ width: 533, height: 300 });
  });

  test("空间比画幅更窄：宽度顶满，上下留白", () => {
    expect(fitFrame(400, 600, 16 / 9)).toEqual({ width: 400, height: 225 });
  });

  test("竖画幅放进横向空间：高度顶满", () => {
    expect(fitFrame(800, 400, 9 / 16)).toEqual({ width: 225, height: 400 });
  });

  test("方画幅", () => {
    expect(fitFrame(500, 300, 1)).toEqual({ width: 300, height: 300 });
  });

  test("还没量出空间（0 或负数）时给 0，不画", () => {
    expect(fitFrame(0, 300, 16 / 9)).toEqual({ width: 0, height: 0 });
    expect(fitFrame(300, 0, 16 / 9)).toEqual({ width: 0, height: 0 });
    expect(fitFrame(-5, -5, 16 / 9)).toEqual({ width: 0, height: 0 });
  });

  test("结果都是整数，且不超出空间", () => {
    for (const ratio of [16 / 9, 9 / 16, 1, 4 / 3, 21 / 9]) {
      const frame = fitFrame(777, 433, ratio);
      expect(Number.isInteger(frame.width)).toBe(true);
      expect(Number.isInteger(frame.height)).toBe(true);
      expect(frame.width).toBeLessThanOrEqual(777);
      expect(frame.height).toBeLessThanOrEqual(433);
    }
  });
});

describe("浮窗的取景框", () => {
  test("横画幅按宽度定", () => {
    expect(floatingFrame(16 / 9)).toEqual({ width: FLOATING_FRAME.width, height: 167 });
  });

  test("竖画幅太高时按最大高度收，免得浮窗占满视口", () => {
    const frame = floatingFrame(9 / 16);
    expect(frame.height).toBe(FLOATING_FRAME.maxHeight);
    expect(frame.width).toBe(Math.round(FLOATING_FRAME.maxHeight * (9 / 16)));
  });
});
