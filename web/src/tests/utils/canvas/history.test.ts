import { describe, expect, test } from "bun:test";

import type { CanvasEdge, CanvasGroupNode, CanvasNode, CanvasNodeData, FlowNode } from "@/types";
import { unflatten, type GraphChange, type MergeEdge } from "@/utils/canvas/graph-changes";
import { contentKey, rebaseSnapshot, restoreNodes, structureKey } from "@/utils/canvas/history";

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

  test("进行中的任务集合属于任务字段：撤销不倒回它", () => {
    const [kept] = restoreNodes(
      [node("n1")],
      [node("n1", { status: "done", pendingTaskIds: ["2", "3"] })],
    );
    expect(kept.data.pendingTaskIds).toEqual(["2", "3"]);
    const [dropped] = restoreNodes(
      [node("n1", { pendingTaskIds: ["2"] })],
      [node("n1", { status: "done" })],
    );
    expect(dropped.data.pendingTaskIds).toBeUndefined();
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

describe("rebaseSnapshot：Agent 的改动并进撤销栈里的快照", () => {
  const merge = {
    makeNode: (fields: Record<string, unknown>) => unflatten(fields) as unknown as FlowNode,
    makeEdge: (fields: Record<string, unknown>) => fields as unknown as CanvasEdge & MergeEdge,
  };
  const edge = (id: string, source: string, target: string) =>
    ({ id, source, target }) as unknown as CanvasEdge;
  const update = (id: string, before: object, after: object): GraphChange => ({
    kind: "node",
    op: "update",
    id,
    before: before as Record<string, unknown>,
    after: after as Record<string, unknown>,
  });

  test("Agent 改的字段：快照里还是改动前的值就换成改动后的", () => {
    const snap = { nodes: [node("a", { prompt: "旧" })], edges: [] };
    const next = rebaseSnapshot(
      snap,
      [update("a", { "data.prompt": "旧" }, { "data.prompt": "新" })],
      merge,
    );
    expect(next.nodes[0].data.prompt).toBe("新");
  });

  test("快照里是用户自己的值（和改动前不同）：保持不动", () => {
    const snap = { nodes: [node("a", { prompt: "用户的" })], edges: [] };
    const next = rebaseSnapshot(
      snap,
      [update("a", { "data.prompt": "旧" }, { "data.prompt": "新" })],
      merge,
    );
    expect(next.nodes[0].data.prompt).toBe("用户的");
  });

  test("Agent 新建的节点和连线加进快照；两端不在快照里的连线跳过", () => {
    const snap = { nodes: [node("a")], edges: [] };
    const changes: GraphChange[] = [
      {
        kind: "node",
        op: "create",
        id: "n2",
        after: { id: "n2", type: "canvas", "data.kind": "image" },
      },
      { kind: "edge", op: "create", id: "e1", after: { id: "e1", source: "a", target: "n2" } },
      { kind: "edge", op: "create", id: "e2", after: { id: "e2", source: "a", target: "gone" } },
    ];
    const next = rebaseSnapshot(snap, changes, merge);
    expect(next.nodes.map((n) => n.id)).toEqual(["a", "n2"]);
    expect(next.edges.map((e) => e.id)).toEqual(["e1"]);
  });

  test("Agent 删掉的节点从快照里去掉，连着它的线一起走", () => {
    const snap = { nodes: [node("a"), node("b")], edges: [edge("e", "a", "b")] };
    const next = rebaseSnapshot(snap, [{ kind: "node", op: "delete", id: "b" }], merge);
    expect(next.nodes.map((n) => n.id)).toEqual(["a"]);
    expect(next.edges).toEqual([]);
  });

  test("这批改动和快照无关：原样返回同一个对象", () => {
    const snap = { nodes: [node("a")], edges: [] };
    const next = rebaseSnapshot(
      snap,
      [update("zzz", { "data.prompt": "a" }, { "data.prompt": "b" })],
      merge,
    );
    expect(next).toBe(snap);
  });

  test("不改入参快照", () => {
    const snap = { nodes: [node("a", { prompt: "旧" })], edges: [] };
    rebaseSnapshot(snap, [update("a", { "data.prompt": "旧" }, { "data.prompt": "新" })], merge);
    expect(snap.nodes[0].data.prompt).toBe("旧");
  });

  test("撤销到换过基准的快照，Agent 刚改的内容和新建的节点都还在", () => {
    // 用户先改了 b 的位置（快照记下改之前的画布），随后 Agent 改了 a 的提示词并新建了 n2
    const snapshot = { nodes: [node("a", { prompt: "旧" }), node("b", {}, 0)], edges: [] };
    const present = [node("a", { prompt: "新" }), node("b", {}, 80), node("n2")];
    const changes: GraphChange[] = [
      update("a", { "data.prompt": "旧" }, { "data.prompt": "新" }),
      {
        kind: "node",
        op: "create",
        id: "n2",
        after: { id: "n2", type: "canvas", "data.kind": "image" },
      },
    ];
    const rebased = rebaseSnapshot(snapshot, changes, merge);
    const restored = restoreNodes(rebased.nodes, present);
    expect(restored.map((n) => n.id)).toEqual(["a", "b", "n2"]);
    expect(restored[0].data.prompt).toBe("新");
    expect(restored[1].position.x).toBe(0);
  });
});
