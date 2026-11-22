import { describe, expect, test } from "bun:test";

import type { CanvasNode } from "@/types";
import { deserializeGraph, serializeGraph } from "@/utils/canvas/canvas-persistence";

import { makeData } from "../tasks/fixtures";

const viewport = { x: 0, y: 0, zoom: 1 };

/** 合成成功后的分镜表节点：成片三件套在 data 顶层，storyboard 里是镜头 */
const composedNode = (): CanvasNode => ({
  id: "board",
  type: "canvas",
  position: { x: 10, y: 20 },
  data: makeData({
    kind: "storyboard",
    label: "分镜表",
    storyboard: {
      shots: [{ id: "s1", scene: "", shot: "", camera: "", action: "", dialogue: "" }],
    },
    finalAssetId: "1024",
    finalFingerprint: "abc123",
    finalDurationMs: 42000,
    finalSegmentCount: 3,
  }),
});

describe("成片字段必须活过存档往返", () => {
  test("存档带上四个 final* 字段（否则刷新页面成片信息就没了）", () => {
    const graph = serializeGraph([composedNode()], [], viewport);
    expect(graph.nodes[0].data).toMatchObject({
      finalAssetId: "1024",
      finalFingerprint: "abc123",
      finalDurationMs: 42000,
      finalSegmentCount: 3,
    });
  });

  test("存档 → 读档 → 再存档，四个字段逐项不丢", () => {
    const first = serializeGraph([composedNode()], [], viewport);
    const back = deserializeGraph(first).nodes as CanvasNode[];
    const second = serializeGraph(back, [], viewport);
    expect(second.nodes[0].data).toMatchObject({
      finalAssetId: "1024",
      finalFingerprint: "abc123",
      finalDurationMs: 42000,
      finalSegmentCount: 3,
    });
  });

  test("没有成片的节点不会被塞上空字段", () => {
    const plain: CanvasNode = {
      id: "img",
      type: "canvas",
      position: { x: 0, y: 0 },
      data: makeData({ kind: "image", label: "图" }),
    };
    const graph = serializeGraph([plain], [], viewport);
    expect(graph.nodes[0].data).not.toHaveProperty("finalAssetId");
    expect(graph.nodes[0].data).not.toHaveProperty("finalFingerprint");
  });
});
