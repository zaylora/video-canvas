import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { RatioIcon } from "@/components/ratio-icon";
import { ratioFrame } from "@/utils/ratio-icon";

describe("比例图标的外形", () => {
  test.each([
    ["16:9", 16 / 9],
    ["21:9", 21 / 9],
    ["9:16", 9 / 16],
    ["4:3", 4 / 3],
    ["1:1", 1],
  ])("%s 画出来的框和标称比例一致", (ratio, expected) => {
    const frame = ratioFrame(ratio);
    expect(frame).not.toBeNull();
    const actual = frame!.width / frame!.height;
    expect(Math.abs(actual - expected) / expected).toBeLessThan(0.01);
  });

  test("扁、窄的比例也留得出内部空间，不会被描边和圆角挤成胶囊", () => {
    const flat = ratioFrame("21:9")!;
    expect(flat.height - 2 * flat.stroke).toBeGreaterThan(flat.height / 2);
    expect(flat.radius).toBeLessThanOrEqual(flat.height / 4);
  });

  test("所有比例放在同样大的外框里，长边一样长，方便对比形状", () => {
    const long = ["16:9", "9:16", "1:1", "21:9"].map((ratio) => {
      const frame = ratioFrame(ratio)!;
      return Math.max(frame.width, frame.height);
    });
    expect(new Set(long).size).toBe(1);
  });

  test("不是 宽:高 的值（Auto）没有比例框", () => {
    expect(ratioFrame("Auto")).toBeNull();
    expect(ratioFrame("auto")).toBeNull();
  });

  test("Auto 和 1:1 画的不是同一个图形", () => {
    const auto = renderToStaticMarkup(<RatioIcon ratio="Auto" />);
    const square = renderToStaticMarkup(<RatioIcon ratio="1:1" />);
    expect(auto).not.toBe(square);
    expect(square).toContain("<rect");
    expect(auto).not.toContain("<rect");
  });
});
