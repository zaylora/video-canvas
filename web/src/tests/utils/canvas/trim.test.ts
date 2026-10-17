import { describe, expect, test } from "bun:test";

import type { CanvasNode, FlowNode } from "@/types";
import {
  TRIM_MIN_GAP,
  buildTrimNode,
  canConfirmTrim,
  clampTrimTime,
  confirmHint,
  estimatedTrimBytes,
  formatTrimSeconds,
  isFullTrim,
  thumbTimes,
  trimFileName,
  trimRange,
} from "@/utils/canvas/trim";

import { makeData } from "../tasks/fixtures";

describe("时间取整与钳制", () => {
  test("取整到 0.01 秒，浮点误差不留尾巴", () => {
    expect(clampTrimTime(3.333333, 29)).toBe(3.33);
    expect(clampTrimTime(0.1 + 0.2, 29)).toBe(0.3);
  });

  test("超出范围钳到 0 到总时长", () => {
    expect(clampTrimTime(-1, 29)).toBe(0);
    expect(clampTrimTime(99, 29)).toBe(29);
  });

  test("总时长还没量出来（0）时钳到 0", () => {
    expect(clampTrimTime(5, 0)).toBe(0);
  });
});

describe("拖手柄改选区", () => {
  test("左手柄不能超过右手柄，最多贴到最小间隔", () => {
    expect(trimRange(3, 10, 29, "start", 9.8)).toEqual({ start: 9.5, end: 10 });
  });

  test("右手柄不能小于左手柄，最多贴到最小间隔", () => {
    expect(trimRange(3, 10, 29, "end", 3.2)).toEqual({ start: 3, end: 3.5 });
  });

  test("拖到负数或超出总时长都停在边界", () => {
    expect(trimRange(3, 10, 29, "start", -4)).toEqual({ start: 0, end: 10 });
    expect(trimRange(3, 10, 29, "end", 40)).toEqual({ start: 3, end: 29 });
  });

  test("拖动不会改变另一端", () => {
    expect(trimRange(0, 29, 29, "end", 12)).toEqual({ start: 0, end: 12 });
  });
});

describe("选区显示", () => {
  test("时长保留两位小数，带空格和 s", () => {
    expect(formatTrimSeconds(28.874)).toBe("28.87 s");
    expect(formatTrimSeconds(9)).toBe("9.00 s");
  });

  test("全长算没修剪", () => {
    expect(isFullTrim(0, 29, 29)).toBe(true);
    expect(isFullTrim(0.01, 28.99, 29)).toBe(true);
    expect(isFullTrim(0.05, 29, 29)).toBe(false);
    expect(isFullTrim(0, 28.5, 29)).toBe(false);
  });
});

describe("确认按钮能不能点", () => {
  const span = TRIM_MIN_GAP * 3;

  test("参数还没加载出来（总时长为 0）时不能点", () => {
    expect(canConfirmTrim({ start: 0, end: 0, duration: 0, phase: "loading" })).toBe(false);
  });

  test("没修剪（选区等于全长）不能点，提示拖手柄", () => {
    expect(canConfirmTrim({ start: 0, end: 29, duration: 29, phase: "editing" })).toBe(false);
    expect(confirmHint({ start: 0, end: 29, duration: 29, phase: "editing" })).toBe(
      "拖动两端选择要保留的片段",
    );
  });

  test("选区太短不能点，提示最短多久", () => {
    const state = { start: 3, end: 3.2, duration: 29, phase: "editing" as const };
    expect(canConfirmTrim(state)).toBe(false);
    expect(confirmHint(state)).toBe("选区至少 0.5 秒");
  });

  test("编码中或还在加载时不能点", () => {
    expect(canConfirmTrim({ start: 3, end: 3 + span, duration: 29, phase: "encoding" })).toBe(
      false,
    );
    expect(canConfirmTrim({ start: 3, end: 3 + span, duration: 29, phase: "loading" })).toBe(false);
  });

  test("正常修剪（剪掉了一部分、时长够）可以点", () => {
    const state = { start: 3, end: 3 + span, duration: 29, phase: "editing" as const };
    expect(canConfirmTrim(state)).toBe(true);
    expect(confirmHint(state)).toBe("确认修剪");
  });
});

describe("胶片条缩略图的时间点", () => {
  test("每一格取那一段的中点，铺满整段视频", () => {
    expect(thumbTimes(10, 4)).toEqual([1.25, 3.75, 6.25, 8.75]);
  });

  test("没有时长时返回空数组", () => {
    expect(thumbTimes(0, 4)).toEqual([]);
    expect(thumbTimes(10, 0)).toEqual([]);
  });
});

