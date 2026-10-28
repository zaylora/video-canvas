import { useEffect } from "react";
import { useReactFlow } from "@xyflow/react";
import { toast } from "sonner";

import { useTask, useTasksStore } from "@/store/tasks";
import type { CanvasNode, CanvasNodeData } from "@/types";
import { applyVoiceResult, voiceTaskView, type VoiceTaskView } from "@/utils/characters/voice-task";
import { reconcileTaskIds } from "@/utils/ws/task-events";

/**
 * 角色卡节点上声音任务的回填（在始终挂载的节点组件里调用一次）：
 * - 任务成功：把产出的音频绑成角色的声音，清掉 voiceTaskId；
 * - 任务取消：清掉 voiceTaskId，提示积分已退回；失败的留着，详情页要显示原因和重试；
 * - 刷新后只有 voiceTaskId、本地没有快照：按 id 对账一次，服务端说没有这个任务就清掉记录，别让声音格一直转圈。
 * 声音任务不占节点的 status / taskId，所以不走画布的通用回填（utils/tasks/backfill）。
 * @param id 角色卡节点 id
 * @param data 节点数据
 * @returns 声音任务此刻的状态，节点据此显示「生成中」
 */
export function useVoiceBackfill(id: string, data: CanvasNodeData): VoiceTaskView {
  const { updateNodeData } = useReactFlow<CanvasNode>();
  const taskId = data.voiceTaskId;
  const view = useTask(taskId);
  const state = voiceTaskView(taskId, view);
  const phase = state.phase;
  const asset = state.phase === "done" ? state.asset : null;

  useEffect(() => {
    if (!taskId) return;
    if (asset) {
      updateNodeData(id, (node) => applyVoiceResult(node.data, asset));
    } else if (phase === "canceled") {
      toast.info("已取消生成声音，积分已退回", { id: `voice-canceled:${taskId}` });
      updateNodeData(id, { voiceTaskId: undefined });
    }
  }, [asset, id, phase, taskId, updateNodeData]);

  useEffect(() => {
    if (!taskId || useTasksStore.getState().tasks[taskId]) return;
    let active = true;
    void reconcileTaskIds([taskId]).then((missing) => {
      if (!active || !missing?.includes(taskId)) return;
      if (useTasksStore.getState().tasks[taskId]) return;
      updateNodeData(id, { voiceTaskId: undefined });
    });
    return () => {
      active = false;
    };
  }, [id, taskId, updateNodeData]);

  return state;
}
