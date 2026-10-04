import { describe, expect, test } from "bun:test";

import type { CanvasGroupNode, CanvasNode, FlowNode } from "@/types";
import { deserializeGraph, serializeGraph } from "@/utils/canvas/canvas-persistence";

const viewport = { x: 0, y: 0, zoom: 1 };

const member: CanvasNode = {
  id: "a",
  type: "canvas",
  parentId: "g",
  position: { x: 24, y: 24 },
  data: { kind: "image", label: "A" },
};
const group: CanvasGroupNode = {
  id: "g",
  type: "group",
  position: { x: 100, y: 100 },
  width: 448,
  height: 300,
  data: { label: "第一场", color: "blue", labelColor: "orange" },
};

describe("组的持久化", () => {
  test("序列化组：类型、尺寸、名字、两种颜色都写下来", () => {
    const graph = serializeGraph([group, member], [], viewport);
    expect(graph.nodes[0]).toEqual({
      id: "g",
      type: "group",
      position: { x: 100, y: 100 },
      width: 448,
      height: 300,
      data: { label: "第一场", color: "blue", labelColor: "orange" },
    });
  });

  test("没设颜色的组不写颜色字段", () => {
    const plain: CanvasGroupNode = { ...group, data: { label: "组" } };
    const [saved] = serializeGraph([plain], [], viewport).nodes;
    expect(saved.data).toEqual({ label: "组" });
  });

  test("成员带上 parentId，没有父的节点不写这个字段", () => {
    const loose: CanvasNode = { ...member, id: "b", parentId: undefined };
    const graph = serializeGraph([group, member, loose], [], viewport);
    expect(graph.nodes[1]).toMatchObject({ id: "a", parentId: "g", position: { x: 24, y: 24 } });
    expect("parentId" in graph.nodes[2]).toBe(false);
  });

  test("组的尺寸取节点上的 width / height，没有就取测量值", () => {
    const measured: CanvasGroupNode = {
      ...group,
      width: undefined,
      height: undefined,
      measured: { width: 500, height: 320 },
    };
    const [saved] = serializeGraph([measured], [], viewport).nodes;
    expect(saved).toMatchObject({ width: 500, height: 320 });
  });

  test("往返一圈：组仍在成员之前，成员仍指向组", () => {
    const saved = serializeGraph([group, member], [], viewport);
    const loaded = deserializeGraph(saved).nodes as FlowNode[];
    expect(loaded.map((n) => n.id)).toEqual(["g", "a"]);
    expect(loaded[0]).toMatchObject({ type: "group", width: 448, height: 300 });
    expect(loaded[1].parentId).toBe("g");
  });

  test("读档时组在成员后面也会被排到前面；成员指向不存在的组会被清掉", () => {
    const saved = serializeGraph([member, group], [], viewport);
    const loaded = deserializeGraph(saved).nodes as FlowNode[];
    expect(loaded.map((n) => n.id)).toEqual(["g", "a"]);

    const orphan = deserializeGraph(serializeGraph([member], [], viewport)).nodes as FlowNode[];
    expect(orphan[0].parentId).toBeUndefined();
  });

  test("旧画布（没有组）读出来原样不变", () => {
    const old = { ...member, parentId: undefined };
    const loaded = deserializeGraph(serializeGraph([old], [], viewport)).nodes as FlowNode[];
    expect(loaded).toHaveLength(1);
    expect(loaded[0].type).toBe("canvas");
  });
});
