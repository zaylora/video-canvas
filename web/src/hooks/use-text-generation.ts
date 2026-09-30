import { useTaskGeneration } from "@/hooks/use-task-generation";

/**
 * 文本节点的提交与取消：走后端生成任务（kind 为 text），
 * 与视频共用同一套提交流程（Idempotency-Key 复用、提交失败交回调用方就地提示）。
 * 任务状态由 WebSocket / 对账写进任务 store，再由 useTaskBackfill 把正文回填到 data.text；
 * 节点被删则回填自然忽略，重复点击由提交锁挡住，重新生成以最新的 taskId 为准。
 */
export const useTextGeneration = (nodeId: string) => useTaskGeneration(nodeId, "script");
