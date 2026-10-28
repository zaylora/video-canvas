import { describe, expect, test } from "bun:test";

import { FIXED_SIDE_COLLISION } from "@/components/ui/popup-position";

describe("FIXED_SIDE_COLLISION", () => {
  test("不翻到触发器的另一侧，也不换到垂直方向的另一轴", () => {
    expect(FIXED_SIDE_COLLISION.side).toBe("none");
    expect(FIXED_SIDE_COLLISION.fallbackAxisSide).toBe("none");
  });

  test("水平方向允许平移：窄屏时不会被推出屏幕", () => {
    expect(FIXED_SIDE_COLLISION.align).toBe("shift");
  });
});
