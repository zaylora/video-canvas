import { describe, expect, test } from "bun:test";

import { ANIMATED_EDGE_OPTIONS } from "@/constants/canvas";
import type { CanvasNode, FlowNode } from "@/types";
import { evenLines, planTiles } from "@/utils/canvas/grid-split";
import { buildGridSplitNodes, gridTileFileName } from "@/utils/canvas/grid-split-node";
import { nodeFrameSize } from "@/utils/canvas/node-frame";
import { AVOID_GAP, NODE_GAP_X } from "@/utils/canvas/place-beside";

import { makeData } from "../tasks/fixtures";

const source: CanvasNode = {
  id: "src",
  type: "canvas",
  position: { x: 100, y: 200 },
  measured: { width: 576, height: 324 },
  data: makeData({ kind: "image", label: "图片", status: "done", src: "/files/a.png" }),
};

let counter = 0;
const build = (rows: number, cols: number, over: object = {}) =>
  buildGridSplitNodes({
    source,
    nodes: [source] as FlowNode[],
    tiles: planTiles(evenLines(cols), evenLines(rows), 1600, 900),
    rows,
    cols,
    newId: () => `id${++counter}`,
    ...over,
  });

describe("gridTileFileName 文件名", () => {
  test("grid-行-列.png，从 1 起", () => {
    expect(gridTileFileName(0, 0)).toBe("grid-1-1.png");
    expect(gridTileFileName(1, 2)).toBe("grid-2-3.png");
  });
});

describe("buildGridSplitNodes 造切分节点和连线", () => {
  test("每格一个上传中的图片节点，没有生成字段", () => {
    const { nodes } = build(2, 2);
    expect(nodes).toHaveLength(4);
    for (const node of nodes) {
      expect(node.data).toMatchObject({
        kind: "image",
        status: "running",
        uploadProgress: 0,
        mediaType: "image",
        uploaded: true,
      });
      expect(node.data.taskId).toBeUndefined();
      expect(node.data.prompt).toBeUndefined();
    }
    expect(nodes.map((node) => node.data.fileName)).toEqual([
      "grid-1-1.png",
      "grid-1-2.png",
      "grid-2-1.png",
      "grid-2-2.png",
    ]);
  });

  test("节点记下该格的宽高比，大小用 nodeFrameSize", () => {
    // 1600×900 切 1×2：每格 800×900
    const { nodes } = build(1, 2);
    expect(nodes[0].data.mediaAspect).toBeCloseTo(800 / 900);
    expect([nodes[0].width, nodes[0].height]).toEqual([
      nodeFrameSize(800 / 900).width,
      nodeFrameSize(800 / 900).height,
    ]);
  });

  test("拖过线、格子不等大时各格按自己的比例定大小", () => {
    const tiles = planTiles([0.25], [], 1000, 500);
    const { nodes } = build(1, 2, { tiles });
    expect(nodes[0].data.mediaAspect).toBeCloseTo(250 / 500);
    expect(nodes[1].data.mediaAspect).toBeCloseTo(750 / 500);
    expect(nodes[0].height).toBe(nodeFrameSize(0.5).height);
    expect(nodes[1].width).toBe(nodeFrameSize(1.5).width);
  });

  test("标题是「源标题 行-列」", () => {
    const { nodes } = build(2, 2);
    expect(nodes.map((node) => node.data.label)).toEqual([
      "图片 1-1",
      "图片 1-2",
      "图片 2-1",
      "图片 2-2",
    ]);
  });

  test("源标题本身带数字时不做特殊处理", () => {
    const named: CanvasNode = { ...source, data: { ...source.data, label: "图片 2" } };
    const { nodes } = build(1, 2, { source: named, nodes: [named] as FlowNode[] });
    expect(nodes[0].data.label).toBe("图片 2 1-1");
  });

  test("对同一张图再切一次，标题自动去重", () => {
    const first = build(1, 2);
    const again = build(1, 2, { nodes: [source, ...first.nodes] as FlowNode[] });
    expect(again.nodes.map((node) => node.data.label)).toEqual(["图片 1-1 2", "图片 1-2 2"]);
  });

  test("源标题很长时截短，后缀不丢", () => {
    const long: CanvasNode = { ...source, data: { ...source.data, label: "字".repeat(40) } };
    const { nodes } = build(5, 5, { source: long, nodes: [long] as FlowNode[] });
    expect(nodes[24].data.label.endsWith(" 5-5")).toBe(true);
    expect([...nodes[24].data.label].length).toBeLessThanOrEqual(40);
  });

  test("每格一条从源节点出来的普通连线", () => {
    const { nodes, edges } = build(2, 3);
    expect(edges).toHaveLength(6);
    expect(edges.map((edge) => edge.source)).toEqual(Array(6).fill("src"));
    expect(edges.map((edge) => edge.target)).toEqual(nodes.map((node) => node.id));
    for (const edge of edges) {
      expect(edge.type).toBe(ANIMATED_EDGE_OPTIONS.type);
      expect(edge.sourceHandle).toBeNull();
      expect(edge.targetHandle).toBeNull();
    }
    const ids = [...nodes.map((node) => node.id), ...edges.map((edge) => edge.id)];
    expect(new Set(ids).size).toBe(ids.length);
  });

  test("在源节点右侧按行列排成网格，同行顶对齐、相邻留出间距", () => {
    const { nodes } = build(2, 2);
    const [a, b, c, d] = nodes;
    expect(a.position.x).toBe(100 + 576 + NODE_GAP_X);
    expect(a.position.y).toBe(b.position.y);
    expect(c.position.y).toBe(d.position.y);
    expect(a.position.x).toBe(c.position.x);
    expect(b.position.x).toBe(a.position.x + (a.width ?? 0) + NODE_GAP_X);
    expect(c.position.y).toBe(a.position.y + (a.height ?? 0) + AVOID_GAP);
  });

  test("撞到已有节点时整组往下错开，不压到它", () => {
    const blocker: CanvasNode = {
      ...source,
      id: "blocker",
      position: { x: 756, y: 0 },
      measured: { width: 600, height: 1200 },
    };
    const { nodes } = build(2, 2, { nodes: [source, blocker] as FlowNode[] });
    for (const node of nodes) expect(node.position.y).toBeGreaterThanOrEqual(1200 + AVOID_GAP);
  });

  test("节点互不重叠（5×5）", () => {
    const { nodes } = build(5, 5);
    for (let i = 0; i < nodes.length; i++) {
      for (let j = i + 1; j < nodes.length; j++) {
        const a = nodes[i];
        const b = nodes[j];
        const apart =
          a.position.x + (a.width ?? 0) <= b.position.x ||
          b.position.x + (b.width ?? 0) <= a.position.x ||
          a.position.y + (a.height ?? 0) <= b.position.y ||
          b.position.y + (b.height ?? 0) <= a.position.y;
        expect(apart).toBe(true);
      }
    }
  });

  test("没有格子什么也不建", () => {
    expect(build(1, 1, { tiles: [] })).toEqual({ nodes: [], edges: [] });
  });
});
