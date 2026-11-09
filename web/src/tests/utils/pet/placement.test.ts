import { describe, expect, test } from "bun:test";

import {
  DEFAULT_ANCHOR,
  PANEL_GAP,
  PET_MARGIN,
  anchorOf,
  panelSize,
  placePanel,
  rectOf,
  type Rect,
} from "@/utils/pet/placement";

const area = { width: 1200, height: 800 };
const size = { width: 80, height: 87 };
const petAt = (x: number, y: number): Rect => ({ x, y, ...size });
const want = { width: 400, height: 600 };

describe("panelSize", () => {
  test("宽 400，高取 600 与区域高度减 24 的较小者", () => {
    expect(panelSize({ width: 1200, height: 800 })).toEqual({ width: 400, height: 600 });
    expect(panelSize({ width: 1200, height: 500 })).toEqual({ width: 400, height: 476 });
  });
});

describe("placePanel", () => {
  test("上方放得下：放上方，间距 8px，右半边右对齐", () => {
    const pet = petAt(1100, 700);
    const r = placePanel(pet, area, want);
    expect(r.y).toBe(pet.y - PANEL_GAP - want.height);
    expect(r.x + want.width).toBe(pet.x + pet.width);
    expect(r.origin).toBe("bottom right");
  });

  test("在左半边：左边缘对齐桌宠", () => {
    const pet = petAt(40, 700);
    const r = placePanel(pet, area, want);
    expect(r.x).toBe(40);
    expect(r.y).toBe(700 - PANEL_GAP - 600);
    expect(r.origin).toBe("bottom left");
  });

  test("上方放不下：放下方", () => {
    const pet = petAt(40, 20);
    const r = placePanel(pet, area, want);
    expect(r.y).toBe(pet.y + pet.height + PANEL_GAP);
    expect(r.origin).toBe("top left");
  });

  test("上下都放不下：放到左右空间更大的一侧", () => {
    const short = { width: 1200, height: 500 };
    // 桌宠在左边：右侧空间大，面板放右边
    const left = placePanel(petAt(40, 200), short, want);
    expect(left.x).toBe(40 + size.width + PANEL_GAP);
    expect(left.origin).toBe("center left");
    // 桌宠在右边：面板放左边
    const right = placePanel(petAt(1100, 200), short, want);
    expect(right.x).toBe(1100 - PANEL_GAP - want.width);
    expect(right.origin).toBe("center right");
  });

  test("结果总是夹回区域内，留 12px 边距", () => {
    const cases = [petAt(0, 0), petAt(1120, 713), petAt(600, 400), petAt(0, 713), petAt(1120, 0)];
    for (const pet of cases) {
      const r = placePanel(pet, area, want);
      expect(r.x).toBeGreaterThanOrEqual(PET_MARGIN);
      expect(r.y).toBeGreaterThanOrEqual(PET_MARGIN);
      expect(r.x + want.width).toBeLessThanOrEqual(area.width - PET_MARGIN);
      expect(r.y + want.height).toBeLessThanOrEqual(area.height - PET_MARGIN);
    }
  });

  test("桌宠贴着左边缘、面板左对齐会越界时夹回", () => {
    const r = placePanel(petAt(0, 700), area, want);
    expect(r.x).toBe(PET_MARGIN);
  });

  test("区域比面板还小：贴边距左上角", () => {
    const r = placePanel(petAt(10, 10), { width: 300, height: 300 }, want);
    expect(r.x).toBe(PET_MARGIN);
    expect(r.y).toBe(PET_MARGIN);
  });
});

describe("anchorOf / rectOf", () => {
  test("选最近的角，偏移是桌宠到该角两条边的距离", () => {
    expect(anchorOf(petAt(1100, 700), area)).toEqual({ corner: "br", dx: 20, dy: 13 });
    expect(anchorOf(petAt(30, 40), area)).toEqual({ corner: "tl", dx: 30, dy: 40 });
    expect(anchorOf(petAt(30, 700), area)).toEqual({ corner: "bl", dx: 30, dy: 13 });
    expect(anchorOf(petAt(1000, 50), area)).toEqual({ corner: "tr", dx: 120, dy: 50 });
  });

  test("正中间落在右下角", () => {
    const a = anchorOf(petAt(560, 360), area);
    expect(a.corner).toBe("br");
  });

  test("互逆：区域内的位置换算后原样回来", () => {
    const rects = [
      petAt(1100, 700),
      petAt(30, 40),
      petAt(30, 700),
      petAt(1000, 50),
      petAt(500, 300),
    ];
    for (const rect of rects) {
      expect(rectOf(anchorOf(rect, area), area, size)).toEqual(rect);
    }
  });

  test("位置在边距以外时，先夹到边距再记录", () => {
    const a = anchorOf(petAt(-50, 900), area);
    expect(a.dx).toBe(PET_MARGIN);
    expect(a.dy).toBe(PET_MARGIN);
  });

  test("区域缩小：按锚定的角重算位置并夹回区域内", () => {
    const a = anchorOf(petAt(1100, 700), area); // br, dx 20, dy 13
    const small = { width: 500, height: 400 };
    expect(rectOf(a, small, size)).toEqual({ x: 500 - 20 - 80, y: 400 - 13 - 87, ...size });
    // 区域比桌宠加边距还小时，贴左上边距
    const tiny = { width: 60, height: 60 };
    const r = rectOf(a, tiny, size);
    expect(r.x).toBe(PET_MARGIN);
    expect(r.y).toBe(PET_MARGIN);
  });

  test("左上锚点在区域缩小后仍在边距内", () => {
    const r = rectOf({ corner: "tl", dx: 700, dy: 500 }, { width: 400, height: 300 }, size);
    expect(r.x).toBe(400 - PET_MARGIN - size.width);
    expect(r.y).toBe(300 - PET_MARGIN - size.height);
  });

  test("默认锚点：右下角，留边距", () => {
    expect(DEFAULT_ANCHOR).toEqual({ corner: "br", dx: PET_MARGIN, dy: PET_MARGIN });
    expect(rectOf(DEFAULT_ANCHOR, area, size)).toEqual({
      x: 1200 - PET_MARGIN - 80,
      y: 800 - PET_MARGIN - 87,
      ...size,
    });
  });
});
