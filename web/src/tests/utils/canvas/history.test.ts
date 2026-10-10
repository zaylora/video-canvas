import { describe, expect, test } from "bun:test";

import type { CanvasGroupNode, CanvasNode, CanvasNodeData } from "@/types";
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

  test("上传进度取眼下的：撤销别的操作不会把还在传的节点进度倒回快照时的 0", () => {
    const saved = node("n1", { status: "running", uploadProgress: 0 });
    const now = node("n1", { status: "running", uploadProgress: 64 });
    const [restored] = restoreNodes([saved], [now]);
    expect(restored.data.uploadProgress).toBe(64);
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

describe("组", () => {
  const group = (width: number, x = 0): CanvasGroupNode => ({
    id: "g",
    type: "group",
    position: { x, y: 0 },
    width,
    height: 200,
    data: { label: "组" },
  });

  test("成员入组 / 退组（只换了 parentId）是结构变化", () => {
    const loose = node("n1");
    const inside = { ...node("n1"), parentId: "g" };
    expect(structureKey({ nodes: [group(300), inside], edges: [] })).not.toBe(
      structureKey({ nodes: [group(300), loose], edges: [] }),
    );
  });

  test("缩放组框、挪动组都是结构变化", () => {
    const base = structureKey({ nodes: [group(300)], edges: [] });
    expect(structureKey({ nodes: [group(400)], edges: [] })).not.toBe(base);
    expect(structureKey({ nodes: [group(300, 50)], edges: [] })).not.toBe(base);
  });

  test("改组名、换颜色是内容变化，不是结构变化", () => {
    const renamed: CanvasGroupNode = { ...group(300), data: { label: "第一场", color: "red" } };
    expect(structureKey({ nodes: [renamed], edges: [] })).toBe(
      structureKey({ nodes: [group(300)], edges: [] }),
    );
    expect(contentKey({ nodes: [renamed], edges: [] })).not.toBe(
      contentKey({ nodes: [group(300)], edges: [] }),
    );
  });

  test("撤销时组取快照里的尺寸和位置，不被眼下的覆盖", () => {
    const [restored] = restoreNodes([group(300)], [group(500, 80)]);
    expect(restored).toMatchObject({ width: 300, position: { x: 0, y: 0 } });
  });
});
