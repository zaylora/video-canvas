import { useEffect, useState, type Dispatch, type RefObject, type SetStateAction } from "react";

import type { CanvasDetailDto, CanvasGraphDto } from "@/api/canvas/type";
import { getCanvas } from "@/api/canvas";
import { ANIMATED_EDGE_OPTIONS } from "@/constants/canvas";
import { useAgentStore } from "@/store/agent";
import { useAgentHighlight } from "@/store/agent-highlight";
import type { CanvasEdge, FlowNode } from "@/types";
import { subscribeCanvasPatch } from "@/utils/agent/patch-bus";
import { activeRunId } from "@/utils/agent/session-state";
import { createAgentSync } from "@/utils/canvas/agent-sync";
import { deserializeGraph } from "@/utils/canvas/canvas-persistence";
import { normalizeFlowNodes } from "@/utils/canvas/group";
import { unflatten, type MergeEdge } from "@/utils/canvas/graph-changes";
import { reconcileTaskIds } from "@/utils/ws/task-events";

/** 画布页交给同步的东西：当前画布内容（要是最新的）、写回的方法、保存的版本 */
export type AgentCanvasSyncArgs = {
  /** 打开时拿到的画布（服务端的内容和版本） */
  canvas: CanvasDetailDto;
  /** 本地当前认的服务端版本 */
  getVersion: () => number;
  /** 别处的改动已并进本地：把版本接到它 */
  mergeVersion: (version: number) => void;
  /** 本地还有没存上的改动 */
  hasUnsaved: () => boolean;
  /** 最新的节点（合并后立刻更新，不等渲染） */
  nodesRef: RefObject<FlowNode[]>;
  /** 最新的连线（同上） */
  edgesRef: RefObject<CanvasEdge[]>;
  /** 写节点 */
  setNodes: Dispatch<SetStateAction<FlowNode[]>>;
  /** 写连线 */
  setEdges: Dispatch<SetStateAction<CanvasEdge[]>>;
  /** 这一轮节点变化不需要保存 */
  skipNextSave: () => void;
  /** 把服务端已经有的任务 id 记为已知，免得当成「刚提交」立刻再存一遍 */
  markTasksKnown: (ids: string[]) => void;
  /** 清空撤销栈 */
  resetHistory: () => void;
};

/**
 * 把 Agent 对这张画布的改动并进用户正在看的画布：订阅 canvas.patch，保存撞上 409 时自动合并。
 * 合并规则和时序在 createAgentSync 里；这里只负责接上画布页的状态。
 * @returns 给保存用的 autoMerge 和 saved 回调（引用稳定）
 */
export function useAgentCanvasSync(args: AgentCanvasSyncArgs) {
  const [sync] = useState(() =>
    createAgentSync<FlowNode, CanvasEdge & MergeEdge>({
      initial: { graph: args.canvas.graph, version: args.canvas.version },
      fetchLatest: async () => {
        const latest = await getCanvas(args.canvas.id);
        return { graph: latest.graph, version: latest.version };
      },
      getVersion: args.getVersion,
      mergeVersion: args.mergeVersion,
      hasUnsaved: args.hasUnsaved,
      getLocal: () => ({ nodes: args.nodesRef.current, edges: args.edgesRef.current }),
      setLocal: (graph, { quiet, taskIds, touchedIds }) => {
        args.nodesRef.current = graph.nodes;
        args.edgesRef.current = graph.edges;
        if (quiet) args.skipNextSave();
        args.markTasksKnown(taskIds);
        args.setNodes(graph.nodes);
        args.setEdges(graph.edges);
        args.resetHistory();
        useAgentHighlight.getState().touch(touchedIds);
        // 批准生成后绑定的任务可能在补丁到达前就完成了，主动对账一次
        if (taskIds.length > 0) void reconcileTaskIds(taskIds);
      },
      isAgentActive: () => {
        const { sessionsByCanvas, sessions } = useAgentStore.getState();
        return (sessionsByCanvas[args.canvas.id] ?? []).some(
          (s) => sessions[s.id] && activeRunId(sessions[s.id]) !== null,
        );
      },
      merge: {
        makeNode: (fields) =>
          deserializeGraph({
            nodes: [unflatten(fields) as unknown as CanvasGraphDto["nodes"][number]],
          }).nodes[0],
        makeEdge: (fields) => ({ ...fields, ...ANIMATED_EDGE_OPTIONS }) as unknown as CanvasEdge,
        normalize: normalizeFlowNodes,
      },
    }),
  );

  useEffect(
    () => subscribeCanvasPatch(args.canvas.id, (patch) => void sync.handlePatch(patch)),
    [args.canvas.id, sync],
  );

  return sync;
}
