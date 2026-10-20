import { describe, expect, test } from "bun:test";

import type { CanvasNode, FlowNode } from "@/types";
import { AVOID_GAP, NODE_GAP_X } from "@/utils/canvas/place-beside";
import { placeGroup } from "@/utils/canvas/place-group";

import { makeData } from "../tasks/fixtures";

const node = (id: string, x: number, y: number, w = 576, h = 324): CanvasNode => ({
  id,
  type: "canvas",
  position: { x, y },
  measured: { width: w, height: h },
  data: makeData({ kind: "image", label: id }),
});

const source = node("src", 100, 200);
const size = (width: number, height: number) => ({ width, height });

describe("placeGroup 把一组节点按行列摆到源节点右侧", () => {
  test("整组在源节点右侧 80px 外、垂直居中，同行顶对齐", () => {
    const sizes = [size(324, 324), size(324, 324), size(324, 324), size(324, 324)];
    const positions = placeGroup({
      source,
      nodes: [source] as FlowNode[],
      rows: 2,
      cols: 2,
      sizes,
    });
    const left = 100 + 576 + NODE_GAP_X;
    // 整组高 324 * 2 + 40，按源节点中线居中
    const top = 200 + 324 / 2 - (324 * 2 + AVOID_GAP) / 2;
    expect(positions).toEqual([
      { x: left, y: top },
      { x: left + 324 + NODE_GAP_X, y: top },
      { x: left, y: top + 324 + AVOID_GAP },
      { x: left + 324 + NODE_GAP_X, y: top + 324 + AVOID_GAP },
    ]);
  });

  test("行高取该行最高的，列宽取该列最宽的；同一行顶对齐、同一列左对齐", () => {
    // 第 1 行：宽矮 + 窄高；第 2 行：窄高 + 宽矮
    const sizes = [size(500, 200), size(200, 400), size(200, 300), size(500, 100)];
    const positions = placeGroup({
      source,
      nodes: [source] as FlowNode[],
      rows: 2,
      cols: 2,
      sizes,
    });
    const left = 100 + 576 + NODE_GAP_X;
    // 列宽：max(500, 200) = 500；行高：max(200, 400) = 400
    expect(positions[0].x).toBe(left);
    expect(positions[1].x).toBe(left + 500 + NODE_GAP_X);
    expect(positions[2].x).toBe(left);
    expect(positions[3].x).toBe(left + 500 + NODE_GAP_X);
    expect(positions[0].y).toBe(positions[1].y);
    expect(positions[2].y).toBe(positions[3].y);
    expect(positions[2].y).toBe(positions[0].y + 400 + AVOID_GAP);
  });

  test("摆不进去时整组一起往下错开，刚好躲开阻挡节点并留出间隔", () => {
    const blocker = node("b", 756, 100, 324, 324);
    const positions = placeGroup({
      source,
      nodes: [source, blocker] as FlowNode[],
      rows: 1,
      cols: 2,
      sizes: [size(324, 324), size(324, 324)],
    });
    // 居中的 y 是 200，和 blocker（100 到 424）冲突，下移到 424 + 间隔
    expect(positions[0].y).toBe(100 + 324 + AVOID_GAP);
    expect(positions[1].y).toBe(positions[0].y);
    expect(positions[0].x).toBe(756);
  });

  test("阻挡的是第二列时也整组下移，不会只挪一部分", () => {
    const blocker = node("b", 756 + 324 + NODE_GAP_X, 150, 324, 100);
    const positions = placeGroup({
      source,
      nodes: [source, blocker] as FlowNode[],
      rows: 1,
      cols: 2,
      sizes: [size(324, 324), size(324, 324)],
    });
    expect(positions[0].y).toBe(150 + 100 + AVOID_GAP);
    expect(positions[1].y).toBe(positions[0].y);
  });

  test("下面又有一个阻挡时继续往下找", () => {
    const blockers = [node("b1", 756, 100, 324, 324), node("b2", 756, 464 + 100, 324, 324)];
    const positions = placeGroup({
      source,
      nodes: [source, ...blockers] as FlowNode[],
      rows: 1,
      cols: 1,
      sizes: [size(324, 324)],
    });
    // b1 底 424 + 40 = 464，此处 464 到 788 撞 b2（564 起），再下移到 b2 底 888 + 40
    expect(positions[0].y).toBe(564 + 324 + AVOID_GAP);
  });

  test("没有任何冲突时不挪", () => {
    const far = node("far", 5000, 5000);
    const positions = placeGroup({
      source,
      nodes: [source, far] as FlowNode[],
      rows: 1,
      cols: 1,
      sizes: [size(324, 324)],
    });
    expect(positions[0]).toEqual({ x: 756, y: 200 });
  });

  test("源节点在分组里时按绝对位置摆", () => {
    const group: FlowNode = {
      id: "g",
      type: "group",
      position: { x: 1000, y: 1000 },
      data: makeData({ kind: "image", label: "组" }),
    } as FlowNode;
    const member: CanvasNode = { ...node("m", 10, 20), parentId: "g" };
    const positions = placeGroup({
      source: member,
      nodes: [group, member] as FlowNode[],
      rows: 1,
      cols: 1,
      sizes: [size(324, 324)],
    });
    expect(positions[0].x).toBe(1010 + 576 + NODE_GAP_X);
  });

  test("没有格子返回空", () => {
    expect(
      placeGroup({ source, nodes: [source] as FlowNode[], rows: 0, cols: 0, sizes: [] }),
    ).toEqual([]);
  });
});
