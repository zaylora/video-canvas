import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { RatioIcon } from "@/components/ratio-icon";
import { RATIO_ICON_HEIGHT, ratioFrame, ratioIconBox } from "@/utils/ratio-icon";

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

  test("短边一样长、都等于 1:1 的边长，长边按比例伸长", () => {
    const square = ratioFrame("1:1")!;
    for (const ratio of ["16:9", "9:16", "4:3", "3:4", "21:9"]) {
      const frame = ratioFrame(ratio)!;
      expect(Math.min(frame.width, frame.height)).toBeCloseTo(square.width, 5);
    }
    expect(ratioFrame("21:9")!.width).toBeGreaterThan(ratioFrame("16:9")!.width);
  });

  test("所有比例图标高度一样，放在一排时下面的文字对齐；框在图标里不越界", () => {
    for (const ratio of ["16:9", "9:16", "1:1", "21:9", "Auto"]) {
      const box = ratioIconBox(ratio);
      expect(box.height).toBe(RATIO_ICON_HEIGHT);
      const frame = ratioFrame(ratio);
      if (!frame) continue;
      expect(frame.x).toBeGreaterThanOrEqual(0);
      expect(frame.y).toBeGreaterThanOrEqual(0);
      expect(frame.x + frame.width).toBeLessThanOrEqual(box.width);
      expect(frame.y + frame.height).toBeLessThanOrEqual(box.height);
    }
  });

  test("图标宽度随比例变：横的宽、竖的窄，不留大块空白", () => {
    expect(ratioIconBox("21:9").width).toBeGreaterThan(ratioIconBox("16:9").width);
    expect(ratioIconBox("16:9").width).toBeGreaterThan(ratioIconBox("1:1").width);
    expect(ratioIconBox("9:16").width).toBe(ratioIconBox("1:1").width);
  });

  test("特别细长的比例长边封顶，图标不会无限长", () => {
    const frame = ratioFrame("10:1")!;
    expect(frame.width).toBeLessThanOrEqual(ratioIconBox("10:1").width);
    expect(ratioIconBox("10:1").width).toBeLessThan(40);
    const tall = ratioFrame("1:10")!;
    expect(tall.y + tall.height).toBeLessThanOrEqual(RATIO_ICON_HEIGHT);
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
