import { describe, expect, test } from "bun:test";

import {
  applyChanges,
  diffGraph,
  sameValue,
  unflatten,
  type GraphChange,
  type MergeEdge,
  type MergeNode,
} from "@/utils/canvas/graph-changes";

type N = MergeNode & { position?: { x: number; y: number }; type?: string; selected?: boolean };
type E = MergeEdge;

const node = (id: string, data: Record<string, unknown> = {}, extra: Partial<N> = {}): N => ({
  id,
  type: "canvas",
  position: { x: 0, y: 0 },
  data: { kind: "image", label: id, ...data },
  ...extra,
});

const OPT = {
  makeNode: (f: Record<string, unknown>) => unflatten(f) as unknown as N,
  makeEdge: (f: Record<string, unknown>) => f as unknown as E,
};
const guarded = { guard: true, ...OPT };
const plain = { guard: false, ...OPT };
const g = (nodes: N[], edges: E[] = []) => ({ nodes, edges });

describe("sameValue：null 和缺失视为同一种没有，其余按结构比较", () => {
  test("基本情形", () => {
    expect(sameValue(undefined, null)).toBe(true);
    expect(sameValue(null, "")).toBe(false);
    expect(sameValue({ a: [1, { b: 2 }] }, { a: [1, { b: 2 }] })).toBe(true);
    expect(sameValue({ a: 1 }, { a: 1, b: undefined })).toBe(true);
    expect(sameValue([1], { 0: 1 })).toBe(false);
  });
});

describe("applyChanges：修改字段", () => {
  const update = (
    id: string,
    before: Record<string, unknown>,
    after: Record<string, unknown>,
  ): GraphChange => ({ kind: "node", op: "update", id, before, after });

  test("本地值还等于改动前：写入，其它字段和运行时字段不动", () => {
    const base = g([node("a", { prompt: "旧" }, { selected: true })]);
    const r = applyChanges(
      base,
      [update("a", { "data.prompt": "旧" }, { "data.prompt": "新" })],
      guarded,
    );
    expect(r.nodes[0].data).toMatchObject({ prompt: "新", label: "a" });
    expect(r.nodes[0].selected).toBe(true);
    expect(r.applied).toBe(1);
    expect(r.keptLocal).toBe(0);
    expect((base.nodes[0].data as { prompt: string }).prompt).toBe("旧");
  });

  test("用户在这期间改过的字段保持用户的值，没改的字段照常写入", () => {
    const base = g([node("a", { prompt: "用户改的", label: "旧标题" })]);
    const r = applyChanges(
      base,
      [
        update(
          "a",
          { "data.prompt": "旧", "data.label": "旧标题" },
          { "data.prompt": "Agent 改的", "data.label": "新标题" },
        ),
      ],
      guarded,
    );
    expect(r.nodes[0].data).toMatchObject({ prompt: "用户改的", label: "新标题" });
    expect(r.keptLocal).toBe(1);
  });

  test("改动前没有这个字段：本地也没有时才写（新增字段）", () => {
    const empty = applyChanges(g([node("a")]), [update("a", {}, { "data.model": "m1" })], guarded);
    expect(empty.nodes[0].data).toMatchObject({ model: "m1" });
    const had = applyChanges(
      g([node("a", { model: "用户选的" })]),
      [update("a", {}, { "data.model": "m1" })],
      guarded,
    );
    expect(had.nodes[0].data).toMatchObject({ model: "用户选的" });
  });

  test("改动后字段被删掉（只在 before 里）：本地值等于改动前才删", () => {
    const r = applyChanges(
      g([node("a", { color: "red" })]),
      [update("a", { "data.color": "red" }, {})],
      guarded,
    );
    expect("color" in (r.nodes[0].data as object)).toBe(false);
  });

  test("顶层字段（position、parentId）同样按拍平路径合并", () => {
    const r = applyChanges(
      g([node("a", {}, { position: { x: 1, y: 2 } })]),
      [update("a", { position: { x: 1, y: 2 } }, { position: { x: 9, y: 9 }, parentId: "grp" })],
      guarded,
    );
    expect(r.nodes[0].position).toEqual({ x: 9, y: 9 });
    expect(r.nodes[0].parentId).toBe("grp");
  });

  test("节点已不存在：跳过；guard=false 时直接覆盖", () => {
    expect(applyChanges(g([]), [update("a", {}, { "data.x": 1 })], guarded).applied).toBe(0);
    const r = applyChanges(
      g([node("a", { prompt: "任意" })]),
      [update("a", { "data.prompt": "旧" }, { "data.prompt": "新" })],
      plain,
    );
    expect(r.nodes[0].data).toMatchObject({ prompt: "新" });
  });
});

