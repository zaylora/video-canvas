import type { TaskStatus, TaskView } from "@/api/generation-task/type";

/** 终态：进入后不会再变 */
export const TERMINAL_STATUSES: readonly TaskStatus[] = [
  "succeeded",
  "failed",
  "canceled",
  "expired",
];

export const isTerminalStatus = (status: TaskStatus) => TERMINAL_STATUSES.includes(status);

export const isActiveStatus = (status: TaskStatus) => !isTerminalStatus(status);

/**
 * 能不能取消：只有还没提交给上游的（pending 且没有提交时间，即界面上的「排队中」）才行。
 * 一旦调用了上游（同步请求已发出、平台已受理、生成中、转存中）就取消不了：上游照常生成，取消退款等于白拿一次生成。
 * 和后端 GenerationTaskService.Cancel 的规则一致，后端仍会再判断一次。
 */
export const isCancelableTask = (task: Pick<TaskView, "status" | "submitted_at">) =>
  task.status === "pending" && !task.submitted_at;

/**
 * 从一批任务 id 里挑出该去取消的：本地已有快照且不可取消的跳过；
 * 没有快照的保留（快照可能还没到，交给后端判断）。
 * @param ids 任务 ID
 * @param tasks 任务库快照，以 taskKey 为键
 */
export const cancelableTaskIds = <T extends string | number>(
  ids: readonly T[],
  tasks: Readonly<Record<string, Pick<TaskView, "status" | "submitted_at">>>,
): T[] =>
  ids.filter((id) => {
    const task = tasks[taskKey(id)];
    return !task || isCancelableTask(task);
  });

/** 后端 id 是数字，前端一律当字符串用 */
export const taskKey = (id: number | string) => String(id);

/** 任务的第一份产物；没有产物时为 undefined */
export const firstOutput = (view: TaskView) => view.outputs?.[0];

/**
 * 文本任务的正文：取第一份带非空 text 的产出。
 * 原样返回，不做 trim，正文里的换行与缩进要保留。
 * @param view 任务快照
 * @returns 正文；没有文本产出或全是空白时为 undefined
 */
export const outputText = (view: TaskView): string | undefined =>
  view.outputs?.find((output) => typeof output.text === "string" && output.text.trim() !== "")
    ?.text;
