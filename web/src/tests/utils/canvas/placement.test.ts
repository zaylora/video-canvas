import { describe, expect, test } from "bun:test";

import { defaultNodeSize, topLeftFromAnchor } from "@/utils/canvas/placement";

describe("defaultNodeSize：新节点还没测量时的默认尺寸", () => {
  test("视频节点比其他节点大（576×324）", () => {
    expect(defaultNodeSize("video")).toEqual({ width: 576, height: 324 });
  });

  test("其余种类 384×216", () => {
    expect(defaultNodeSize("image")).toEqual({ width: 384, height: 216 });
    expect(defaultNodeSize("text")).toEqual({ width: 384, height: 216 });
  });
});

describe("topLeftFromAnchor：让落点落在新节点自己的连接点上", () => {
  test("从 source 端拉出：落点是新节点左侧中点", () => {
    expect(topLeftFromAnchor("image", { x: 500, y: 300 }, [0, 0.5])).toEqual({ x: 500, y: 192 });
  });

  test("从 target 端拉出：落点是新节点右侧中点", () => {
    expect(topLeftFromAnchor("image", { x: 500, y: 300 }, [1, 0.5])).toEqual({ x: 116, y: 192 });
  });

  test("视频节点按自己的尺寸算", () => {
    expect(topLeftFromAnchor("video", { x: 500, y: 300 }, [1, 0.5])).toEqual({
      x: -76,
      y: 138,
    });
  });

  test("没有锚点（[0, 0]）时落点就是左上角", () => {
    expect(topLeftFromAnchor("image", { x: 500, y: 300 }, [0, 0])).toEqual({ x: 500, y: 300 });
  });
});

import type { CanvasGraphDto } from "@/api/canvas/type";
import { deserializeGraph } from "@/utils/canvas/canvas-persistence";

describe("读档：老存档里带 origin 的节点统一换算成左上角", () => {
  const saved = (nodes: unknown[]) =>
    deserializeGraph({ nodes, edges: [], viewport: { x: 0, y: 0, zoom: 1 } } as CanvasGraphDto);
  const imageNode = (extra: object) => ({
    id: "a",
    type: "canvas",
    position: { x: 500, y: 300 },
    data: { kind: "image", label: "A" },
    ...extra,
  });

  test("右中锚点：position 换成画面左上角，origin 去掉", () => {
    const [node] = saved([imageNode({ origin: [1, 0.5] })]).nodes;
    expect(node.position).toEqual({ x: 116, y: 192 });
    expect(node.origin).toBeUndefined();
  });

  test("左中锚点同理", () => {
    const [node] = saved([imageNode({ origin: [0, 0.5] })]).nodes;
    expect(node.position).toEqual({ x: 500, y: 192 });
    expect(node.origin).toBeUndefined();
  });

  test("视频节点按视频的默认尺寸换算", () => {
    const [node] = saved([
      imageNode({ origin: [1, 0.5], data: { kind: "video", label: "V" } }),
    ]).nodes;
    expect(node.position).toEqual({ x: -76, y: 138 });
  });

  test("没有 origin 的节点原样不动", () => {
    const [node] = saved([imageNode({})]).nodes;
    expect(node.position).toEqual({ x: 500, y: 300 });
  });
});
