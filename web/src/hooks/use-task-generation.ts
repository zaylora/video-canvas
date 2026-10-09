import { useCallback, useRef, useState } from "react";
import { useReactFlow } from "@xyflow/react";
import { useParams } from "react-router";

import { REMOTE_KIND_OF_NODE } from "@/constants/canvas";
import type { CanvasEdge, CanvasNode } from "@/types";
import { duplicateNode } from "@/utils/canvas/duplicate";
import { releaseObjectUrl } from "@/utils/canvas/media";
import { cancelTasks, submitCanvasTasks } from "@/utils/tasks/gateway";
import {
  buildSubmittedPatch,
  describeSubmitError,
  type SubmitErrorInfo,
} from "@/utils/tasks/submit";

export type SubmitOutcome = { ok: true } | { ok: false; error: SubmitErrorInfo };

/** 走生成任务的节点种类 */
export type TaskNodeKind = keyof typeof REMOTE_KIND_OF_NODE;

/** 提交 / 取消句柄，由各种类的 useXxxGeneration 提供，交给 useTaskNode 使用 */
export type TaskGeneration = ReturnType<typeof useTaskGeneration>;

/**
 * 生成任务节点共用的提交与取消：
 * 点生成 -> POST /generation-tasks（同一次点击的重试复用同一个 Idempotency-Key）
 * -> 节点写 taskId、status=running，此后完全由任务 store 与回填驱动。
 * 生成数量 N > 1 时，点发送的瞬间先像「复制节点」一样在原节点旁复制出 N − 1 个副本（都处于生成中），
 * 再一次提交 N 个节点；按返回逐个写回：成功的写 taskId，失败的节点显示自己的错误并保留。
 * 只生成 1 个时提交失败不动节点，把原因交回调用方就地提示。
 */
export function useTaskGeneration(nodeId: string, nodeKind: TaskNodeKind) {
  const { updateNodeData, getNode, getNodes, getEdges, setNodes, setEdges } = useReactFlow<
    CanvasNode,
    CanvasEdge
  >();
  const { id: canvasId } = useParams();
  const [submitting, setSubmitting] = useState(false);
  const [cancelling, setCancelling] = useState(false);
  const lockRef = useRef(false);

  /** 复制出 count - 1 个副本并选中它们与原节点，返回按顺序的全部节点 id（原节点在第一个） */
  const fanOut = useCallback(
    (count: number) => {
      const source = getNode(nodeId);
      if (!source || count <= 1) return [nodeId];
      const copies = duplicateNode(source, getNodes(), getEdges(), count - 1);
      const copyIds = new Set(copies.nodes.map((node) => node.id));
      setNodes((nodes) => [
        ...nodes.map((node) => ({ ...node, selected: node.id === nodeId })),
        ...copies.nodes.map((node) => ({
          ...node,
          data: { ...node.data, status: "running" as const },
        })),
      ]);
      setEdges((edges) => [...edges, ...copies.edges]);
      return [nodeId, ...copies.nodes.map((node) => node.id).filter((id) => copyIds.has(id))];
    },
    [getEdges, getNode, getNodes, nodeId, setEdges, setNodes],
  );

  /** 一个节点提交失败：多个节点时把错误写在节点上（保留节点），只有一个时不动节点 */
  const markFailed = useCallback(
    (id: string, message: string, multiple: boolean) => {
      if (multiple) updateNodeData(id, { status: "error", error: message, taskId: undefined });
    },
    [updateNodeData],
  );

  const submit = useCallback(
    async (args: {
      modelKey: string;
      input: Record<string, unknown>;
      /** 节点当前的素材地址，重新生成会顶掉它，本地 blob 要还回去 */
      currentSrc?: string | null;
      /** 生成数量：拆成几个任务（几个节点），默认 1 */
      count?: number;
      /** 前端算出的每个任务的积分；与后端冻结额不一致时记日志（两份计价算法漂移了），以后端为准 */
      expectedCredits?: number;
    }): Promise<SubmitOutcome> => {
      if (lockRef.current)
        return { ok: false, error: { kind: "unknown", message: "正在提交，请稍候" } };
      if (!canvasId) {
        return { ok: false, error: { kind: "unknown", message: "画布信息缺失，请刷新页面重试" } };
      }
      lockRef.current = true;
      setSubmitting(true);
      // 点发送的瞬间 N 个节点就在画布上
      const nodeIds = fanOut(Math.max(1, args.count ?? 1));
      const multiple = nodeIds.length > 1;
      if (multiple) updateNodeData(nodeId, { status: "running", taskId: undefined, error: null });
      try {
        // 幂等键、重试、快照入库、刷新余额都在统一入口里（utils/tasks/gateway.ts）
        const items = await submitCanvasTasks({
          kind: REMOTE_KIND_OF_NODE[nodeKind],
          model_id: args.modelKey,
          canvas_id: canvasId,
          node_id: nodeId,
          node_ids: nodeIds,
          input: args.input,
        });
        let own: SubmitErrorInfo | null = null;
        for (const [index, id] of nodeIds.entries()) {
          const item = items.find((entry) => entry.node_id === id) ?? items[index];
          if (item?.task) {
            if (args.expectedCredits !== undefined && item.task.credits !== args.expectedCredits) {
              console.warn("[pricing] 前端计价与后端冻结额不一致，以后端为准", {
                model: args.modelKey,
                expected: args.expectedCredits,
                frozen: item.task.credits,
                taskId: item.task.id,
              });
            }
            if (id === nodeId) releaseObjectUrl(args.currentSrc);
            const taskId = String(item.task.id);
            updateNodeData(id, (node) => buildSubmittedPatch(nodeKind, taskId, node.data));
            continue;
          }
          const info = describeSubmitError(item?.error ?? null);
          markFailed(id, info.message, multiple);
          if (id === nodeId) own = info;
        }
        return own ? { ok: false, error: own } : { ok: true };
      } catch (error) {
        // 请求本身失败（未登录、参数不合法、网络…）：每个节点都显示这条错误，节点保留
        const info = describeSubmitError(error);
        for (const id of nodeIds) markFailed(id, info.message, multiple);
        return { ok: false, error: info };
      } finally {
        lockRef.current = false;
        setSubmitting(false);
      }
    },
    [canvasId, fanOut, markFailed, nodeId, nodeKind, updateNodeData],
  );

  const cancel = useCallback(
    async (taskId: string) => {
      if (cancelling) return;
      setCancelling(true);
      try {
        await cancelTasks([taskId]);
      } finally {
        setCancelling(false);
      }
    },
    [cancelling],
  );

  return { submit, cancel, submitting, cancelling };
}
