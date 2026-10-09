import { describe, expect, test } from "bun:test";

import {
  addSlot,
  collapsedWidth,
  expandedWidth,
  refSlot,
  STACK_MAX,
} from "@/utils/conversation/ref-layout";

const SIZE = { w: 56, h: 72 };

describe("refSlot：参考图的摆放", () => {
  test("收起：叠成一摞，只露前 3 张，后加的在上面", () => {
    const slots = [0, 1, 2, 3, 4].map((i) => refSlot(i, false, SIZE));
    expect(slots.slice(0, STACK_MAX).every((slot) => slot.opacity === 1)).toBe(true);
    expect(slots.slice(STACK_MAX).every((slot) => slot.opacity === 0)).toBe(true);
    expect(slots.map((slot) => slot.z)).toEqual([0, 1, 2, 3, 4]);
    // 三张各有不同的角度和错位
    expect(new Set(slots.slice(0, 3).map((slot) => slot.rotate)).size).toBe(3);
    expect(slots[1].x).toBeGreaterThan(slots[0].x);
    // 第 4、5 张藏在第 3 张的位置
    expect(slots[3].x).toBe(slots[2].x);
  });

  test("展开：一字排开，间距固定，全部可见", () => {
    const slots = [0, 1, 2].map((i) => refSlot(i, true, SIZE));
    expect(slots.map((slot) => slot.x)).toEqual([0, 66, 132]);
    expect(slots.every((slot) => slot.opacity === 1 && slot.y === 0)).toBe(true);
  });
});

describe("collapsedWidth：收起时占的宽度", () => {
  test("0、1 张一样宽；张数越多越宽，3 张封顶", () => {
    expect(collapsedWidth(0, SIZE)).toBe(56);
    expect(collapsedWidth(1, SIZE)).toBe(56);
    expect(collapsedWidth(2, SIZE)).toBeGreaterThan(56);
    expect(collapsedWidth(3, SIZE)).toBeGreaterThan(collapsedWidth(2, SIZE));
    expect(collapsedWidth(8, SIZE)).toBe(collapsedWidth(3, SIZE));
  });
});

describe("expandedWidth：展开后整行的宽度", () => {
  test("每张加最后的上传块，再加上它们之间的间隙", () => {
    expect(expandedWidth(1, SIZE)).toBe(2 * 56 + 10);
    expect(expandedWidth(3, SIZE)).toBe(4 * 56 + 3 * 10);
  });

  test("刚好盖住上传块的右边缘：最后一块的位置 + 宽度", () => {
    const tile = addSlot(3, true, SIZE);
    expect(expandedWidth(3, SIZE)).toBe(tile.x + tile.w);
  });
});

describe("addSlot：加号按钮", () => {
  test("没有参考图：整块上传块，在最左", () => {
    const slot = addSlot(0, false, SIZE);
    expect(slot).toMatchObject({ tile: true, x: 0, w: 56, h: 72 });
  });

  test("有参考图且收起：缩成压在右下角的小圆钮", () => {
    const slot = addSlot(2, false, SIZE);
    expect(slot.tile).toBe(false);
    expect(slot.w).toBe(slot.h);
    expect(slot.w).toBeGreaterThanOrEqual(30);
    expect(slot.w).toBeLessThan(40);
    expect(slot.radius).toBe(slot.w / 2);
    expect(slot.y).toBeGreaterThan(40);
  });

  test("有参考图且展开：变回整块上传块，排在最后一张后面", () => {
    const slot = addSlot(3, true, SIZE);
    expect(slot).toMatchObject({ tile: true, x: 3 * 66, w: 56, h: 72 });
  });
});
