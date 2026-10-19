import { describe, expect, test } from "bun:test";

import type { CanvasNode, FlowNode } from "@/types";
import { buildDirectorExportNodes } from "@/utils/canvas/director-export";

const director: CanvasNode = {
  id: "d1",
  type: "canvas",
  position: { x: 0, y: 0 },
  measured: { width: 576, height: 324 },
  data: { kind: "director", label: "导演台1" },
};

const ids = () => {
  let n = 0;
  return () => `n${++n}`;
};

const build = (nodes: FlowNode[] = [director]) =>
  buildDirectorExportNodes({
    source: director,
    nodes,
    items: [
      { kind: "video", cameraName: "机位1", aspect: 16 / 9, fileName: "导演台1_机位1.mp4" },
      { kind: "image", cameraName: "机位1", aspect: 16 / 9, fileName: "导演台1_机位1.png" },
      { kind: "video", cameraName: "机位2", aspect: 9 / 16, fileName: "导演台1_机位2.mp4" },
    ],
    newId: ids(),
    edgeId: ids(),
  });

describe("导出节点", () => {
  test("每段视频、每张首帧图各建一个上传中的素材节点", () => {
    const { nodes } = build();
    expect(nodes).toHaveLength(3);
    expect(nodes.map((n) => n.data.label)).toEqual([
      "导演台1·机位1",
      "导演台1·机位1·首帧",
      "导演台1·机位2",
    ]);
    expect(nodes[0].data).toMatchObject({
      kind: "video",
      mediaType: "video",
      uploaded: true,
      status: "running",
      uploadProgress: 0,
      fileName: "导演台1_机位1.mp4",
    });
    expect(nodes[1].data).toMatchObject({ kind: "image", mediaType: "image" });
    expect(nodes[2].data.mediaAspect).toBeCloseTo(9 / 16, 6);
  });

  test("每个节点都有一根从导演台出发的来源线", () => {
    const { nodes, edges } = build();
    expect(edges.map((e) => [e.source, e.target])).toEqual(nodes.map((n) => ["d1", n.id]));
  });

  test("摆在导演台右侧，彼此不重叠", () => {
    const { nodes } = build();
    for (const node of nodes) expect(node.position.x).toBeGreaterThan(576);
    const ys = nodes.map((n) => n.position.y);
    expect(new Set(ys).size).toBe(3);
  });

  test("重复导出时名字自动编号，不和旧导出撞名", () => {
    const first = build();
    const again = build([director, ...first.nodes]);
    expect(again.nodes[0].data.label).toBe("导演台1·机位1 2");
  });
});
