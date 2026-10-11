import { describe, expect, test } from "bun:test";

import {
  MAX_MULT,
  detectRatio,
  fitsCap,
  frameByMult,
  frameByRatio,
  frameCap,
  frameHeight,
  frameWidth,
  longMult,
  moveFrame,
  ratioOfFrame,
  resizeFrame,
  snapRatio,
  toRatio,
} from "@/utils/canvas/outpaint-frame";

const origin = { w: 576, h: 324 };

describe("框的预设", () => {
  test("倍数框以原图为中心等比放大", () => {
    const frame = frameByMult(origin, 1.5);
    expect(frameWidth(frame)).toBeCloseTo(864);
    expect(frameHeight(frame)).toBeCloseTo(486);
    expect(frame.x0).toBeCloseTo(-144);
    expect(frame.y0).toBeCloseTo(-81);
  });

  test("比例预设是刚好包住原图的最小框，原图居中", () => {
    // 21:9 比原图更宽：高度不变，宽度放大
    const wide = frameByRatio(origin, 21 / 9);
    expect(frameHeight(wide)).toBeCloseTo(324);
    expect(frameWidth(wide)).toBeCloseTo(756);
    expect(wide.x0).toBeCloseTo(-90);
    // 1:1 比原图更方：宽度不变，高度放大
    const square = frameByRatio(origin, 1);
    expect(frameWidth(square)).toBeCloseTo(576);
    expect(frameHeight(square)).toBeCloseTo(576);
    expect(square.y0).toBeCloseTo(-126);
  });

  test("最长边最多是原图最长边的 3 倍", () => {
    expect(frameCap(origin)).toBe(576 * MAX_MULT);
    expect(fitsCap(origin, frameByMult(origin, 3))).toBe(true);
    expect(fitsCap(origin, frameByMult(origin, 3.01))).toBe(false);
  });

  test("longMult 返回框最长边是原图最长边的几倍", () => {
    expect(longMult(origin, frameByMult(origin, 2))).toBeCloseTo(2);
  });
});

describe("识别当前比例", () => {
  test("原图比例优先，其次是预设，拖出来的自定义比例返回 null", () => {
    expect(detectRatio(origin, frameByMult(origin, 2))).toBe("orig");
    expect(detectRatio(origin, frameByRatio(origin, 1))).toBe("1:1");
    expect(detectRatio(origin, { x0: 0, y0: 0, x1: 700, y1: 400 })).toBeNull();
  });

  test("原图本身就是 1:1 时，1:1 算原比例", () => {
    const square = { w: 400, h: 400 };
    expect(detectRatio(square, frameByMult(square, 1.5))).toBe("orig");
  });
});

describe("moveFrame 平移", () => {
  const frame = frameByMult(origin, 1.5); // x0 -144, y0 -81

  test("平移后框仍然包住原图", () => {
    const right = moveFrame(origin, frame, 5000, 5000);
    expect(right.x0).toBeCloseTo(0);
    expect(right.y0).toBeCloseTo(0);
    const left = moveFrame(origin, frame, -5000, -5000);
    expect(left.x1).toBeCloseTo(origin.w);
    expect(left.y1).toBeCloseTo(origin.h);
  });

  test("平移不改变框的大小", () => {
    const moved = moveFrame(origin, frame, 30, -20);
    expect(frameWidth(moved)).toBeCloseTo(frameWidth(frame));
    expect(frameHeight(moved)).toBeCloseTo(frameHeight(frame));
  });

  test("框和原图一样大时没有可平移的余地", () => {
    const same = frameByMult(origin, 1);
    expect(moveFrame(origin, same, 40, 40)).toEqual(same);
  });
});

