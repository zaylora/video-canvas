import { describe, expect, test } from "bun:test";

import type { CanvasEdge, CanvasNode } from "@/types";
import { duplicateNode } from "@/utils/canvas/duplicate";

const node = (id: string, label: string, x = 0, y = 0, extra = {}): CanvasNode => ({
  id,
  type: "canvas",
  position: { x, y },
  measured: { width: 200, height: 100 },
  data: { kind: "video", label, ...extra },
});

const edge = (id: string, source: string, target: string, targetHandle: string | null = null) =>
  ({ id, source, target, targetHandle }) as CanvasEdge;

const ids = () => {
  let n = 0;
  return () => `id${++n}`;
};

describe("duplicateNode", () => {
  const source = node("src", "视频节点", 0, 0, {
    model: "m1",
    prompt: "猫",
    params: { prompt: "猫", duration: 8, images: ["7"] },
    paramAssets: { "7": { url: "u" } },
    status: "done",
    src: "https://x/a.mp4",
    assetId: "9",
    taskId: "t1",
    error: "旧错误",
  });

  test("复制内容与上游连线，不复制结果、任务状态与出去的连线", () => {
    const edges = [
      edge("e1", "img", "src", "images"),
      edge("e2", "txt", "src", "prompt"),
      edge("e3", "src", "down"),
    ];
    const out = duplicateNode(source, [source], edges, 2, ids());
    expect(out.nodes).toHaveLength(2);
    const copy = out.nodes[0];
    expect(copy.data).toEqual({
      kind: "video",
      label: "视频节点 2",
      model: "m1",
      prompt: "猫",
      params: { prompt: "猫", duration: 8, images: ["7"] },
      paramAssets: { "7": { url: "u" } },
    });
    expect(copy.selected).toBe(true);
    expect(out.nodes[1].data.label).toBe("视频节点 3");
    // 每个副本各复制一份进来的连线（2 根），出去的那根不复制
    expect(out.edges).toHaveLength(4);
    expect(
      out.edges.filter((e) => e.target === copy.id).map((e) => [e.source, e.targetHandle]),
    ).toEqual([
      ["img", "images"],
      ["txt", "prompt"],
    ]);
    // 参数是深拷贝，改副本不影响原节点
    (copy.data.params as { images: string[] }).images.push("8");
    expect(source.data.params?.images).toEqual(["7"]);
  });

  test("编号跳过已占用的，副本不和已有节点重叠", () => {
    const blocker = node("b", "视频节点 2", 240, 0);
    const out = duplicateNode(source, [source, blocker], [], 1, ids());
    expect(out.nodes[0].data.label).toBe("视频节点 3");
    const p = out.nodes[0].position;
    const hit =
      p.x < 240 + 200 + 40 && 240 < p.x + 200 + 40 && p.y < 100 + 40 && 0 < p.y + 100 + 40;
    expect(hit).toBe(false);
  });

  test("原节点标题本身带编号时按同一底名续编", () => {
    const out = duplicateNode(node("s", "图片节点 5"), [node("s", "图片节点 5")], [], 2, ids());
    expect(out.nodes.map((n) => n.data.label)).toEqual(["图片节点 2", "图片节点 3"]);
  });
});
