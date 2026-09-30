import { useCallback, useRef, useState } from "react";
import { useReactFlow } from "@xyflow/react";
import { useParams } from "react-router";

import { cancelGenerationTask, createGenerationTask } from "@/api/generation-task";
import { handleTaskView } from "@/utils/ws/task-events";
import { REMOTE_KIND_OF_NODE } from "@/constants/canvas";
import { useCreditsStore } from "@/store/credits";
import type { CanvasNode } from "@/types";
import { releaseObjectUrl } from "@/utils/canvas/media";
import {
  buildSubmittedPatch,
  describeSubmitError,
  submitWithRetry,
  type SubmitErrorInfo,
} from "@/utils/tasks/submit";

export type SubmitOutcome = { ok: true } | { ok: false; error: SubmitErrorInfo };

/** 走生成任务的节点种类 */
export type TaskNodeKind = keyof typeof REMOTE_KIND_OF_NODE;

/** 提交 / 取消句柄，由各种类的 useXxxGeneration 提供，交给 useTaskNode 使用 */
export type TaskGeneration = ReturnType<typeof useTaskGeneration>;

/**
 * 生成任务节点（视频、文本）共用的提交与取消：
 * 点生成 -> POST /generation-tasks（同一次点击的重试复用同一个 Idempotency-Key）
 * -> 节点写 taskId、status=running，此后完全由任务 store 与回填驱动。
 * 提交失败（积分不足、并发已满、参数错误…）不动节点，把原因交回调用方就地提示。
 */
export function useTaskGeneration(nodeId: string, nodeKind: TaskNodeKind) {
  const { updateNodeData } = useReactFlow<CanvasNode>();
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
    }): Promise<SubmitOutcome> => {
      if (lockRef.current)
        return { ok: false, error: { kind: "unknown", message: "正在提交，请稍候" } };
      if (!canvasId) {
        return { ok: false, error: { kind: "unknown", message: "画布信息缺失，请刷新页面重试" } };
      }
      lockRef.current = true;
      setSubmitting(true);
      // 每次点击一个新 key；submitWithRetry 内部的重试沿用它
      const idempotencyKey = crypto.randomUUID();
      try {
        const view = await submitWithRetry(
          (key) =>
            createGenerationTask(
              {
                kind: REMOTE_KIND_OF_NODE[nodeKind],
                model_id: args.modelKey,
                canvas_id: canvasId,
                node_id: nodeId,
                input: args.input,
              },
              key,
            ),
          idempotencyKey,
        );
        handleTaskView(view, "reconcile");
        releaseObjectUrl(args.currentSrc);
        updateNodeData(nodeId, buildSubmittedPatch(nodeKind, String(view.id)));
        void useCreditsStore.getState().refresh();
        return { ok: true };
      } catch (error) {
        return { ok: false, error: describeSubmitError(error) };
      } finally {
        lockRef.current = false;
        setSubmitting(false);
      }
    },
    [canvasId, nodeId, nodeKind, updateNodeData],
  );

  const cancel = useCallback(
    async (taskId: string) => {
      if (cancelling) return;
      setCancelling(true);
      try {
        const view = await cancelGenerationTask(taskId);
        // 后端可能直接回最新快照；没有的话等 WebSocket / 对账推终态
        if (view && typeof view === "object" && "id" in view) {
          handleTaskView(view, "reconcile");
        }
      } finally {
        setCancelling(false);
      }
    },
    [cancelling],
  );

  return { submit, cancel, submitting, cancelling };
}