describe("applyChanges：新建和删除", () => {
  test("新建节点（含 taskId 要报出来）和连线；已存在的不重复", () => {
    const create: GraphChange = {
      kind: "node",
      op: "create",
      id: "b",
      after: {
        id: "b",
        type: "canvas",
        position: { x: 5, y: 5 },
        "data.kind": "video",
        "data.label": "镜头",
        "data.taskId": "123",
      },
    };
    const edge: GraphChange = {
      kind: "edge",
      op: "create",
      id: "e1",
      after: { id: "e1", source: "a", target: "b" },
    };
    const r = applyChanges(g([node("a")]), [create, edge, create, edge], guarded);
    expect(r.nodes.map((n) => n.id)).toEqual(["a", "b"]);
    expect(r.nodes[1].data).toMatchObject({ kind: "video", label: "镜头" });
    expect(r.edges).toHaveLength(1);
    expect(r.taskIds).toEqual(["123"]);
    expect(r.applied).toBe(2);
  });

  test("连线的一端不存在：不加，不留悬空的线", () => {
    const edge: GraphChange = {
      kind: "edge",
      op: "create",
      id: "e1",
      after: { id: "e1", source: "a", target: "ghost" },
    };
    expect(applyChanges(g([node("a")]), [edge], guarded).edges).toHaveLength(0);
  });

  test("同一对节点已经有线（用户自己连的）：不再加第二条", () => {
    const edge: GraphChange = {
      kind: "edge",
      op: "create",
      id: "agent-e",
      after: { id: "agent-e", source: "a", target: "b" },
    };
    const r = applyChanges(
      g([node("a"), node("b")], [{ id: "mine", source: "a", target: "b" }]),
      [edge],
      guarded,
    );
    expect(r.edges.map((e) => e.id)).toEqual(["mine"]);
  });

  test("删除节点：带走连着它的线；删除连线", () => {
    const del: GraphChange = { kind: "node", op: "delete", id: "b" };
    const r = applyChanges(
      g(
        [node("a"), node("b"), node("c")],
        [
          { id: "e1", source: "a", target: "b" },
          { id: "e2", source: "a", target: "c" },
        ],
      ),
      [del],
      guarded,
    );
    expect(r.nodes.map((n) => n.id)).toEqual(["a", "c"]);
    expect(r.edges.map((e) => e.id)).toEqual(["e2"]);
    const r2 = applyChanges(
      g([node("a"), node("c")], [{ id: "e2", source: "a", target: "c" }]),
      [{ kind: "edge", op: "delete", id: "e2" }],
      guarded,
    );
    expect(r2.edges).toHaveLength(0);
  });

  test("normalize 在合并后整理节点顺序", () => {
    const r = applyChanges(
      g([node("a")]),
      [{ kind: "node", op: "create", id: "grp", after: { id: "grp", type: "group" } }],
      {
        ...guarded,
        normalize: (nodes) =>
          [...nodes].sort((x, y) => Number(y.type === "group") - Number(x.type === "group")),
      },
    );
    expect(r.nodes.map((n) => n.id)).toEqual(["grp", "a"]);
  });
});

describe("diffGraph：把两张画布的差别变成一批改动", () => {
  const before = g(
    [node("a", { prompt: "1", color: "red" }), node("b"), node("gone")],
    [{ id: "e1", source: "a", target: "b" }],
  );
  const after = g(
    [node("a", { prompt: "2" }), node("b"), node("new", { kind: "video" })],
    [{ id: "e2", source: "a", target: "new" }],
  );

  test("新建、修改（前后值）、删除、连线增删", () => {
    const changes = diffGraph(before, after);
    const by = (kind: string, op: string, id: string) =>
      changes.find((c) => c.kind === kind && c.op === op && c.id === id);
    expect(by("node", "update", "a")).toMatchObject({
      before: { "data.prompt": "1", "data.color": "red" },
      after: { "data.prompt": "2" },
    });
    expect(by("node", "create", "new")?.after).toMatchObject({ "data.kind": "video" });
    expect(by("node", "delete", "gone")).toBeDefined();
    expect(by("edge", "create", "e2")).toBeDefined();
    expect(by("edge", "delete", "e1")).toBeDefined();
    expect(by("node", "update", "b")).toBeUndefined();
  });

  test("diff 再不加防护地套用，得到目标画布", () => {
    const r = applyChanges(before, diffGraph(before, after), plain);
    const ids = (nodes: N[]) => nodes.map((n) => n.id).sort();
    expect(ids(r.nodes)).toEqual(ids(after.nodes));
    expect(r.edges.map((e) => e.id)).toEqual(["e2"]);
    expect(r.nodes.find((n) => n.id === "a")?.data).toEqual(after.nodes[0].data);
  });

  test("没有差别：没有改动", () => {
    expect(diffGraph(before, before)).toEqual([]);
  });
});

describe("与后端 canvasgraph.Change 同形", () => {
  test("后端发来的 JSON 原样可用", () => {
    const wire = JSON.parse(`[
      {"kind":"node","op":"update","id":"a","before":{"data.prompt":"雨夜"},"after":{"data.prompt":"雨夜便利店","data.model":"seedream"}},
      {"kind":"node","op":"create","id":"n_1","after":{"id":"n_1","type":"canvas","position":{"x":40,"y":80},"data.kind":"image","data.label":"角色"}},
      {"kind":"edge","op":"create","id":"e_1","after":{"id":"e_1","source":"a","target":"n_1"}}
    ]`) as GraphChange[];
    const r = applyChanges(g([node("a", { prompt: "雨夜" })]), wire, guarded);
    expect(r.nodes.map((n) => n.id)).toEqual(["a", "n_1"]);
    expect(r.nodes[0].data).toMatchObject({ prompt: "雨夜便利店", model: "seedream" });
    expect(r.edges).toHaveLength(1);
    expect(r.applied).toBe(3);
  });
});
