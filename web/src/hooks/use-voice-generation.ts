import { useCallback, useRef, useState } from "react";
import { useReactFlow } from "@xyflow/react";

import type { ModelInfo } from "@/api/model/type";
import { useRemoteModels } from "@/hooks/use-models";
import { useTask } from "@/store/tasks";
import type { CanvasNode, CanvasNodeData } from "@/types";
import { useCurrentCanvasId } from "@/utils/canvas/canvas-id";
import { buildVoiceInput, voiceNodeId, voiceTaskView } from "@/utils/characters/voice-task";
import { cancelTasks, submitCanvasTasks } from "@/utils/tasks/gateway";
import { describeSubmitError, settleSubmission } from "@/utils/tasks/submit";

/**
 * 详情页「AI 生成声音」的提交与取消：用音频模型生成一段音频，任务绑在 voice:{节点id} 上，
 * 提交成功后把任务 id 记在节点的 voiceTaskId，结果由 useVoiceBackfill 绑成角色的声音。
 * 没有参数面板：只提交提示词，其余参数取模型默认值；模型收参考图时带上角色主图。
 * @param id 角色卡节点 id
 * @param data 节点数据
 */
export function useVoiceGeneration(id: string, data: CanvasNodeData) {
  const { updateNodeData } = useReactFlow<CanvasNode>();
  const canvasId = useCurrentCanvasId();
  const remote = useRemoteModels("audio");
  const [modelKey, setModelKey] = useState<string | undefined>();
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const lockRef = useRef(false);

  const model: ModelInfo | undefined =
    remote.models.find((item) => item.key === modelKey) ?? remote.models[0];
  const task = voiceTaskView(data.voiceTaskId, useTask(data.voiceTaskId));

  /**
   * 此刻不能生成音色的原因（提交前就能知道的那些）；可以返回 null。
   * 「生成（含音色）」要在提交形象之前先问它，免得形象出了图音色才发现交不了。
   */
  const blockedReason = useCallback(
    (prompt: string): string | null => {
      if (task.phase === "generating") return "音色正在生成";
      if (data.voiceAfterImage?.length) return "出图后会接着生成音色";
      if (!prompt.trim()) return "还没有音色描述";
      if (!model) return "没有可用的音频模型";
      return buildVoiceInput(model.capabilities, prompt).error;
    },
    [data.voiceAfterImage, model, task.phase],
  );

  /**
   * 提交：成功返回 null，失败返回原因（同时记在 error 上给详情页显示）。
   * @param prompt 提示词
   * @param imageAssetId 参考的角色主图，缺省取节点此刻的主图（出图后接着生成时由调用方传新图）
   */
  const submit = useCallback(
    async (prompt: string, imageAssetId = data.assetId): Promise<string | null> => {
      if (lockRef.current) return "正在提交";
      const fail = (reason: string) => {
        setError(reason);
        return reason;
      };
      setError(null);
      if (!canvasId) return fail("画布信息缺失，请刷新页面重试");
      if (!model) return fail("没有可用的音频模型");
      const built = buildVoiceInput(model.capabilities, prompt, imageAssetId);
      if (built.error) return fail(built.error);
      lockRef.current = true;
      setSubmitting(true);
      try {
        const items = await submitCanvasTasks({
          kind: "audio",
          model_id: model.key,
          canvas_id: canvasId,
          node_id: voiceNodeId(id),
          input: built.input,
        });
        const { taskIds, failures } = settleSubmission(items);
        if (taskIds.length === 0) return fail(failures[0]?.message ?? "提交失败，请稍后重试");
        updateNodeData(id, { voiceTaskId: taskIds[0] });
        return null;
      } catch (caught) {
        return fail(describeSubmitError(caught).message);
      } finally {
        lockRef.current = false;
        setSubmitting(false);
      }
    },
    [canvasId, data.assetId, id, model, updateNodeData],
  );

  const cancel = useCallback(async () => {
    if (data.voiceTaskId) await cancelTasks([data.voiceTaskId]);
  }, [data.voiceTaskId]);

  return {
    models: remote.models,
    modelsStatus: remote.status,
    model,
    selectModel: setModelKey,
    task,
    blockedReason,
    submitting,
    error,
    submit,
    cancel,
  };
}

export type VoiceGeneration = ReturnType<typeof useVoiceGeneration>;
