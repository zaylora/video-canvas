import { describe, expect, test } from "bun:test";

import { cutButtonScale, nearestRatio } from "@/utils/canvas/edge-cut";

describe("cutButtonScale：断开按钮在屏幕上的缩放系数", () => {
  test("100% 缩放时就是原大小", () => {
    expect(cutButtonScale(1)).toBe(1);
  });

  test("只轻微跟着画布缩放：放大变大一点，缩小变小一点", () => {
    expect(cutButtonScale(1.5)).toBeGreaterThan(1);
    expect(cutButtonScale(0.6)).toBeLessThan(1);
    // 画布缩放 2 倍，按钮远没有跟着放大 2 倍
    expect(cutButtonScale(2)).toBeLessThan(1.3);
  });

  test("再极端的缩放也夹在 0.8 到 1.2 之间，缩到很小时仍点得到", () => {
    expect(cutButtonScale(0.05)).toBe(0.8);
    expect(cutButtonScale(8)).toBe(1.2);
  });
});

describe("nearestRatio：把点击点投影到连线上", () => {
  const line = (t: number) => ({ x: 100 * t, y: 0 });

  test("落在线旁边的点，取线上离它最近的位置", () => {
    expect(nearestRatio(line, { x: 30, y: 12 })).toBeCloseTo(0.3, 2);
    expect(nearestRatio(line, { x: 71, y: -9 })).toBeCloseTo(0.71, 2);
  });

  test("超出两端的点夹到起点和终点", () => {
    expect(nearestRatio(line, { x: -40, y: 5 })).toBe(0);
    expect(nearestRatio(line, { x: 240, y: 5 })).toBe(1);
  });

  test("弯曲的线也行：四分之一圆弧上取最近点", () => {
    const arc = (t: number) => ({
      x: 100 * Math.cos((t * Math.PI) / 2),
      y: 100 * Math.sin((t * Math.PI) / 2),
    });
    // 45° 方向上稍微靠外的点，最近的是弧的正中
    expect(nearestRatio(arc, { x: 80, y: 80 })).toBeCloseTo(0.5, 2);
  });
});
