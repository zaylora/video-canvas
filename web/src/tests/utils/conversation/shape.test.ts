import { describe, expect, test } from "bun:test";

import { cellAspect, ratioOf, shapeOfAspect } from "@/utils/conversation/shape";

describe("cellAspect：格子此刻该用的宽高比", () => {
  test("生成前按所选比例画框：4:3 就是 4/3，不会被归到 16:9", () => {
    expect(cellAspect({ aspect_ratio: "4:3" }, null)).toBeCloseTo(4 / 3);
    expect(cellAspect({ ratio: "9:16" }, null)).toBeCloseTo(9 / 16);
    expect(cellAspect({ aspect_ratio: "1:1" }, null)).toBe(1);
    expect(cellAspect({ aspect_ratio: "21:9" }, null)).toBeCloseTo(21 / 9);
  });

  test("量到成片的真实比例后以它为准", () => {
    expect(cellAspect({ aspect_ratio: "4:3" }, 1.5)).toBe(1.5);
    expect(cellAspect({}, 0.75)).toBe(0.75);
  });

  test("Auto、空、不认识的写法且没量到成片：返回 null，调用方按默认横版", () => {
    for (const value of ["Auto", "", undefined, null, "wide", "16x9"]) {
      expect(cellAspect({ aspect_ratio: value }, null)).toBeNull();
    }
    expect(cellAspect({}, null)).toBeNull();
  });
});

describe("ratioOf：不同模型的比例参数名", () => {
  test("aspect_ratio 优先，其次 ratio，都没有是 undefined", () => {
    expect(ratioOf({ aspect_ratio: "9:16", ratio: "1:1" })).toBe("9:16");
    expect(ratioOf({ ratio: "1:1" })).toBe("1:1");
    expect(ratioOf({})).toBeUndefined();
  });
});

describe("shapeOfAspect：量到的真实宽高比 → 格子画幅", () => {
  test("横图是横版，竖图是竖版", () => {
    expect(shapeOfAspect(16 / 9)).toBe("wide");
    expect(shapeOfAspect(4 / 3)).toBe("wide");
    expect(shapeOfAspect(9 / 16)).toBe("tall");
  });

  test("接近正方形（误差 2% 以内）算方形，1024×1008 这类不会被当成横图", () => {
    expect(shapeOfAspect(1)).toBe("square");
    expect(shapeOfAspect(1024 / 1008)).toBe("square");
    expect(shapeOfAspect(1008 / 1024)).toBe("square");
  });

  test("无效值按横版", () => {
    for (const value of [0, -1, Number.NaN, Number.POSITIVE_INFINITY]) {
      expect(shapeOfAspect(value)).toBe("wide");
    }
  });
});
