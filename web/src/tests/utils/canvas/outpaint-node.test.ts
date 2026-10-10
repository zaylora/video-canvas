import { describe, expect, test } from "bun:test";

import type { CanvasNode } from "@/types";
import { planOutpaintNode } from "@/utils/canvas/outpaint-node";
import { isSourceEdge } from "@/utils/canvas/source-edge";

const source: CanvasNode = {
  id: "img",
  type: "canvas",
  position: { x: 100, y: 200 },
  measured: { width: 576, height: 324 },
  data: { kind: "image", label: "图片 1", src: "/files/a.png", assetId: "7", status: "done" },
};

describe("planOutpaintNode：扩图结果节点怎么落到画布上", () => {
  const plan = () =>
    planOutpaintNode(
      source,
      [source],
      { modelKey: "nano", prompt: "补全指令", aspect: 1.5 },
      (() => {
        let n = 0;
        return () => `id${++n}`;
      })(),
    );

  test("放在原图右侧，顶边对齐，不和原图叠", () => {
    const { node } = plan();
    expect(node.position.x).toBeGreaterThan(100 + 576);
    expect(node.position.y).toBe(200);
  });

  test("是一个生成型图片节点：带模型和提示词，先显示进度（拼图、上传中），不是上传型", () => {
    const { node } = plan();
    expect(node.data).toMatchObject({
      kind: "image",
      label: "图片 1-扩图",
      model: "nano",
      status: "running",
      uploadProgress: 0,
      params: { prompt: "补全指令", op: "i2i" },
      aspect: 1.5,
    });
    expect(node.data.uploaded).toBeFalsy();
    expect(node.data.src).toBeUndefined();
  });

  test("挂一根从原图出来的来源线", () => {
    const { node, edge } = plan();
    expect(isSourceEdge(edge)).toBe(true);
    expect(edge.source).toBe("img");
    expect(edge.target).toBe(node.id);
  });

  test("画布里已有同名节点时标题错开", () => {
    const taken: CanvasNode = {
      ...source,
      id: "t",
      position: { x: 0, y: 3000 },
      data: { kind: "image", label: "图片 1-扩图" },
    };
    const { node } = planOutpaintNode(source, [source, taken], {
      modelKey: "m",
      prompt: "p",
      aspect: 1,
    });
    expect(node.data.label).toBe("图片 1-扩图 2");
  });
});
