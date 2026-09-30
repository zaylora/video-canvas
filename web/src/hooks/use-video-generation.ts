import { useTaskGeneration } from "@/hooks/use-task-generation";

/** 视频节点的提交与取消，具体流程见 useTaskGeneration */
export const useVideoGeneration = (nodeId: string) => useTaskGeneration(nodeId, "video");
