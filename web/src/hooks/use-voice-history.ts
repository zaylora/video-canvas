import { useEffect, useMemo, useState } from "react";

import { getGenerationTasksByNode } from "@/api/generation-task";
import type { TaskView } from "@/api/generation-task/type";
import type { ModelInfo } from "@/api/model/type";
import { useTask } from "@/store/tasks";
import { useCurrentCanvasId } from "@/utils/canvas/canvas-id";
import { voiceHistoryRows, type VoiceHistoryRow } from "@/utils/characters/voice-history";
import { voiceNodeId } from "@/utils/characters/voice-task";
import { isTerminalStatus } from "@/utils/tasks/status";

/**
 * 详情页「生成记录」：打开详情时按节点（voice:{节点id}）拉一次这张画布上的声音任务，
 * 节点上的声音任务换了（刚提交、成功后被回填清掉）或到了终态就重新拉一次，新的那条自然排在最前；
 * 正在跑的那条的进度跟着任务库走。记录在服务端，关掉详情、节点卸载都不会丢。
 * @param id 角色卡节点 id
 * @param open 详情是否打开；关着时不请求
 * @param voiceTaskId 节点上记着的声音任务 id
 * @param voiceAssetId 角色眼下声音的素材 id，用来标出「当前」那条
 * @param models 音频模型清单，任务没带模型名时用它查
 * @returns 生成记录的行，新的在前；还没拉到或拉失败时为空数组
 */
export function useVoiceHistory(
  id: string,
  open: boolean,
  voiceTaskId: string | undefined,
  voiceAssetId: string | undefined,
  models: readonly ModelInfo[],
): VoiceHistoryRow[] {
  const canvasId = useCurrentCanvasId();
  const live = useTask(voiceTaskId);
  const settled = live ? isTerminalStatus(live.status) : false;
  const [views, setViews] = useState<TaskView[]>([]);

  useEffect(() => {
    if (!open || !canvasId) return;
    let active = true;
    // 请求失败由全局 toast 提示，这里保留上一次拉到的记录
    getGenerationTasksByNode(canvasId, voiceNodeId(id))
      .then((list) => {
        if (active) setViews(list);
      })
      .catch(() => undefined);
    return () => {
      active = false;
    };
  }, [canvasId, id, open, settled, voiceTaskId]);

  return useMemo(
    () =>
      voiceHistoryRows(views, {
        voiceAssetId,
        modelLabel: (key) => models.find((item) => item.key === key)?.label,
        live: (taskId) => (live && String(live.id) === taskId ? live : undefined),
      }),
    [live, models, views, voiceAssetId],
  );
}
