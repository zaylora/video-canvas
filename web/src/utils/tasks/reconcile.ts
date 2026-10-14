import type { TaskView } from "@/api/generation-task/type";
import type { CanvasNodeData } from "@/types";
import { isTerminalStatus, taskKey } from "./status";
import type { TaskMap } from "./merge";

/**
 * 对账用：本地认为还在进行、但服务端「进行中」列表里已经没有的任务。
 * 它们多半是断线期间错过了终态推送，得按 id 再查一次拿到最终状态。
 */
export function findStaleActiveIds(local: TaskMap, active: TaskView[]): string[] {
  const alive = new Set(active.map((task) => taskKey(task.id)));
  return Object.values(local)
    .filter((task) => !isTerminalStatus(task.status) && !alive.has(taskKey(task.id)))
    .map((task) => taskKey(task.id));
}

/**
 * 打开画布时要对账的任务：节点还在 running 且带 taskId，或者批次里还有没结清的任务（pendingTaskIds，
 * 第一张已出、节点是 done 也要对），本地又没有终态快照。
 */
export function collectRunningTaskIds(
  nodes: Array<{ data: Pick<CanvasNodeData, "status" | "taskId" | "pendingTaskIds"> }>,
  local: TaskMap = {},
): string[] {
  const ids = new Set<string>();
  for (const node of nodes) {
    const { status, taskId, pendingTaskIds } = node.data;
    const candidates = [
      ...(pendingTaskIds ?? []),
      ...(status === "running" && taskId ? [taskId] : []),
    ];
    for (const id of candidates) {
      const known = local[id];
      if (known && isTerminalStatus(known.status)) continue;
      ids.add(id);
    }
  }
  return [...ids];
}

/**
 * 服务端确认没有这些任务（被清理或不属于当前用户）时算出要写回节点的补丁，别让节点永远转圈：
 * 从 pendingTaskIds 里移出；taskId 也没了就指向还活着的第一个；一个都不剩且节点还没出过结果（running）就标成失败。
 * 没有受影响的任务返回 null。
 */
export function markTasksGone(
  data: Pick<CanvasNodeData, "status" | "taskId" | "pendingTaskIds">,
  gone: ReadonlySet<string>,
): Partial<CanvasNodeData> | null {
  const pending = data.pendingTaskIds;
  const pendingHit = !!pending?.some((id) => gone.has(id));
  const taskHit = data.status === "running" && !!data.taskId && gone.has(data.taskId);
  if (!pendingHit && !taskHit) return null;
  const rest = (pending ?? []).filter((id) => !gone.has(id));
  if (rest.length === 0 && data.status === "running") {
    return {
      status: "error",
      taskId: undefined,
      pendingTaskIds: undefined,
      error: "任务状态已失效，请重新生成",
    };
  }
  const patch: Partial<CanvasNodeData> = { pendingTaskIds: rest.length ? rest : undefined };
  if (data.taskId && gone.has(data.taskId) && rest.length) patch.taskId = rest[0];
  return patch;
}
