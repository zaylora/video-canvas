import { useCallback, useRef, useState } from "react";
import { useReactFlow } from "@xyflow/react";
import { useParams } from "react-router";

import {
  cancelGenerationTask,
  createGenerationTask,
} from "@/api/generation-task";
import { handleTaskView } from "@/utils/ws/task-events";
import { useCreditsStore } from "@/store/credits";
import type { CanvasNode } from "@/types";
import { releaseObjectUrl } from "@/utils/canvas/media";
import {
  describeSubmitError,
  submitWithRetry,
  type SubmitErrorInfo,
} from "@/utils/tasks/submit";

export type SubmitOutcome =
  | { ok: true }
  | { ok: false; error: SubmitErrorInfo };

/**
 * 视频节点的提交与取消：
 * 点生成 -> POST /generation-tasks（同一次点击的重试复用同一个 Idempotency-Key）
 * -> 节点写 taskId、status=running，此后完全由任务 store 与回填驱动。
 * 提交失败（积分不足、并发已满、参数错误…）不动节点，把原因交回调用方就地提示。
 */
export function useVideoGeneration(nodeId: string) {
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
      if (lockRef.current) return { ok: false, error: { kind: "unknown", message: "正在提交，请稍候" } };
      const canvas = Number(canvasId);
      if (!canvasId || !Number.isFinite(canvas)) {
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
                kind: "video",
                model_id: args.modelKey,
                canvas_id: canvas,
                node_id: nodeId,
                input: args.input,
              },
              key,
            ),
          idempotencyKey,
        );
        handleTaskView(view, "reconcile");
        releaseObjectUrl(args.currentSrc);
        updateNodeData(nodeId, {
          taskId: String(view.id),
          status: "running",
          src: null,
          assetId: undefined,
          mediaType: undefined,
          uploaded: false,
          fileName: undefined,
          error: null,
        });
        void useCreditsStore.getState().refresh();
        return { ok: true };
      } catch (error) {
        return { ok: false, error: describeSubmitError(error) };
      } finally {
        lockRef.current = false;
        setSubmitting(false);
      }
    },
    [canvasId, nodeId, updateNodeData],
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