describe("产出文件名", () => {
  test("用源节点标题加 -剪辑，非法字符换成下划线", () => {
    expect(trimFileName("视频 1", "mp4")).toBe("视频 1-剪辑.mp4");
    expect(trimFileName("a/b:c", "mp4")).toBe("a_b_c-剪辑.mp4");
  });

  test("标题太长时按键位截断，不超长", () => {
    const name = trimFileName("很长的标题".repeat(20), "mp4");
    expect([...name].length).toBeLessThanOrEqual(60);
    expect(name.endsWith("-剪辑.mp4")).toBe(true);
  });
});

describe("编码前预估体积", () => {
  test("按选区占全片的比例折算", () => {
    expect(estimatedTrimBytes(100 * 1024 * 1024, 5, 20, 30)).toBe(50 * 1024 * 1024);
  });

  test("总时长为 0 时按原大小算，宁可拦下也不放过", () => {
    expect(estimatedTrimBytes(90 * 1024 * 1024, 0, 1, 0)).toBe(90 * 1024 * 1024);
  });
});

describe("buildTrimNode 造剪辑节点和连线", () => {
  const source: CanvasNode = {
    id: "src",
    type: "canvas",
    position: { x: 100, y: 200 },
    measured: { width: 576, height: 324 },
    data: makeData({ kind: "video", label: "视频 1", status: "done", src: "/files/a.mp4" }),
  };
  const args = {
    source,
    nodes: [source] as FlowNode[],
    fileName: "视频 1-剪辑.mp4",
    aspect: 16 / 9,
    newId: () => "new1",
    edgeId: () => "edge1",
  };

  test("节点记下源视频的画幅", () => {
    const { node } = buildTrimNode({ ...args, aspect: 1 });
    expect(node.data.mediaAspect).toBe(1);
  });

  test("是上传中的视频节点，标题「剪辑」，带上传标记，没有任何生成字段", () => {
    const { node } = buildTrimNode(args);
    expect(node.id).toBe("new1");
    expect(node.type).toBe("canvas");
    expect(node.data).toMatchObject({
      kind: "video",
      label: "剪辑",
      status: "running",
      uploadProgress: 0,
      mediaType: "video",
      uploaded: true,
      fileName: "视频 1-剪辑.mp4",
    });
    expect(node.data.taskId).toBeUndefined();
    expect(node.data.model).toBeUndefined();
    expect(node.data.prompt).toBeUndefined();
  });

  test("重名时编号：剪辑 2、剪辑 3", () => {
    const taken = (label: string): CanvasNode => ({
      ...source,
      id: label,
      position: { x: 5000, y: 5000 },
      data: makeData({ kind: "video", label }),
    });
    const { node } = buildTrimNode({
      ...args,
      nodes: [source, taken("剪辑"), taken("剪辑 2")],
    });
    expect(node.data.label).toBe("剪辑 3");
  });

  test("连一条源节点到新节点的普通动画连线", () => {
    const { edge } = buildTrimNode(args);
    expect(edge).toMatchObject({
      id: "edge1",
      source: "src",
      target: "new1",
      sourceHandle: null,
      targetHandle: null,
      type: "animatedSvgEdge",
    });
  });

  test("摆在源节点右侧 80px 外，按新节点自己的画幅垂直居中", () => {
    const { node } = buildTrimNode(args);
    expect(node.position.x).toBe(100 + 576 + 80);
    expect(node.position.y).toBeCloseTo(200);
  });

  test("竖屏视频的新节点框是竖的，按它的高度居中", () => {
    const { node } = buildTrimNode({ ...args, aspect: 9 / 16 });
    expect(node.position.y).toBeCloseTo(200 + 324 / 2 - 576 / 2);
  });

  test("右侧被占就向下错开", () => {
    const blocker: CanvasNode = { ...source, id: "b", position: { x: 756, y: 200 } };
    const { node } = buildTrimNode({ ...args, nodes: [source, blocker] });
    expect(node.position.y).toBeGreaterThanOrEqual(200 + 324);
  });

  test("可以修剪一个已经是「剪辑」的节点：新节点重名编号，源节点不变", () => {
    const cut: CanvasNode = {
      ...source,
      id: "cut",
      data: makeData({ kind: "video", label: "剪辑", uploaded: true, src: "/files/c.mp4" }),
    };
    const { node, edge } = buildTrimNode({ ...args, source: cut, nodes: [source, cut] });
    expect(node.data.label).toBe("剪辑 2");
    expect(edge.source).toBe("cut");
    expect(cut.data.label).toBe("剪辑");
  });
});
