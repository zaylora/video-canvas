import { useContext, useEffect, useRef, useState } from "react";
import { useReactFlow } from "@xyflow/react";
import { toast } from "sonner";

import type { CanvasNode, CanvasNodeData } from "@/types";
import { useTask } from "@/store/tasks";
import { CanvasGenerationContext, useCurrentCanvasId } from "@/utils/canvas/canvas-id";
import { voiceAfterImageStep } from "@/utils/characters/voice-task";
import { getCurrentUserId } from "@/utils/storage/user-id";
import { reconcileTaskIds } from "@/utils/ws/task-events";
import { isTerminalStatus } from "@/utils/tasks/status";

import type { VoiceGeneration } from "./use-voice-generation";

/**
 * 角色卡「生成（含音色）」的后半程（在始终挂载的节点组件里调用一次）：
 * 节点记着 voiceAfterImage 时，等这次的形象出图，用新主图接着生成音色；形象失败或被取消就放弃并提示。
 * 不管哪种结果都先删掉记录，一份记录只处理一次。
 * @param id 角色卡节点 id
 * @param data 节点数据
 * @param voiceGen 节点上那一份声音生成
 */
export function useVoiceAfterImage(id: string, data: CanvasNodeData, voiceGen: VoiceGeneration) {
  const { updateNodeData } = useReactFlow<CanvasNode>();
  const step = voiceAfterImageStep(data);
  const kind = step?.kind;
  const imageAssetId = step?.kind === "submit" ? step.imageAssetId : undefined;
  const reason = step?.kind === "abort" ? step.reason : undefined;
  const key = data.voiceAfterImage?.join(",");

  // 提交时要读最新的提示词和模型，但它们变了不该重新触发
  const latest = useRef({ voiceGen, prompt: data.character?.voicePrompt ?? "" });
  useEffect(() => {
    latest.current = { voiceGen, prompt: data.character?.voicePrompt ?? "" };
  });
  // 开发模式下 effect 会跑两遍，同一份记录只处理一次
  const handled = useRef<string | undefined>(undefined);

  useEffect(() => {
    if (!key || !kind || kind === "wait" || handled.current === key) return;
    handled.current = key;
    updateNodeData(id, { voiceAfterImage: undefined });
    if (kind === "abort") {
      toast.info(reason, { id: `voice-after-image:${key}` });
      return;
    }
    const { voiceGen: gen, prompt } = latest.current;
    void gen.submit(prompt, imageAssetId).then((failed) => {
      if (failed) toast.warning(`音色没能生成：${failed}`, { id: `voice-after-image:${key}` });
    });
  }, [id, imageAssetId, key, kind, reason, updateNodeData]);
}

/** 由提交形象的账号在本机续接音色，等待协作版本确认该形象后才发起。 */
export function useCollaborativeVoiceAfterImage(
  id: string,
  data: CanvasNodeData,
  voiceGen: VoiceGeneration,
) {
  const collaboration = useContext(CanvasGenerationContext);
  const canvas = useCurrentCanvasId();
  const user = getCurrentUserId();
  const key =
    collaboration && canvas && user
      ? `canvas-voice:${user}:${canvas}:${collaboration.epoch}:${id}`
      : undefined;
  const [local, setLocal] = useState<string>();
  const taskId = key ? (local ?? readContinuation(key)) : undefined;
  const task = useTask(taskId);
  const { submit } = voiceGen;
  const prompt = data.character?.voicePrompt ?? "";
  const handled = useRef<string | undefined>(undefined);

  useEffect(() => {
    if (taskId) void reconcileTaskIds([taskId]);
  }, [taskId]);

  useEffect(() => {
    if (!key || !taskId || handled.current === taskId || !collaboration?.canGenerate) return;
    const ready = data.characterImageTaskId === taskId && data.status === "done" && data.assetId;
    const failed = task && isTerminalStatus(task.status) && task.status !== "succeeded";
    if (!ready && !failed) return;
    handled.current = taskId;
    if (ready) {
      void submit(prompt, ready, `voice-after-image:${taskId}`).then((reason) => {
        if (readContinuation(key) === taskId) writeContinuation(key);
        setLocal((current) => (current === taskId ? undefined : current));
        if (reason) toast.warning(`音色没能生成：${reason}`);
      });
    } else {
      writeContinuation(key);
      setLocal(undefined);
    }
  }, [
    collaboration?.canGenerate,
    data.assetId,
    data.characterImageTaskId,
    data.status,
    key,
    prompt,
    submit,
    task,
    taskId,
  ]);

  return {
    pending: Boolean(taskId),
    remember(taskIds: string[]) {
      const taskId = taskIds[0];
      if (!key || !taskId) return;
      writeContinuation(key, taskId);
      setLocal(taskId);
    },
  };
}

function readContinuation(key: string): string | undefined {
  try {
    return localStorage.getItem(key) ?? undefined;
  } catch {
    return undefined;
  }
}

function writeContinuation(key: string, taskId?: string) {
  try {
    if (taskId) localStorage.setItem(key, taskId);
    else localStorage.removeItem(key);
  } catch {
    return;
  }
}
