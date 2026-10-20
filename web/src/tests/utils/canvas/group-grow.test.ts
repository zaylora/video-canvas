import { describe, expect, test } from "bun:test";

import type { CanvasGroupNode, CanvasNode, FlowNode } from "@/types";
import { GROUP_GROW_GAP, growGroupsToFitMembers } from "@/utils/canvas/group-grow";
import { GROUP_PADDING } from "@/utils/canvas/group";

/**
 * 成员实际画出来的尺寸（measured）比后端排版时估的大（比如模型选了 Auto、成片是竖图），
 * 成员会从组框里伸出来。兜底：组框只往右、往下撑大，并把因此被压住的相邻对象推开。
 */

const node = (
  id: string,
  x: number,
  y: number,
  width = 576,
  height = 324,
  parentId?: string,
): CanvasNode => ({
  id,
  type: "canvas",
  position: { x, y },
  measured: { width, height },
  ...(parentId ? { parentId } : {}),
  data: { kind: "image", label: id },
});

const group = (
  id: string,
  x: number,
  y: number,
  width: number,
  height: number,
): CanvasGroupNode => ({
  id,
  type: "group",
  position: { x, y },
  width,
  height,
  data: { label: id },
});

const byId = (nodes: FlowNode[], id: string) => nodes.find((n) => n.id === id) as FlowNode;

/** 一个刚好框住一个 w×h 成员的组（左上留白 48/72，右下留白 48/48） */
const fitted = (id: string, x: number, y: number, w: number, h: number) =>
  group(id, x, y, w + GROUP_PADDING.x * 2, h + GROUP_PADDING.top + GROUP_PADDING.bottom);

describe("growGroupsToFitMembers：什么时候撑大", () => {
  test("成员都在组框里：原样返回同一个数组", () => {
    const nodes: FlowNode[] = [
      fitted("g", 0, 0, 576, 324),
      node("a", GROUP_PADDING.x, GROUP_PADDING.top, 576, 324, "g"),
    ];
    expect(growGroupsToFitMembers(nodes)).toBe(nodes);
  });

  test("没有成员的组、没有组的画布：原样返回", () => {
    const empty: FlowNode[] = [group("g", 0, 0, 200, 140), node("x", 500, 0)];
    expect(growGroupsToFitMembers(empty)).toBe(empty);
  });

  test("成员比估的高（竖图 324×576）：组框往下撑到刚好框住，宽度不变", () => {
    const nodes: FlowNode[] = [
      fitted("g", 0, 0, 324, 324), // 后端按 324 高估的
      node("a", GROUP_PADDING.x, GROUP_PADDING.top, 324, 576, "g"), // 实际画出来 576 高
    ];
    const out = growGroupsToFitMembers(nodes);
    const g = byId(out, "g") as CanvasGroupNode;
    expect(g.height).toBe(GROUP_PADDING.top + 576 + GROUP_PADDING.bottom);
    expect(g.width).toBe(324 + GROUP_PADDING.x * 2);
    // 位置和成员的相对坐标都不动
    expect(g.position).toEqual({ x: 0, y: 0 });
    expect(byId(out, "a").position).toEqual({ x: GROUP_PADDING.x, y: GROUP_PADDING.top });
  });

  test("成员比估的宽（21:9）：组框往右撑", () => {
    const nodes: FlowNode[] = [
      fitted("g", 0, 0, 576, 324),
      node("a", GROUP_PADDING.x, GROUP_PADDING.top, 756, 324, "g"),
    ];
    const g = byId(growGroupsToFitMembers(nodes), "g") as CanvasGroupNode;
    expect(g.width).toBe(756 + GROUP_PADDING.x * 2);
  });

  test("只撑大不缩小：组框比成员大得多时不动", () => {
    const nodes: FlowNode[] = [
      group("g", 0, 0, 2000, 1000),
      node("a", GROUP_PADDING.x, GROUP_PADDING.top, 324, 324, "g"),
    ];
    expect(growGroupsToFitMembers(nodes)).toBe(nodes);
  });

  test("不到 1 像素的差（测量取整）不算：不抖动", () => {
    const nodes: FlowNode[] = [
      fitted("g", 0, 0, 576, 324),
      node("a", GROUP_PADDING.x, GROUP_PADDING.top, 576.4, 324.4, "g"),
    ];
    expect(growGroupsToFitMembers(nodes)).toBe(nodes);
  });

  test("还没测量过的成员（没有 measured）按默认 576×324 算，不会误撑", () => {
    const unmeasured = { ...node("a", GROUP_PADDING.x, GROUP_PADDING.top, 576, 324, "g") };
    delete (unmeasured as { measured?: unknown }).measured;
    const nodes: FlowNode[] = [fitted("g", 0, 0, 576, 324), unmeasured];
    expect(growGroupsToFitMembers(nodes)).toBe(nodes);
  });
});

