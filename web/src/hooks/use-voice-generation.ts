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
 * 没有参数面板：只提交提示词，其余参数取模型默认值。
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

  /** 提交：成功返回 null，失败返回原因（同时记在 error 上给详情页显示） */
  const submit = useCallback(
    async (prompt: string): Promise<string | null> => {
      if (lockRef.current) return "正在提交";
      const fail = (reason: string) => {
        setError(reason);
        return reason;
      };
      setError(null);
      if (!canvasId) return fail("画布信息缺失，请刷新页面重试");
      if (!model) return fail("没有可用的音频模型");
      const built = buildVoiceInput(model.capabilities, prompt);
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
    [canvasId, id, model, updateNodeData],
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
    submitting,
    error,
    submit,
    cancel,
  };
}

export type VoiceGeneration = ReturnType<typeof useVoiceGeneration>;
