import { useVideoGeneration } from "@/hooks/use-video-generation";
import { useTaskNode, type TaskNodeModel } from "@/hooks/use-task-node";
import type { CanvasNodeData } from "@/types";

export type { PendingModelSwitch } from "@/hooks/use-task-node";
export type VideoNodeModel = TaskNodeModel;

/** 视频节点的业务状态，具体见 useTaskNode */
export function useVideoNode(id: string, data: CanvasNodeData) {
  const generation = useVideoGeneration(id);
  return useTaskNode(id, data, "video", generation);
}
