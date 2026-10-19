import { describe, expect, test } from "bun:test";

import type { CanvasNode, CanvasNodeData } from "@/types";
import { deserializeGraph, serializeGraph } from "@/utils/canvas/canvas-persistence";
import { restoreNodes } from "@/utils/canvas/history";
import { addProp, createDefaultScene } from "@/utils/director/scene";

const viewport = { x: 0, y: 0, zoom: 1 };

const ids = () => {
  let n = 0;
  return () => `id${++n}`;
};

const director = (data: Partial<CanvasNodeData> = {}, x = 0): CanvasNode => ({
  id: "d1",
  type: "canvas",
  position: { x, y: 0 },
  data: { kind: "director", label: "导演台1", scene: createDefaultScene(ids()), ...data },
});

describe("导演台节点存档", () => {
  test("种类和场景原样存下、读回", () => {
    const node = director();
    const graph = serializeGraph([node], [], viewport);
    expect(graph.nodes[0].data).toMatchObject({ kind: "director", label: "导演台1" });
    const restored = deserializeGraph(JSON.parse(JSON.stringify(graph))).nodes[0] as CanvasNode;
    expect(restored.data.kind).toBe("director");
    expect(restored.data.scene).toEqual(node.data.scene);
  });

  test("场景损坏时读成默认场景，节点照常可用", () => {
    const graph = serializeGraph([director()], [], viewport);
    const raw = JSON.parse(JSON.stringify(graph));
    raw.nodes[0].data.scene = { version: 99 };
    const restored = deserializeGraph(raw).nodes[0] as CanvasNode;
    expect(restored.data.scene?.characters).toHaveLength(1);
    expect(restored.data.scene?.cameras).toHaveLength(1);
  });

  test("没有场景的导演台节点读回时补上默认场景", () => {
    const graph = serializeGraph([director({ scene: undefined })], [], viewport);
    const restored = deserializeGraph(JSON.parse(JSON.stringify(graph))).nodes[0] as CanvasNode;
    expect(restored.data.scene?.version).toBe(1);
  });

  test("别的种类的节点不带场景", () => {
    const image: CanvasNode = {
      id: "i1",
      type: "canvas",
      position: { x: 0, y: 0 },
      data: { kind: "image", label: "图片" },
    };
    const restored = deserializeGraph(serializeGraph([image], [], viewport)).nodes[0] as CanvasNode;
    expect(restored.data.scene).toBeUndefined();
  });
});

describe("画布撤销不回滚场景", () => {
  test("撤销挪节点时，场景取眼下的", () => {
    const saved = director({}, 0);
    const edited = addProp(saved.data.scene!, () => "p1").scene;
    const now = director({ scene: edited }, 120);
    const [restored] = restoreNodes([saved], [now]) as CanvasNode[];
    expect(restored.position.x).toBe(0);
    expect(restored.data.scene?.props).toHaveLength(1);
  });
});
