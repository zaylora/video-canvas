import { describe, expect, test } from "bun:test";

import { ANIMATED_EDGE_OPTIONS } from "@/constants/canvas";
import type { CanvasNode, FlowNode } from "@/types";
import {
  FRAME_STEP,
  MAX_STAGED_FRAMES,
  buildFrameNodes,
  canStageFrame,
  formatFrameTime,
  frameFileName,
  stepFrameTime,
} from "@/utils/canvas/frame";

import { makeData } from "../tasks/fixtures";

describe("时间显示", () => {
  test("mm:ss，不满一秒向下取整", () => {
    expect(formatFrameTime(0)).toBe("00:00");
    expect(formatFrameTime(4.9)).toBe("00:04");
    expect(formatFrameTime(65)).toBe("01:05");
  });

  test("负数当 0", () => {
    expect(formatFrameTime(-3)).toBe("00:00");
  });
});

describe("帧图文件名", () => {
  test("首帧、尾帧", () => {
    expect(frameFileName("视频 1", "first")).toBe("视频 1-首帧.png");
    expect(frameFileName("视频 1", "last")).toBe("视频 1-尾帧.png");
  });

  test("自定义写成时刻，冒号换成短横线", () => {
    expect(frameFileName("视频 1", 4.2)).toBe("视频 1-00-04.png");
  });

  test("标题里不能出现在文件名里的字符换成下划线", () => {
    expect(frameFileName("a/b:c*d", "first")).toBe("a_b_c_d-首帧.png");
  });
});

describe("暂存规则", () => {
  test("没到上限、不重复就能暂存", () => {
    expect(canStageFrame([{ time: 1 }], 2)).toEqual({ ok: true });
  });

  test("到上限不能再收", () => {
    const staged = Array.from({ length: MAX_STAGED_FRAMES }, (_, i) => ({ time: i }));
    expect(canStageFrame(staged, 99)).toEqual({ ok: false, reason: "limit" });
  });

  test("同一帧之内的时刻算重复，相邻的两帧不算", () => {
    expect(canStageFrame([{ time: 4 }], 4.005)).toEqual({ ok: false, reason: "duplicate" });
    expect(canStageFrame([{ time: 4 }], 4 + FRAME_STEP)).toEqual({ ok: true });
  });
});

describe("方向键微调播放头", () => {
  test("走一帧、按住 Shift 走一秒，钳在 0 到时长之间", () => {
    expect(stepFrameTime(1, 1, false, 8)).toBeCloseTo(1 + FRAME_STEP, 6);
    expect(stepFrameTime(1, -1, true, 8)).toBe(0);
    expect(stepFrameTime(7.99, 1, true, 8)).toBe(8);
    expect(stepFrameTime(0, -1, false, 8)).toBe(0);
  });
});

describe("buildFrameNodes 造帧图节点和连线", () => {
  const source: CanvasNode = {
    id: "src",
    type: "canvas",
    position: { x: 100, y: 200 },
    measured: { width: 576, height: 324 },
    data: makeData({ kind: "video", label: "视频 1", status: "done", src: "/files/a.mp4" }),
  };
  let n = 0;
  const args = (names: string[], over: object = {}) => ({
    source,
    nodes: [source] as FlowNode[],
    files: names.map((name) => ({ name })),
    aspect: 16 / 9,
    newId: () => `id${++n}`,
    ...over,
  });

  test("每个文件一个上传中的图片节点，没有生成字段", () => {
    const { nodes } = buildFrameNodes(args(["视频 1-首帧.png"]));
    expect(nodes).toHaveLength(1);
    expect(nodes[0].data).toMatchObject({
      kind: "image",
      status: "running",
      uploadProgress: 0,
      mediaType: "image",
      uploaded: true,
      fileName: "视频 1-首帧.png",
    });
    expect(nodes[0].data.taskId).toBeUndefined();
    expect(nodes[0].data.prompt).toBeUndefined();
  });

  test("每个节点一根从视频出来的普通连线", () => {
    const { nodes, edges } = buildFrameNodes(args(["a.png", "b.png"]));
    expect(edges).toHaveLength(2);
    expect(edges.map((edge) => edge.source)).toEqual(["src", "src"]);
    expect(edges.map((edge) => edge.target)).toEqual(nodes.map((node) => node.id));
    for (const edge of edges) expect(edge.type).toBe(ANIMATED_EDGE_OPTIONS.type);
  });

  test("摆在视频右侧，多个按顺序从上往下排、互不重叠", () => {
    const { nodes } = buildFrameNodes(args(["a.png", "b.png", "c.png"]));
    const xs = new Set(nodes.map((node) => node.position.x));
    expect(xs.size).toBe(1);
    expect([...xs][0]).toBeGreaterThan(100 + 576);
    const ys = nodes.map((node) => node.position.y);
    expect(ys[1]).toBeGreaterThan(ys[0]);
    expect(ys[2]).toBeGreaterThan(ys[1]);
  });

  test("标题按文件名取，重名时编号", () => {
    const taken: CanvasNode = {
      ...source,
      id: "t",
      position: { x: 9000, y: 9000 },
      data: makeData({ kind: "image", label: "视频 1-首帧" }),
    };
    const { nodes } = buildFrameNodes(
      args(["视频 1-首帧.png"], { nodes: [source, taken] as FlowNode[] }),
    );
    expect(nodes[0].data.label).toBe("视频 1-首帧 2");
  });

  test("没有文件什么也不建", () => {
    expect(buildFrameNodes(args([]))).toEqual({ nodes: [], edges: [] });
  });
});
