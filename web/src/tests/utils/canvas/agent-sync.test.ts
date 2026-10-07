import { describe, expect, test } from "bun:test";

import type { CanvasPatchDto } from "@/api/agent/type";
import { createAgentSync, type AgentSyncDeps } from "@/utils/canvas/agent-sync";
import { unflatten, type MergeEdge, type MergeNode } from "@/utils/canvas/graph-changes";

type N = MergeNode & { data: Record<string, unknown> };
const node = (id: string, data: Record<string, unknown> = {}): N => ({
  id,
  data: { kind: "image", label: id, ...data },
});

/** 一个假的前端画布和服务端：本地内容、版本、未保存状态都可以直接摆弄 */
function setup(
  over: {
    local?: N[];
    base?: N[];
    version?: number;
    active?: boolean;
    latest?: { nodes: N[]; version: number };
  } = {},
) {
  const baseNodes = over.base ?? [node("a", { prompt: "旧" })];
  const state = {
    local: { nodes: over.local ?? baseNodes, edges: [] as MergeEdge[] },
    version: over.version ?? 3,
    unsaved: false,
    active: over.active ?? false,
    latest: over.latest ?? { nodes: baseNodes, version: over.version ?? 3 },
    writes: [] as { quiet: boolean; taskIds: string[]; touchedIds: string[] }[],
    fetches: 0,
    failFetch: false,
  };
  const deps: AgentSyncDeps<N, MergeEdge> = {
    initial: { graph: { nodes: baseNodes, edges: [] }, version: state.version },
    fetchLatest: async () => {
      state.fetches++;
      if (state.failFetch) throw new Error("network");
      return { graph: { nodes: state.latest.nodes, edges: [] }, version: state.latest.version };
    },
    getVersion: () => state.version,
    mergeVersion: (v) => (state.version = Math.max(state.version, v)),
    hasUnsaved: () => state.unsaved,
    getLocal: () => state.local,
    setLocal: (g, meta) => {
      state.local = g;
      state.writes.push(meta);
    },
    isAgentActive: () => state.active,
    merge: {
      makeNode: (f) => unflatten(f) as unknown as N,
      makeEdge: (f) => f as unknown as MergeEdge,
    },
  };
  return { state, sync: createAgentSync(deps) };
}

const patch = (
  before: number,
  after: number,
  changes: CanvasPatchDto["changes"],
): CanvasPatchDto => ({
  mutation_id: "m1",
  run_id: "r1",
  canvas_id: "c1",
  kind: "apply_ops",
  revision_before: before,
  revision_after: after,
  changes,
});
const setPrompt = (before: string, after: string) => ({
  kind: "node" as const,
  op: "update" as const,
  id: "a",
  before: { "data.prompt": before },
  after: { "data.prompt": after },
});

describe("canvas.patch：接得上本地版本", () => {
  test("并进本地，版本接到 revision_after；本地没有未保存改动时静默（不用再存）", async () => {
    const { state, sync } = setup();
    await sync.handlePatch(patch(3, 4, [setPrompt("旧", "新")]));
    expect(state.local.nodes[0].data.prompt).toBe("新");
    expect(state.version).toBe(4);
    expect(state.writes).toEqual([{ quiet: true, taskIds: [], touchedIds: ["a"] }]);
    expect(state.fetches).toBe(0);
  });

  test("本地有未保存的改动：不静默，用户改过的字段保持用户的值", async () => {
    const { state, sync } = setup({ local: [node("a", { prompt: "用户改的" })] });
    state.unsaved = true;
    await sync.handlePatch(patch(3, 4, [setPrompt("旧", "Agent 改的")]));
    expect(state.local.nodes[0].data.prompt).toBe("用户改的");
    expect(state.version).toBe(4);
  });

  test("新建的节点带着任务 id 报给调用方对账", async () => {
    const { state, sync } = setup();
    const create = {
      kind: "node" as const,
      op: "create" as const,
      id: "n2",
      after: { id: "n2", type: "canvas", "data.kind": "image", "data.taskId": "99" },
    };
    await sync.handlePatch(patch(3, 4, [create]));
    expect(state.local.nodes.map((n) => n.id)).toEqual(["a", "n2"]);
    expect(state.writes[0].taskIds).toEqual(["99"]);
  });

  test("被新建、修改的节点报给调用方高亮；被删的不报", async () => {
    const { state, sync } = setup({ local: [node("a", { prompt: "旧" }), node("b")] });
    const create = {
      kind: "node" as const,
      op: "create" as const,
      id: "n2",
      after: { id: "n2", type: "canvas", "data.kind": "image" },
    };
    const del = { kind: "node" as const, op: "delete" as const, id: "b" };
    await sync.handlePatch(patch(3, 4, [setPrompt("旧", "新"), create, del]));
    expect(state.writes[0].touchedIds).toEqual(["a", "n2"]);
  });

  test("连续两条按序到达：依次并入", async () => {
    const { state, sync } = setup();
    await Promise.all([
      sync.handlePatch(patch(3, 4, [setPrompt("旧", "一")])),
      sync.handlePatch(patch(4, 5, [setPrompt("一", "二")])),
    ]);
    expect(state.local.nodes[0].data.prompt).toBe("二");
    expect(state.version).toBe(5);
    expect(state.fetches).toBe(0);
  });

  test("已经包含在本地版本里的旧补丁：忽略", async () => {
    const { state, sync } = setup({ version: 6 });
    await sync.handlePatch(patch(3, 4, [setPrompt("旧", "新")]));
    expect(state.writes).toHaveLength(0);
    expect(state.local.nodes[0].data.prompt).toBe("旧");
  });
});

