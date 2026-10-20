import { describe, expect, test } from "bun:test";

import { ANIMATED_EDGE_OPTIONS } from "@/constants/canvas";
import type { CanvasNode, FlowNode } from "@/types";
import { DOODLE_FILE_NAME, buildDoodleNode } from "@/utils/canvas/doodle-node";
import { NODE_FRAME_SHORT_SIDE, nodeFrameSize } from "@/utils/canvas/node-frame";

import { makeData } from "../tasks/fixtures";

const source: CanvasNode = {
  id: "src",
  type: "canvas",
  position: { x: 100, y: 200 },
  measured: { width: 576, height: 324 },
  data: makeData({ kind: "image", label: "图片", status: "done", src: "/a.png", assetId: "9" }),
};

/** 依次产出 id1、id2……，节点 id 先于连线 id */
const counter = () => {
  let n = 0;
  return () => `id${++n}`;
};

const args = (over: Partial<Parameters<typeof buildDoodleNode>[0]> = {}) => ({
  source,
  nodes: [source] as FlowNode[],
  aspect: 16 / 9,
  newId: counter(),
  ...over,
});

describe("buildDoodleNode 涂鸦结果节点", () => {
  test("是一个等着上传的图片节点，不带任何生成字段", () => {
    const { node } = buildDoodleNode(args());
    expect(node.id).toBe("id1");
    expect(node.type).toBe("canvas");
    expect(node.data).toMatchObject({
      kind: "image",
      status: "running",
      uploadProgress: 0,
      mediaType: "image",
      uploaded: true,
      fileName: DOODLE_FILE_NAME,
    });
    expect(node.data.taskId).toBeUndefined();
    expect(node.data.prompt).toBeUndefined();
    expect(node.data.src).toBeUndefined();
  });

  test("文件名是 doodle.png", () => {
    expect(DOODLE_FILE_NAME).toBe("doodle.png");
  });

  test("标题是「源标题 标注」", () => {
    expect(buildDoodleNode(args()).node.data.label).toBe("图片 标注");
  });

  test("标题重名时按 newNodeLabel 编号", () => {
    const taken: CanvasNode = {
      ...source,
      id: "t",
      position: { x: 9000, y: 9000 },
      data: makeData({ kind: "image", label: "图片 标注" }),
    };
    const { node } = buildDoodleNode(args({ nodes: [source, taken] as FlowNode[] }));
    expect(node.data.label).toBe("图片 标注 2");
  });

  test("记下源图画幅，大小按 nodeFrameSize", () => {
    const { node } = buildDoodleNode(args({ aspect: 9 / 16 }));
    expect(node.data.mediaAspect).toBeCloseTo(9 / 16);
    expect(node.width).toBe(NODE_FRAME_SHORT_SIDE);
    expect(node.height).toBe(nodeFrameSize(9 / 16).height);
  });

  test("摆在源节点右侧", () => {
    const { node } = buildDoodleNode(args());
    expect(node.position.x).toBeGreaterThan(100 + 576);
  });

  test("一条从源节点出来的普通连线", () => {
    const { node, edge } = buildDoodleNode(args());
    expect(edge).toMatchObject({
      id: "id2",
      source: "src",
      target: node.id,
      sourceHandle: null,
      targetHandle: null,
    });
    expect(edge.type).toBe(ANIMATED_EDGE_OPTIONS.type);
  });

  test("不改源节点", () => {
    const before = JSON.stringify(source);
    buildDoodleNode(args());
    expect(JSON.stringify(source)).toBe(before);
  });
});
