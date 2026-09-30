import { useTextGeneration } from "@/hooks/use-text-generation";
import { useTaskNode, type TaskNodeModel } from "@/hooks/use-task-node";
import type { CanvasNodeData } from "@/types";

export type TextNodeModel = TaskNodeModel;

/**
 * 文本节点的业务状态：模型清单读 GET /models?kind=text，
 * 提交走 useTextGeneration，其余（下线判断、schema 参数、上游连线）与视频共用 useTaskNode。
 */
export function useTextNode(id: string, data: CanvasNodeData) {
  const generation = useTextGeneration(id);
  return useTaskNode(id, data, "script", generation);
}
