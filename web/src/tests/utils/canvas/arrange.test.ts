import { describe, expect, test } from "bun:test";

import type { CanvasNode } from "@/types";
import { ARRANGE_GAP, arrangeNodes } from "@/utils/canvas/arrange";

const node = (id: string, x: number, y: number, width = 100, height = 50): CanvasNode => ({
  id,
  type: "canvas",
  position: { x, y },
  measured: { width, height },
  data: { kind: "image", label: id },
});

describe("arrangeNodes", () => {
  const nodes = [node("b", 500, 10), node("a", 20, 300), node("c", 900, 0, 200, 80)];

  test("横排：按 x 先后，从选区左上角起排，间距固定", () => {
    const out = arrangeNodes(nodes, "row");
    expect(out.get("a")).toEqual({ x: 20, y: 0 });
    expect(out.get("b")).toEqual({ x: 20 + 100 + ARRANGE_GAP.x, y: 0 });
    expect(out.get("c")).toEqual({ x: 20 + 2 * (100 + ARRANGE_GAP.x), y: 0 });
  });

  test("竖排：按 y 先后", () => {
    const out = arrangeNodes(nodes, "column");
    expect([...out.entries()].sort((p, q) => p[1].y - q[1].y).map(([id]) => id)).toEqual([
      "c",
      "b",
      "a",
    ]);
    expect(out.get("b")).toEqual({ x: 20, y: 80 + ARRANGE_GAP.y });
  });

  test("网格：列宽取该列最宽的节点", () => {
    const four = [node("1", 0, 0, 300), node("2", 10, 0), node("3", 0, 100), node("4", 10, 100)];
    const out = arrangeNodes(four, "grid");
    expect(out.get("1")).toEqual({ x: 0, y: 0 });
    expect(out.get("2")).toEqual({ x: 300 + ARRANGE_GAP.x, y: 0 });
    expect(out.get("3")).toEqual({ x: 0, y: 50 + ARRANGE_GAP.y });
  });

  test("空选区返回空表", () => {
    expect(arrangeNodes([], "grid").size).toBe(0);
  });
});
