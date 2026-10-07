import type { CanvasPatchDto } from "@/api/agent/type";
import {
  applyChanges,
  diffGraph,
  unflatten,
  type GraphChange,
  type MergeEdge,
  type MergeGraph,
  type MergeNode,
  type MergeOptions,
} from "./graph-changes";

/** 服务端某个版本的画布内容（只看节点和连线，视口是本机的视图状态） */
type Snapshot = { nodes: MergeNode[]; edges: MergeEdge[] };

/** 同步控制器的依赖，都由调用方注入，方便脱离 React 测试 */
export type AgentSyncDeps<N extends MergeNode, E extends MergeEdge> = {
  /** 打开画布时服务端的内容和版本 */
  initial: { graph: Snapshot; version: number };
  /** 拉服务端最新的画布 */
  fetchLatest: () => Promise<{ graph: Snapshot; version: number }>;
  /** 本地当前认的服务端版本 */
  getVersion: () => number;
  /** 别处的改动已并进本地：把版本接到它 */
  mergeVersion: (version: number) => void;
  /** 本地还有没存上的改动 */
  hasUnsaved: () => boolean;
  /** 读本地画布（要是最新的，包括刚合并进去还没渲染的） */
  getLocal: () => MergeGraph<N, E>;
  /**
   * 写回合并后的本地画布。quiet 为 true 表示本地原本没有未保存的改动，这次合并的内容服务端已经有了，
   * 不需要再保存一遍；taskIds 是这批改动里带来的生成任务，调用方要对账；
   * touchedIds 是被新建或修改的节点，调用方可以短暂高亮
   */
  setLocal: (
    graph: MergeGraph<N, E>,
    meta: { quiet: boolean; taskIds: string[]; touchedIds: string[] },
  ) => void;
  /** 这张画布上现在有没有进行中的 Agent 运行 */
  isAgentActive: () => boolean;
  /** 造节点、造连线、整理顺序（前端运行时的默认值） */
  merge: Pick<MergeOptions<N, E>, "makeNode" | "makeEdge" | "normalize">;
};

/** 同步控制器 */
export type AgentSync = {
  /** 收到 Agent 对画布的一次改动 */
  handlePatch: (patch: CanvasPatchDto) => Promise<void>;
  /** 保存撞上 409：别处的改动是 Agent 写的就自动合并，返回合并后的服务端版本；不能合并返回 null */
  autoMerge: () => Promise<number | null>;
  /** 一次保存成功：这份图谱就是服务端在该版本的内容 */
  saved: (graph: Snapshot, version: number) => void;
};

/**
 * Agent 改画布时，前端怎么跟上：
 * - 收到 canvas.patch 且接得上本地版本（revision_before 等于本地版本）：把这批改动并进本地，版本接到 revision_after
 * - 接不上（漏了一条、顺序乱了）：拉最新画布，和「服务端上一次已知内容」比出差异，同样并进本地
 * - 保存撞上 409 且这张画布被 Agent 动过：同上自动合并后重试，不弹冲突窗
 * 并进本地时用户改过的字段保持用户的值（见 applyChanges 的 guard），下一次保存会把它写回服务端，两边最终一致。
 * 所有合并排成一队，不会交错。
 */
export function createAgentSync<N extends MergeNode, E extends MergeEdge>(
  deps: AgentSyncDeps<N, E>,
): AgentSync {
  let base = { graph: deps.initial.graph, version: deps.initial.version };
  let touched = false;
  let tail: Promise<unknown> = Promise.resolve();
  const serial = <T>(fn: () => Promise<T>): Promise<T> => {
    const run = tail.then(fn, fn);
    tail = run.catch(() => undefined);
    return run;
  };

  /** 基准图是服务端内容，直接套用，不需要保护用户的值 */
  const advanceBase = (changes: GraphChange[], version: number) => {
    const r = applyChanges(base.graph as MergeGraph<MergeNode, MergeEdge>, changes, {
      guard: false,
      makeNode: (f) => unflatten(f) as unknown as MergeNode,
      makeEdge: (f) => f as unknown as MergeEdge,
    });
    base = { graph: { nodes: r.nodes, edges: r.edges }, version };
  };

  const applyRemote = (changes: GraphChange[]) => {
    if (changes.length === 0) return;
    const quiet = !deps.hasUnsaved();
    const r = applyChanges(deps.getLocal(), changes, { guard: true, ...deps.merge });
    if (r.applied > 0) {
      const alive = new Set(r.nodes.map((n) => n.id));
      const touchedIds = changes
        .filter((c) => c.kind === "node" && c.op !== "delete" && alive.has(c.id))
        .map((c) => c.id);
      deps.setLocal({ nodes: r.nodes, edges: r.edges }, { quiet, taskIds: r.taskIds, touchedIds });
    }
  };

  /** 拉最新画布并并进本地；服务端没有比本地更新的版本返回 null */
  const resync = async (): Promise<number | null> => {
    const latest = await deps.fetchLatest();
    if (latest.version <= deps.getVersion()) return null;
    applyRemote(diffGraph(base.graph, latest.graph));
    base = { graph: latest.graph, version: latest.version };
    deps.mergeVersion(latest.version);
    return latest.version;
  };

  return {
    handlePatch: (patch) =>
      serial(async () => {
        touched = true;
        const version = deps.getVersion();
        if (patch.revision_after <= version) return; // 已经包含在本地版本里
        if (patch.revision_before === version) {
          applyRemote(patch.changes);
          advanceBase(patch.changes, patch.revision_after);
          deps.mergeVersion(patch.revision_after);
          return;
        }
        // 接不上：拉最新的补齐；拉失败不要紧，下一次改动或保存冲突时会再来
        await resync().catch(() => undefined);
      }),

    autoMerge: () =>
      serial(async () => {
        if (!touched && !deps.isAgentActive()) return null;
        return resync();
      }),

    saved: (graph, version) => {
      if (version > base.version)
        base = { graph: { nodes: graph.nodes, edges: graph.edges }, version };
    },
  };
}
