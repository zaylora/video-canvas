import { describe, expect, test } from "bun:test";

import type { CanvasNode, CanvasNodeData } from "@/types";
import { contentKey, restoreNodes, structureKey } from "@/utils/canvas/history";

const node = (id: string, data: Partial<CanvasNodeData> = {}, x = 0): CanvasNode => ({
  id,
  type: "canvas",
  position: { x, y: 0 },
  data: { kind: "image", label: id, ...data },
});

describe("structureKey / contentKey", () => {
  test("任务字段变化不算一步", () => {
    const a = { nodes: [node("n1")], edges: [] };
    const b = { nodes: [node("n1", { status: "running", taskId: "3", src: null })], edges: [] };
    expect(structureKey(a)).toBe(structureKey(b));
    expect(contentKey(a)).toBe(contentKey(b));
  });

  test("挪位置是结构变化，改提示词是内容变化", () => {
    const a = { nodes: [node("n1")], edges: [] };
    expect(structureKey({ nodes: [node("n1", {}, 40)], edges: [] })).not.toBe(structureKey(a));
    const typed = { nodes: [node("n1", { prompt: "hi" })], edges: [] };
    expect(structureKey(typed)).toBe(structureKey(a));
    expect(contentKey(typed)).not.toBe(contentKey(a));
  });
});

describe("restoreNodes", () => {
  const out = (id: string) => ({
    id,
    src: `/f/${id}.png`,
    mediaType: "image" as const,
    assetId: id,
    createdAt: 0,
  });

  test("位置和提示词取快照，生成结果取眼下", () => {
    const saved = node("n1", { prompt: "old" }, 0);
    const now = node(
      "n1",
      { prompt: "new", status: "done", outputs: [out("1")], src: "/f/1.png" },
      99,
    );
    const [restored] = restoreNodes([saved], [now]);
    expect(restored.position.x).toBe(0);
    expect(restored.data).toMatchObject({ prompt: "old", status: "done", src: "/f/1.png" });
    expect(restored.data.outputs).toHaveLength(1);
  });

  test("快照里的当前版本按眼下的历史重新镜像", () => {
    const saved = node("n1", { activeOutputId: "1" });
    const now = node("n1", {
      status: "done",
      outputs: [out("1"), out("2")],
      activeOutputId: "2",
      src: "/f/2.png",
      assetId: "2",
    });
    const [restored] = restoreNodes([saved], [now]);
    expect(restored.data).toMatchObject({ activeOutputId: "1", src: "/f/1.png", assetId: "1" });
  });

  test("被删的节点原样放回，眼下多出来的节点去掉", () => {
    const restored = restoreNodes([node("n1"), node("n2")], [node("n1"), node("n3")]);
    expect(restored.map((item) => item.id)).toEqual(["n1", "n2"]);
  });

  test("快照里没有、眼下才有的任务字段会被带上", () => {
    const [restored] = restoreNodes([node("n1")], [node("n1", { status: "running", taskId: "7" })]);
    expect(restored.data).toMatchObject({ status: "running", taskId: "7" });
  });
});
