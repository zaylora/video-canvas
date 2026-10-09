import { describe, expect, test } from "bun:test";

import { ratioOf, shapeOf } from "@/utils/conversation/shape";

describe("shapeOf：比例 → 格子画幅", () => {
  test("宽大于高是横版，高大于宽是竖版，相等是方形", () => {
    expect(shapeOf("16:9")).toBe("wide");
    expect(shapeOf("21:9")).toBe("wide");
    expect(shapeOf("9:16")).toBe("tall");
    expect(shapeOf("3:4")).toBe("tall");
    expect(shapeOf("1:1")).toBe("square");
  });

  test("Auto、空、不认识的写法按横版", () => {
    for (const value of ["Auto", "", undefined, null, "wide", "16x9"]) {
      expect(shapeOf(value)).toBe("wide");
    }
  });
});

describe("ratioOf：不同模型的比例参数名", () => {
  test("aspect_ratio 优先，其次 ratio，都没有是 undefined", () => {
    expect(ratioOf({ aspect_ratio: "9:16", ratio: "1:1" })).toBe("9:16");
    expect(ratioOf({ ratio: "1:1" })).toBe("1:1");
    expect(ratioOf({})).toBeUndefined();
  });
});
