import { useCallback, useRef, useState } from "react";
import { useReactFlow } from "@xyflow/react";
import { useParams } from "react-router";
import { toast } from "sonner";

import { REMOTE_KIND_OF_NODE } from "@/constants/canvas";
import type { CanvasEdge, CanvasNode } from "@/types";
import { releaseObjectUrl } from "@/utils/canvas/media";
import { cancelTasks, submitCanvasTasks } from "@/utils/tasks/gateway";
import {
  buildSubmittedPatch,
  describeSubmitError,
  settleSubmission,
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
 * 生成数量 N > 1 时一次请求提交 N 个任务，**都绑在这个节点上**（node_ids 全是它自己，不再复制出副本节点）：
 * 成功创建的任务写进 pendingTaskIds，陆续回填成这个节点的历史版本（docs/design/多结果归并）。
 * 只有一部分任务提交成功（积分只够几个、并发已满）时，成功的照常生成，失败的数量用 toast 说明；
 * 一个都没成功时节点不动，把原因交回调用方就地提示。
 */
export function useTaskGeneration(nodeId: string, nodeKind: TaskNodeKind) {
  const { updateNodeData } = useReactFlow<CanvasNode, CanvasEdge>();
  const { id: canvasId } = useParams();
  const [submitting, setSubmitting] = useState(false);
  const [cancelling, setCancelling] = useState(false);
  const lockRef = useRef(false);

  const submit = useCallback(
    async (args: {
      modelKey: string;
      input: Record<string, unknown>;
      /** 节点当前的素材地址，重新生成会顶掉它，本地 blob 要还回去 */
      currentSrc?: string | null;
      /** 生成数量：拆成几个任务（都绑在这个节点上），默认 1 */
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
      const count = Math.max(1, args.count ?? 1);
      try {
        // 幂等键、重试、快照入库、刷新余额都在统一入口里（utils/tasks/gateway.ts）
        const items = await submitCanvasTasks({
          kind: REMOTE_KIND_OF_NODE[nodeKind],
          model_id: args.modelKey,
          canvas_id: canvasId,
          node_id: nodeId,
          node_ids: Array.from({ length: count }, () => nodeId),
          input: args.input,
        });
        for (const item of items) {
          if (
            item.task &&
            args.expectedCredits !== undefined &&
            item.task.credits !== args.expectedCredits
          ) {
            console.warn("[pricing] 前端计价与后端冻结额不一致，以后端为准", {
              model: args.modelKey,
              expected: args.expectedCredits,
              frozen: item.task.credits,
              taskId: item.task.id,
            });
          }
        }
        const { taskIds, failures } = settleSubmission(items);
        if (taskIds.length === 0) {
          return {
            ok: false,
            error: failures[0] ?? { kind: "unknown", message: "提交失败，请稍后重试" },
          };
        }
        releaseObjectUrl(args.currentSrc);
        updateNodeData(nodeId, (node) => buildSubmittedPatch(nodeKind, taskIds, node.data));
        if (failures.length > 0) {
          toast.warning(`有 ${failures.length} 张没能提交：${failures[0].message}`);
        }
        return { ok: true };
      } catch (error) {
        // 请求本身失败（未登录、参数不合法、网络…）：节点不动，原因交回调用方就地提示
        return { ok: false, error: describeSubmitError(error) };
      } finally {
        lockRef.current = false;
        setSubmitting(false);
      }
    },
    [canvasId, nodeId, nodeKind, updateNodeData],
  );

  /** 取消任务；传多个（节点的全部进行中任务）时逐个取消 */
  const cancel = useCallback(
    async (taskIds: string | string[]) => {
      if (cancelling) return;
      setCancelling(true);
      try {
        await cancelTasks(Array.isArray(taskIds) ? taskIds : [taskIds]);
      } finally {
        setCancelling(false);
      }
    },
    [cancelling],
  );

  return { submit, cancel, submitting, cancelling };
}
