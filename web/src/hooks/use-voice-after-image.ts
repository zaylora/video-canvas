import { useEffect, useRef } from "react";
import { useReactFlow } from "@xyflow/react";
import { toast } from "sonner";

import type { CanvasNode, CanvasNodeData } from "@/types";
import { voiceAfterImageStep } from "@/utils/characters/voice-task";

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
