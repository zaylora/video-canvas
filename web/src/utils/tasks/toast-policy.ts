import type { TaskView } from "@/api/generation-task/type";
import { isTerminalStatus } from "./status";

/**
 * 这次快照变更是不是「刚进入终态」（用来刷新积分）：
 * 必须是此前没见过，或此前还是进行中；
 * 对账时拉回来的陈年终态任务（source=reconcile 且本地没有记录）不算。
 * 生成结果（成功、失败）不弹通知，结果在节点、格子、片段上自己会出来。
 */
export function isFreshTerminalTransition(
  prev: TaskView | undefined,
  next: TaskView,
  source: "live" | "reconcile",
): boolean {
  if (!isTerminalStatus(next.status)) return false;
  if (prev) return !isTerminalStatus(prev.status);
  return source === "live";
}
