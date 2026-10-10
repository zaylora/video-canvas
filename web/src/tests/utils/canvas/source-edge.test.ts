import { describe, expect, test } from "bun:test";

import { ANIMATED_EDGE_DATA, ANIMATED_EDGE_OPTIONS } from "@/constants/canvas";
import type { CanvasEdge, CanvasNode } from "@/types";
import { deserializeGraph, serializeGraph } from "@/utils/canvas/canvas-persistence";
import { diffGraph } from "@/utils/canvas/graph-changes";
import { mentionableNodes } from "@/utils/canvas/link-rule";
import {
  SOURCE_EDGE_TYPE,
  isSourceEdge,
  makeSourceEdge,
  withoutSourceEdges,
} from "@/utils/canvas/source-edge";

const viewport = { x: 0, y: 0, zoom: 1 };
const linked = (id: string, source: string, target: string): CanvasEdge => ({
  id,
  source,
  target,
  ...ANIMATED_EDGE_OPTIONS,
});

describe("来源线的识别与过滤", () => {
  test("makeSourceEdge 造出的线能被认出来，普通连线认不出", () => {
    const edge = makeSourceEdge("e1", "video", "frame");
    expect(isSourceEdge(edge)).toBe(true);
    expect(isSourceEdge(linked("e2", "a", "b"))).toBe(false);
  });

  test("来源线的外观和普通连线一样：带同一份流动光带的数据", () => {
    const edge = makeSourceEdge("e1", "video", "frame");
    expect(edge).toMatchObject({ id: "e1", source: "video", target: "frame" });
    expect(edge.type).toBe(SOURCE_EDGE_TYPE);
    expect(edge.data).toEqual(ANIMATED_EDGE_DATA);
  });

  test("withoutSourceEdges 只留下参与生成的连线，没有来源线时原样返回", () => {
    const real = linked("e2", "a", "b");
    const source = makeSourceEdge("e1", "video", "frame");
    expect(withoutSourceEdges([source, real])).toEqual([real]);
    const plain = [real];
    expect(withoutSourceEdges(plain)).toBe(plain);
  });
});

describe("来源线的持久化", () => {
  test("保存时带上 relation，读回来仍是来源线，而不是流动光带的连线", () => {
    const saved = serializeGraph(
      [],
      [makeSourceEdge("e1", "video", "frame"), linked("e2", "a", "b")],
      viewport,
    );
    expect(saved.edges[0]).toMatchObject({ id: "e1", relation: "source" });
    expect(saved.edges[1]).not.toHaveProperty("relation");

    const loaded = deserializeGraph(saved).edges;
    expect(isSourceEdge(loaded[0] as CanvasEdge)).toBe(true);
    expect(loaded[0]?.data).toEqual(ANIMATED_EDGE_DATA);
    expect(isSourceEdge(loaded[1] as CanvasEdge)).toBe(false);
    expect(loaded[1]?.type).toBe(ANIMATED_EDGE_OPTIONS.type);
  });
});

describe("来源线不当引用", () => {
  const node = (id: string, kind: "video" | "image"): CanvasNode => ({
    id,
    type: "canvas",
    position: { x: 0, y: 0 },
    data: { kind, label: id, src: `/files/${id}.png`, assetId: id, status: "done" },
  });

  test("视频派生出的帧图，不算在视频的下游，视频的 @ 菜单里仍能选它", () => {
    const video = node("video", "video");
    const frame = node("frame", "image");
    const { canvas } = mentionableNodes(
      "video",
      [video, frame],
      [makeSourceEdge("e1", "video", "frame")],
    );
    expect(canvas.map((item) => item.id)).toEqual(["frame"]);
  });
});

describe("上传中的帧图还没存下来时，指向它的线也不存", () => {
  const uploading: CanvasNode = {
    id: "frame",
    type: "canvas",
    position: { x: 0, y: 0 },
    data: { kind: "image", label: "帧", status: "running", uploadProgress: 30, uploaded: true },
  };
  const video: CanvasNode = {
    id: "video",
    type: "canvas",
    position: { x: 0, y: 0 },
    data: { kind: "video", label: "镜头" },
  };

  test("节点被丢掉时，端点是它的连线不留成悬空线；两端都在的连线照存", () => {
    const edges = [makeSourceEdge("e1", "video", "frame"), linked("e2", "video", "video")];
    const saved = serializeGraph([video, uploading], edges, viewport);
    expect(saved.nodes.map((n) => n.id)).toEqual(["video"]);
    expect(saved.edges.map((e) => e.id)).toEqual(["e2"]);
  });
});

describe("来源线在两份画布合并时不被改成普通连线", () => {
  const base = { nodes: [], edges: [] };

  test("新增的来源线，变更里带着 relation（本地的画布线和存档里的线两种写法都认）", () => {
    for (const edge of [
      makeSourceEdge("e1", "video", "frame"),
      { id: "e1", source: "video", target: "frame", relation: "source" },
    ]) {
      const [change] = diffGraph(base, { nodes: [], edges: [edge] });
      expect(change?.after).toMatchObject({ id: "e1", relation: "source" });
    }
  });

  test("普通连线的变更不带 relation", () => {
    const [change] = diffGraph(base, { nodes: [], edges: [linked("e2", "a", "b")] });
    expect(change?.after).not.toHaveProperty("relation");
  });

  test("按变更重建边（Agent 同步用的 deserializeGraph）仍是来源线", () => {
    const [change] = diffGraph(base, {
      nodes: [],
      edges: [makeSourceEdge("e1", "video", "frame")],
    });
    const rebuilt = deserializeGraph({
      edges: [change?.after as never],
    }).edges[0] as CanvasEdge;
    expect(isSourceEdge(rebuilt)).toBe(true);
  });
});
