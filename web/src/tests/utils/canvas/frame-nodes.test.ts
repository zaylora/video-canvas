import { describe, expect, test } from "bun:test";

import type { CanvasNode } from "@/types";
import { planFrameNodes } from "@/utils/canvas/frame-nodes";
import { DEFAULT_NODE_SIZE } from "@/utils/canvas/placement";
import { isSourceEdge } from "@/utils/canvas/source-edge";

const file = (name: string) => new File(["x"], name, { type: "image/png" });

const node = (
  id: string,
  x: number,
  y: number,
  extra: Partial<CanvasNode> = {},
  label = id,
): CanvasNode => ({
  id,
  type: "canvas",
  position: { x, y },
  data: { kind: "video", label },
  ...extra,
});

const ids = () => {
  let n = 0;
  return () => `id${++n}`;
};

describe("planFrameNodes：截出来的帧怎么落到画布上", () => {
  const video = node("v", 100, 200, { measured: { width: 576, height: 324 } }, "镜头10");

  test("一帧：放在视频右侧，顶边对齐，是带上传进度的上传型图片节点", () => {
    const plan = planFrameNodes(video, [video], [file("镜头10-首帧.png")], ids());
    expect(plan.nodes).toHaveLength(1);
    const [frame] = plan.nodes;
    expect(frame.position.x).toBeGreaterThan(100 + 576);
    expect(frame.position.y).toBe(200);
    expect(frame.data).toMatchObject({
      kind: "image",
      label: "镜头10-首帧",
      fileName: "镜头10-首帧.png",
      uploaded: true,
      status: "running",
      uploadProgress: 0,
      mediaType: "image",
    });
    expect(frame.selected).toBeFalsy();
  });

  test("每个帧图都挂一根从视频出来的来源线", () => {
    const plan = planFrameNodes(video, [video], [file("a.png"), file("b.png")], ids());
    expect(plan.edges).toHaveLength(2);
    for (const [index, edge] of plan.edges.entries()) {
      expect(isSourceEdge(edge)).toBe(true);
      expect(edge.source).toBe("v");
      expect(edge.target).toBe(plan.nodes[index]?.id);
    }
  });

  test("多帧：同一列从上往下排，互不重叠，按截取顺序", () => {
    const plan = planFrameNodes(
      video,
      [video],
      [file("a.png"), file("b.png"), file("c.png")],
      ids(),
    );
    const xs = new Set(plan.nodes.map((item) => item.position.x));
    expect(xs.size).toBe(1);
    const ys = plan.nodes.map((item) => item.position.y);
    expect(ys).toEqual([...ys].sort((a, b) => a - b));
    expect(ys[1] - ys[0]).toBeGreaterThanOrEqual(DEFAULT_NODE_SIZE.height);
    expect(plan.nodes.map((item) => item.data.fileName)).toEqual(["a.png", "b.png", "c.png"]);
  });

  test("右侧已经有节点挡着：挪到不重叠的空位，不叠在一起", () => {
    const blocker = node("b", 100 + 576 + 60, 200, { measured: { width: 576, height: 324 } });
    const plan = planFrameNodes(video, [video, blocker], [file("a.png")], ids());
    const { x, y } = plan.nodes[0]?.position ?? { x: 0, y: 0 };
    const clear =
      x >= blocker.position.x + 576 ||
      y >= blocker.position.y + 324 ||
      x + DEFAULT_NODE_SIZE.width <= blocker.position.x ||
      y + DEFAULT_NODE_SIZE.height <= blocker.position.y;
    expect(clear).toBe(true);
  });

  test("节点标题避开画布里已有的同名节点，同一次多帧同名也要错开", () => {
    const taken = node("t", 0, 2000, {}, "镜头10-首帧");
    const plan = planFrameNodes(
      video,
      [video, taken],
      [file("镜头10-首帧.png"), file("镜头10-首帧.png")],
      ids(),
    );
    expect(plan.nodes.map((item) => item.data.label)).toEqual(["镜头10-首帧 2", "镜头10-首帧 3"]);
  });

  test("视频在组里：按画布上的绝对位置算，帧图落在画布上不入组", () => {
    const group: CanvasNode = {
      id: "g",
      type: "group",
      position: { x: 1000, y: 1000 },
      data: { label: "组" },
    } as unknown as CanvasNode;
    const inGroup = node("v2", 50, 60, { parentId: "g", measured: { width: 576, height: 324 } });
    const plan = planFrameNodes(inGroup, [group, inGroup], [file("a.png")], ids());
    expect(plan.nodes[0]?.parentId).toBeUndefined();
    expect(plan.nodes[0]?.position.x).toBeGreaterThan(1050 + 576);
    expect(plan.nodes[0]?.position.y).toBe(1060);
  });

  test("没有文件时什么都不建", () => {
    expect(planFrameNodes(video, [video], [], ids())).toEqual({ nodes: [], edges: [] });
  });
});