describe("canvas.patch：接不上（漏了一条）", () => {
  test("拉最新画布，和已知的服务端内容比出差异并进本地", async () => {
    const latest = { nodes: [node("a", { prompt: "第三版" }), node("b")], version: 6 };
    const { state, sync } = setup({ latest });
    await sync.handlePatch(patch(5, 6, [setPrompt("第二版", "第三版")]));
    expect(state.fetches).toBe(1);
    expect(state.local.nodes.map((n) => n.id)).toEqual(["a", "b"]);
    expect(state.local.nodes[0].data.prompt).toBe("第三版");
    expect(state.version).toBe(6);
  });

  test("拉取失败：不抛错，版本不动，等下一次", async () => {
    const { state, sync } = setup();
    state.failFetch = true;
    await sync.handlePatch(patch(5, 6, [setPrompt("旧", "新")]));
    expect(state.fetches).toBe(1);
    expect(state.version).toBe(3);
    expect(state.writes).toHaveLength(0);
  });

  test("服务端最新版本不比本地新：什么都不做", async () => {
    const { state, sync } = setup();
    await sync.handlePatch(patch(5, 6, []));
    expect(state.writes).toHaveLength(0);
    expect(state.version).toBe(3);
  });
});

describe("保存撞上 409：自动合并", () => {
  test("Agent 动过这张画布：并进本地，返回服务端版本，不弹冲突窗", async () => {
    const { state, sync } = setup({
      latest: { nodes: [node("a", { prompt: "Agent 改的" })], version: 5 },
    });
    state.active = true;
    expect(await sync.autoMerge()).toBe(5);
    expect(state.local.nodes[0].data.prompt).toBe("Agent 改的");
    expect(state.version).toBe(5);
  });

  test("收到过 Agent 的补丁，运行结束后撞上 409 也一样合并", async () => {
    const { state, sync } = setup({ latest: { nodes: [node("a", { prompt: "旧" })], version: 5 } });
    await sync.handlePatch(patch(3, 4, [setPrompt("旧", "新")]));
    state.latest = { nodes: [node("a", { prompt: "新" }), node("b")], version: 6 };
    expect(await sync.autoMerge()).toBe(6);
    expect(state.local.nodes.map((n) => n.id)).toEqual(["a", "b"]);
  });

  test("没有 Agent 参与（别的标签页改的）：不自动合并，走原来的冲突弹窗", async () => {
    const { state, sync } = setup({
      latest: { nodes: [node("a", { prompt: "别处改的" })], version: 5 },
    });
    expect(await sync.autoMerge()).toBeNull();
    expect(state.fetches).toBe(0);
    expect(state.local.nodes[0].data.prompt).toBe("旧");
  });

  test("服务端没有更新的版本：返回 null", async () => {
    const { state, sync } = setup();
    state.active = true;
    expect(await sync.autoMerge()).toBeNull();
  });
});

describe("saved：用户自己保存成功后，基准跟着走", () => {
  test("之后的差异只算服务端新增的部分，不会把用户刚存的内容当成别人的改动", async () => {
    const mine = [node("a", { prompt: "旧" }), node("mine")];
    const { state, sync } = setup({ local: mine });
    sync.saved({ nodes: mine, edges: [] }, 4);
    state.version = 4;
    state.active = true;
    state.latest = { nodes: [...mine, node("agent")], version: 5 };
    await sync.autoMerge();
    expect(state.local.nodes.map((n) => n.id)).toEqual(["a", "mine", "agent"]);
  });
});
