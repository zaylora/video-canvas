import { describe, expect, test } from "bun:test";

import {
  PROP_CATEGORIES,
  PROP_PRESETS,
  PROP_SHAPES,
  propLabel,
  propSize,
  shapesOf,
} from "@/utils/director/prop-presets";

describe("道具预设清单", () => {
  test("几何体 4 个、物体 10 个，每个都有中文名和默认尺寸", () => {
    expect(shapesOf("geometry")).toEqual(["box", "cylinder", "sphere", "plane"]);
    expect(shapesOf("object")).toHaveLength(10);
    for (const shape of PROP_SHAPES) {
      expect(propLabel(shape).length).toBeGreaterThan(0);
      const [w, h, d] = propSize(shape);
      expect(w).toBeGreaterThan(0);
      expect(h).toBeGreaterThan(0);
      expect(d).toBeGreaterThan(0);
    }
  });

  test("两个分类：几何体、物体", () => {
    expect(PROP_CATEGORIES.map((c) => c.id)).toEqual(["geometry", "object"]);
    expect(PROP_CATEGORIES.map((c) => c.label)).toEqual(["几何体", "物体"]);
  });

  test("名字不重复，方便按名字搜索", () => {
    const labels = PROP_SHAPES.map(propLabel);
    expect(new Set(labels).size).toBe(labels.length);
  });

  test("默认尺寸是真实世界里的米：桌子约 0.75 米高，门约 2 米高，汽车约 4 米长", () => {
    expect(propSize("table")[1]).toBeCloseTo(0.75, 1);
    expect(propSize("door")[1]).toBeGreaterThanOrEqual(2);
    expect(propSize("car")[2]).toBeGreaterThan(3.5);
    expect(propSize("tree")[1]).toBeGreaterThan(3);
  });
});

describe("拼装零件", () => {
  test("几何体没有零件（用自己的几何体），物体至少有两个零件", () => {
    for (const shape of shapesOf("geometry")) expect(PROP_PRESETS[shape].parts).toBeUndefined();
    for (const shape of shapesOf("object")) {
      expect(PROP_PRESETS[shape].parts?.length).toBeGreaterThanOrEqual(2);
    }
  });

  test("零件都在单位立方体里：水平居中、y 从 0 到 1，没有悬空穿出去的", () => {
    for (const shape of shapesOf("object")) {
      for (const part of PROP_PRESETS[shape].parts!) {
        const [cx, cy, cz] = part.center;
        const [sx, sy, sz] = part.size;
        const where = `${shape}:${part.kind}`;
        expect(cx - sx / 2, where).toBeGreaterThanOrEqual(-0.5 - 1e-9);
        expect(cx + sx / 2, where).toBeLessThanOrEqual(0.5 + 1e-9);
        expect(cz - sz / 2, where).toBeGreaterThanOrEqual(-0.5 - 1e-9);
        expect(cz + sz / 2, where).toBeLessThanOrEqual(0.5 + 1e-9);
        expect(cy - sy / 2, where).toBeGreaterThanOrEqual(-1e-9);
        expect(cy + sy / 2, where).toBeLessThanOrEqual(1 + 1e-9);
      }
    }
  });

  test("每个物体都贴地（有零件的底面在 y = 0）、顶到 y = 1（占满自己的高度）", () => {
    for (const shape of shapesOf("object")) {
      const parts = PROP_PRESETS[shape].parts!;
      const bottom = Math.min(...parts.map((p) => p.center[1] - p.size[1] / 2));
      const top = Math.max(...parts.map((p) => p.center[1] + p.size[1] / 2));
      expect(bottom, shape).toBeCloseTo(0, 6);
      expect(top, shape).toBeCloseTo(1, 6);
    }
  });
});
