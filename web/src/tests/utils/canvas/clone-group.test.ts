import { describe, expect, test } from "bun:test";

import type { CanvasEdge, FlowNode } from "@/types";
import { cloneGroup, selectionEdges } from "@/utils/canvas/clone-group";

const node = (id: string) =>
  ({
    id,
    type: "script",
    position: { x: 0, y: 0 },
    data: { kind: "script", label: id, status: "idle" },
  }) as unknown as FlowNode;

const edge = (id: string, source: string, target: string) =>
  ({ id, source, target, sourceHandle: "out", targetHandle: "in" }) as CanvasEdge;

const offset = { x: 32, y: 32 };

describe("selectionEdges：复制范围要带的连线", () => {
  const edges = [edge("a", "up", "down"), edge("b", "down", "next"), edge("c", "x", "y")];

  test("带上从范围外连进来的引用连线，不带连出去的和无关的", () => {
    expect(selectionEdges(new Set(["down"]), edges).map((e) => e.id)).toEqual(["a"]);
  });

  test("两头都在范围内的照样带", () => {
    expect(selectionEdges(new Set(["up", "down"]), edges).map((e) => e.id)).toEqual(["a"]);
  });
});

describe("cloneGroup：引用连线", () => {
  test("只复制下游：副本连回原来的上游", () => {
    const source = { nodes: [node("down")], edges: [edge("a", "up", "down")] };
    const got = cloneGroup(source, offset, [node("up"), node("down")]);
    expect(got.edges).toHaveLength(1);
    expect(got.edges[0].source).toBe("up");
    expect(got.edges[0].target).toBe(got.nodes[0].id);
    expect(got.edges[0].sourceHandle).toBe("out");
    expect(got.edges[0].id).not.toBe("a");
  });

  test("上下游一起复制：副本之间连线", () => {
    const source = { nodes: [node("up"), node("down")], edges: [edge("a", "up", "down")] };
    const got = cloneGroup(source, offset, [node("up"), node("down")]);
    const ids = got.nodes.map((n) => n.id);
    expect(ids).not.toContain("up");
    expect(ids).toContain(got.edges[0].source);
    expect(ids).toContain(got.edges[0].target);
  });

  test("上游已不在画布上（跨画布粘贴）：不连", () => {
    const source = { nodes: [node("down")], edges: [edge("a", "up", "down")] };
    expect(cloneGroup(source, offset, [node("other")]).edges).toHaveLength(0);
  });

  test("只是挪剪贴板位置（不给画布节点）：引用连线保留", () => {
    const source = { nodes: [node("down")], edges: [edge("a", "up", "down")] };
    expect(cloneGroup(source, offset).edges[0].source).toBe("up");
  });
});