describe("growGroupsToFitMembers：推开被压住的对象", () => {
  const tall = (): FlowNode[] => [
    fitted("g", 0, 0, 324, 324),
    node("a", GROUP_PADDING.x, GROUP_PADDING.top, 324, 576, "g"),
  ];
  const gBottom = GROUP_PADDING.top + 576 + GROUP_PADDING.bottom; // 撑大后的组框下沿 696

  test("下方的相邻组被撑大的组压住：往下推到组框下沿再留一段间距", () => {
    const below = fitted("below", 0, 444 + 80, 324, 324); // 原本在 g(444 高) 下面隔 80
    const out = growGroupsToFitMembers([...tall(), below]);
    expect(byId(out, "below").position.y).toBe(gBottom + GROUP_GROW_GAP);
    expect(byId(out, "below").position.x).toBe(0);
  });

  test("右侧的相邻对象被撑宽的组压住：往右推", () => {
    const nodes: FlowNode[] = [
      fitted("g", 0, 0, 576, 324),
      node("a", GROUP_PADDING.x, GROUP_PADDING.top, 756, 324, "g"),
      node("right", 576 + 96 + 80, 0, 576, 324), // 原本在 g 右边隔 80
    ];
    const out = growGroupsToFitMembers(nodes);
    const g = byId(out, "g") as CanvasGroupNode;
    expect(byId(out, "right").position.x).toBe(g.position.x + (g.width ?? 0) + GROUP_GROW_GAP);
  });

  test("没被压住的对象一个都不动，哪怕就在旁边", () => {
    const far = node("far", 5000, 5000);
    const side = node("side", 2000, 0); // 在右边，但竖图只往下撑，碰不到
    const nodes: FlowNode[] = [...tall(), far, side];
    const out = growGroupsToFitMembers(nodes);
    expect(byId(out, "far")).toBe(far);
    expect(byId(out, "side")).toBe(side);
  });

  test("被推开的对象又压住了更远的对象：连锁往下推，最多推 3 层", () => {
    const l1 = fitted("l1", 0, 444 + 80, 324, 324); // 被 g 压住
    const l1Bottom = 444 + 80 + 72 + 324 + 48;
    const l2 = fitted("l2", 0, l1Bottom + 20, 324, 324); // 离 l1 只有 20，l1 下移后会压住它
    const l3 = fitted("l3", 0, l1Bottom + 20 + 444 + 20, 324, 324);
    const l4 = fitted("l4", 0, l1Bottom + 20 + 444 + 20 + 444 + 20, 324, 324);
    const out = growGroupsToFitMembers([...tall(), l1, l2, l3, l4]);
    const ys = ["l1", "l2", "l3", "l4"].map((id) => byId(out, id).position.y);
    // 前三层都被推过，彼此不重叠
    expect(ys[0]).toBeGreaterThanOrEqual(gBottom + GROUP_GROW_GAP);
    expect(ys[1]).toBeGreaterThanOrEqual(ys[0] + 444 + GROUP_GROW_GAP);
    expect(ys[2]).toBeGreaterThanOrEqual(ys[1] + 444 + GROUP_GROW_GAP);
    // 第 4 层超过连锁上限：本来就没被压住，保持原位
    expect(ys[3]).toBe(l4.position.y);
  });

  test("成员跟着它的组走：推开一个组，它的成员相对坐标不变", () => {
    const below = fitted("below", 0, 444 + 80, 324, 324);
    const member = node("m", GROUP_PADDING.x, GROUP_PADDING.top, 324, 324, "below");
    const out = growGroupsToFitMembers([...tall(), below, member]);
    expect(byId(out, "m").position).toEqual({ x: GROUP_PADDING.x, y: GROUP_PADDING.top });
  });

  test("不改入参", () => {
    const nodes = [...tall(), fitted("below", 0, 524, 324, 324)];
    const snapshot = JSON.stringify(nodes);
    growGroupsToFitMembers(nodes);
    expect(JSON.stringify(nodes)).toBe(snapshot);
  });
});
