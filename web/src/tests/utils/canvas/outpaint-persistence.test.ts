import { describe, expect, test } from "bun:test";

import type { CanvasNode } from "@/types";
import {
  OUTPAINT_INTERRUPTED,
  OUTPAINT_RESUBMIT,
  deserializeGraph,
  hasVolatileRunning,
  serializeGraph,
} from "@/utils/canvas/canvas-persistence";

import { makeData } from "../tasks/fixtures";

const viewport = { x: 0, y: 0, zoom: 1 };

const node = (id: string, data: Partial<CanvasNode["data"]>): CanvasNode => ({
  id,
  type: "canvas",
  position: { x: 0, y: 0 },
  data: makeData({ kind: "image", label: id, ...data }),
});

/** 扩图结果节点在「读图、合成、上传、提交」这几步里的样子：生成中，还没有 taskId */
const outpainting = (data: Partial<CanvasNode["data"]> = {}) =>
  node("out", { status: "running", outpaintFrom: "src", model: "m", prompt: "p", ...data });

describe("扩图中途刷新：节点不能悄悄变成空壳", () => {
  test("合成、上传阶段（还没有参考图）存档成失败态，写明要回原图重新扩图", () => {
    const graph = serializeGraph([outpainting()], [], viewport);
    expect(graph.nodes).toHaveLength(1);
    expect(graph.nodes[0].data).toMatchObject({
      status: "error",
      error: OUTPAINT_INTERRUPTED,
      outpaintFrom: "src",
    });
  });

  test("合成图已经传好（有参考图）只差提交时，提示点重试重新提交", () => {
    const graph = serializeGraph(
      [outpainting({ params: { prompt: "p", images: ["9"] } })],
      [],
      viewport,
    );
    expect(graph.nodes[0].data).toMatchObject({ status: "error", error: OUTPAINT_RESUBMIT });
  });

  test("扩图节点中途也要能存下来，不被「本地生成中」拖住保存", () => {
    expect(hasVolatileRunning([outpainting()])).toBe(false);
  });

  test("普通的生成中（没有 taskId、不是扩图）还是等它收尾再存，行为不变", () => {
    expect(hasVolatileRunning([node("gen", { status: "running" })])).toBe(true);
    const graph = serializeGraph([node("gen", { status: "running" })], [], viewport);
    expect(graph.nodes[0].data.status).toBe("idle");
  });

  test("已经拿到 taskId 的扩图节点照常存成生成中，刷新后靠任务对账", () => {
    const graph = serializeGraph([outpainting({ taskId: "t1" })], [], viewport);
    expect(graph.nodes[0].data).toMatchObject({ status: "running", taskId: "t1" });
    expect(hasVolatileRunning([outpainting({ taskId: "t1" })])).toBe(false);
  });

  test("读档：存档里残留的「生成中、没有 taskId」的扩图节点也读成失败态", () => {
    const graph = deserializeGraph({
      nodes: [
        {
          id: "out",
          type: "canvas",
          position: { x: 0, y: 0 },
          data: { kind: "image", label: "out", status: "running", outpaintFrom: "src" },
        },
      ],
      edges: [],
      viewport,
    });
    expect(graph.nodes[0].data).toMatchObject({ status: "error", error: OUTPAINT_INTERRUPTED });
  });
});
