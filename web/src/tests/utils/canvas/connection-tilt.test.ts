import { describe, expect, test } from "bun:test";

import { tiltPerspective } from "@/utils/canvas/connection-tilt";

describe("tiltPerspective：节点只在倾斜时才带透视", () => {
  const rest = { rotateX: 0, rotateY: 0, scale: 1 };

  test("静止（没倾斜、没缩放）时不带透视，节点不会被提升成 3D 合成层", () => {
    expect(tiltPerspective(rest, 900)).toBe(0);
  });

  test("绕 X 轴倾斜时带透视", () => {
    expect(tiltPerspective({ ...rest, rotateX: -3 }, 900)).toBe(900);
  });

  test("绕 Y 轴倾斜时带透视", () => {
    expect(tiltPerspective({ ...rest, rotateY: 0.01 }, 900)).toBe(900);
  });

  test("只是沉下去缩了一点也带透视，回弹过程不会突然变平", () => {
    expect(tiltPerspective({ ...rest, scale: 0.99 }, 900)).toBe(900);
  });
});
