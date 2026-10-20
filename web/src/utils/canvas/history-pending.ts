import type { TaskView } from "@/api/generation-task/type";
import { derivePendingPhase } from "@/utils/tasks/node-view";
import { pendingCopy } from "@/utils/tasks/pending-copy";
import { taskKey } from "@/utils/tasks/status";

/**
 * 历史记录弹窗里“还没出结果”的占位格标题，和画布节点、首页结果格子用同一套说法：
 * 还没提交给上游的是「排队中」，已经在生成的是「生成中」，转存中是「即将完成」。
 * @param ids 这些占位格对应的任务 ID，按显示顺序
 * @param tasks 任务库快照，以 taskKey 为键
 * @param now 当前时间（毫秒）
 * @param count 没有任务 ID 时要显示几个占位格（例如旧数据只记了“还在生成”）
 * @returns 每个占位格的标题；任务快照还没到时按「生成中」，不猜排队
 */
export function pendingTileTitles(
  ids: readonly (number | string)[],
  tasks: Readonly<Record<string, TaskView>>,
  now: number,
  count = 0,
): string[] {
  if (ids.length === 0) return Array.from({ length: count }, () => "生成中");
  return ids.map((id) => {
    const task = tasks[taskKey(id)];
    return task ? pendingCopy(derivePendingPhase(task, now), "image").title : "生成中";
  });
}
