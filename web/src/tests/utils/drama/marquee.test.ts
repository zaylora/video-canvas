import { describe, expect, test } from "bun:test";

import {
  marqueeBox,
  marqueeHits,
  marqueeSelection,
  MARQUEE_THRESHOLD,
} from "@/utils/drama/marquee";

describe("marqueeBox：按下点和当前点围成的框", () => {
  test("往右下拖：左上角是按下点", () => {
    expect(marqueeBox({ x: 10, y: 20 }, { x: 110, y: 70 })).toEqual({
      left: 10,
      top: 20,
      width: 100,
      height: 50,
    });
  });

  test("往左上拖：框照样是正的宽高", () => {
    expect(marqueeBox({ x: 110, y: 70 }, { x: 10, y: 20 })).toEqual({
      left: 10,
      top: 20,
      width: 100,
      height: 50,
    });
  });

  test("移动没超过阈值不算框选（只是点了一下空白）", () => {
    expect(marqueeBox({ x: 0, y: 0 }, { x: MARQUEE_THRESHOLD - 1, y: 1 })).toBeNull();
  });
});

describe("marqueeHits：框碰到哪些卡片", () => {
  const cards = [
    { id: "a", rect: { left: 0, top: 0, width: 100, height: 130 } },
    { id: "b", rect: { left: 112, top: 0, width: 100, height: 130 } },
    { id: "c", rect: { left: 0, top: 150, width: 100, height: 130 } },
  ];

  test("碰到一角也算选中", () => {
    expect(marqueeHits({ left: 90, top: 120, width: 30, height: 40 }, cards)).toEqual([
      "a",
      "b",
      "c",
    ]);
  });

  test("只在卡片之间的空隙里：一个都没碰到", () => {
    expect(marqueeHits({ left: 101, top: 0, width: 10, height: 100 }, cards)).toEqual([]);
  });

  test("框住一整行", () => {
    expect(marqueeHits({ left: -5, top: 10, width: 300, height: 20 }, cards)).toEqual(["a", "b"]);
  });
});

describe("marqueeSelection：框选后的选中集合", () => {
  test("普通拖：只留框到的", () => {
    expect([...marqueeSelection(new Set(["x"]), ["a", "b"], false)]).toEqual(["a", "b"]);
  });

  test("按住 Shift / Ctrl 拖：在原来的选择上追加", () => {
    expect([...marqueeSelection(new Set(["x"]), ["a", "x"], true)].sort()).toEqual(["a", "x"]);
  });
});