describe("resizeFrame 缩放手柄", () => {
  const start = frameByMult(origin, 1.5);

  test("边手柄自由改变一条边", () => {
    const { frame, limited } = resizeFrame("r", origin, start, 100, 0);
    expect(frame.x1).toBeCloseTo(start.x1 + 100);
    expect(frame.x0).toBeCloseTo(start.x0);
    expect(limited).toBe(false);
  });

  test("边手柄不能缩进原图里面", () => {
    const { frame } = resizeFrame("l", origin, start, 5000, 0);
    expect(frame.x0).toBeCloseTo(0);
    const bottom = resizeFrame("b", origin, start, 0, -5000);
    expect(bottom.frame.y1).toBeCloseTo(origin.h);
  });

  test("边手柄拖过 3 倍上限会被拦住并标记受限", () => {
    const { frame, limited } = resizeFrame("r", origin, start, 5000, 0);
    expect(frameWidth(frame)).toBeCloseTo(frameCap(origin));
    expect(limited).toBe(true);
  });

  test("角手柄等比，对角固定", () => {
    const { frame } = resizeFrame("tl", origin, start, -100, -100);
    expect(frameWidth(frame) / frameHeight(frame)).toBeCloseTo(
      frameWidth(start) / frameHeight(start),
    );
    expect(frame.x1).toBeCloseTo(start.x1);
    expect(frame.y1).toBeCloseTo(start.y1);
    expect(frameWidth(frame)).toBeGreaterThan(frameWidth(start));
  });

  test("角手柄往里拖最多缩到刚好包住原图", () => {
    const { frame } = resizeFrame("br", origin, start, -5000, -5000);
    expect(frame.x0).toBeLessThanOrEqual(0.01);
    expect(frame.y0).toBeLessThanOrEqual(0.01);
    expect(frame.x1).toBeGreaterThanOrEqual(origin.w - 0.01);
    expect(frame.y1).toBeGreaterThanOrEqual(origin.h - 0.01);
  });

  test("角手柄拖过 3 倍上限被拦住", () => {
    const { frame, limited } = resizeFrame("br", origin, start, 5000, 5000);
    expect(Math.max(frameWidth(frame), frameHeight(frame))).toBeLessThanOrEqual(
      frameCap(origin) + 0.5,
    );
    expect(limited).toBe(true);
  });
});

describe("toRatio 比例转整数比", () => {
  test("常见画幅", () => {
    expect(toRatio(1920 / 1080)).toBe("16:9");
    expect(toRatio(1600 / 1200)).toBe("4:3");
    expect(toRatio(1080 / 1920)).toBe("9:16");
    expect(toRatio(1)).toBe("1:1");
  });

  test("分子分母不超过 24，极扁的比例也能写成 24:1", () => {
    expect(toRatio(24)).toBe("24:1");
    expect(toRatio(100)).toBe("24:1");
  });

  test("拖出来的自定义比例取最接近的整数比，并列取和更小的", () => {
    expect(toRatio(1.375)).toBe("11:8");
    expect(toRatio(2)).toBe("2:1");
  });
});

describe("ratioOfFrame 写进模型的比例", () => {
  test("预设按名字原样写，不化简", () => {
    expect(ratioOfFrame(origin, frameByRatio(origin, 21 / 9))).toBe("21:9");
  });

  test("原比例按原图宽高比写", () => {
    expect(ratioOfFrame({ w: 1920, h: 1080 }, frameByMult({ w: 1920, h: 1080 }, 2))).toBe("16:9");
  });

  test("自定义比例取最接近的整数比", () => {
    expect(ratioOfFrame(origin, { x0: 0, y0: 0, x1: 700, y1: 400 })).toBe("7:4");
  });
});

describe("snapRatio 取模型支持的最接近比例", () => {
  test("在选项里就原样返回", () => {
    expect(snapRatio("16:9", ["1:1", "16:9"])).toBe("16:9");
  });

  test("不在选项里取数值上最接近的一项", () => {
    expect(snapRatio("7:3", ["1:1", "16:9", "21:9"])).toBe("21:9");
    expect(snapRatio("11:8", ["1:1", "16:9", "4:3"])).toBe("4:3");
  });

  test("跳过读不出比例的选项（Auto 等）", () => {
    expect(snapRatio("16:9", ["Auto", "1:1", "9:16"])).toBe("1:1");
  });

  test("没有可用选项时原样返回", () => {
    expect(snapRatio("16:9", [])).toBe("16:9");
    expect(snapRatio("16:9", ["Auto"])).toBe("16:9");
  });
});
