import type { RecordDto } from "@/api/conversation/type";
import type { TaskOutput, TaskView } from "@/api/generation-task/type";
import { derivePendingPhase } from "@/utils/tasks/node-view";
import { isActiveStatus } from "@/utils/tasks/status";

/** 结果格子的状态，由任务状态推出 */
export type CellState =
  /** 提交失败，没有创建任务 */
  | { kind: "submit_error"; message: string }
  /** 记录里有这一格，却查不到任务（被清理或数据损坏） */
  | { kind: "missing" }
  /** 排队中：还没调用上游（与画布节点同一套说法，见 utils/tasks/node-view.ts） */
  | { kind: "queued" }
  /** 生成中：已调用上游，带已耗时（毫秒，未知为 null）和进度（未知为 null） */
  | { kind: "running"; elapsedMs: number | null; progress: number | null }
  /** 即将完成：正在转存素材 */
  | { kind: "finalizing" }
  /** 成功，带第一个产出 */
  | { kind: "done"; output: TaskOutput }
  /** 失败或超时，带给用户看的原因（积分已退回）和任务编号（到后端日志里定位用，后端没给为 null） */
  | { kind: "failed"; message: string; taskRef: string | null }
  /** 已取消（积分已退回） */
  | { kind: "canceled" };

/**
 * 把任务状态翻成格子状态。
 * @param record 所在记录：没有任务时从它的 submitErrors 里找这一格的失败原因
 * @param index 格子序号，从 0 开始
 * @param task 这一格的最新任务快照；没有为 undefined
 * @param now 当前时间（毫秒），算生成中的已耗时用
 * @returns 格子状态
 */
export function deriveCell(
  record: RecordDto,
  index: number,
  task: TaskView | undefined,
  now: number,
): CellState {
  if (!task) {
    const error = record.submitErrors.find((item) => item.index === index);
    return error ? { kind: "submit_error", message: error.message } : { kind: "missing" };
  }
  switch (task.status) {
    case "pending":
    case "queued":
    case "running":
    case "finalizing": {
      // 与画布节点共用同一套阶段判断
      const pending = derivePendingPhase(task, now);
      return pending.phase === "queued"
        ? { kind: "queued" }
        : pending.phase === "running"
          ? { kind: "running", elapsedMs: pending.elapsedMs, progress: pending.progress }
          : { kind: "finalizing" };
    }
    case "succeeded": {
      const output = task.outputs?.[0];
      return output
        ? { kind: "done", output }
        : { kind: "failed", message: "没有生成出结果", taskRef: task.task_ref ?? null };
    }
    case "canceled":
      return { kind: "canceled" };
    case "expired":
      return {
        kind: "failed",
        message: task.error_message || "生成超时",
        taskRef: task.task_ref ?? null,
      };
    default:
      return {
        kind: "failed",
        message: task.error_message || "生成失败",
        taskRef: task.task_ref ?? null,
      };
  }
}

/**
 * 记录里是否还有进行中的任务。
 * @param tasks 这条记录的任务快照（提交失败的格子不在其中）
 * @returns 任一任务未结束为 true
 */
export const recordActive = (tasks: TaskView[]) =>
  tasks.some((task) => isActiveStatus(task.status));

/**
 * 记录的积分：还有任务在跑时是冻结合计（完成后按实际结算）；全部结束后是实际扣费合计，
 * 失败、取消、超时已退回，不算。
 * @param tasks 这条记录的任务快照
 * @returns 积分数，以及它是否还只是冻结额
 */
export function recordCredits(tasks: TaskView[]): { credits: number; frozen: boolean } {
  if (recordActive(tasks)) {
    return { credits: tasks.reduce((sum, task) => sum + task.credits, 0), frozen: true };
  }
  const credits = tasks
    .filter((task) => task.status === "succeeded")
    .reduce((sum, task) => sum + (task.charged_credits ?? task.credits), 0);
  return { credits, frozen: false };
}
