import type { TaskView } from '@/api/generation-task/type'
import { isTerminalStatus, taskKey } from './status'

export type TaskMap = Record<string, TaskView>

/** 终态任务最多留多少个；进行中的任务永远保留 */
export const MAX_TERMINAL_TASKS = 60

/**
 * 合并一份快照：只有 version 严格更大才生效，重复、乱序、过期的推送都被吸收。
 * 没变化时原样返回同一个 map（引用相等），方便 zustand 跳过通知。
 */
export function mergeTask(
  tasks: TaskMap,
  view: TaskView,
): { tasks: TaskMap; applied: boolean; prev?: TaskView } {
  const key = taskKey(view.id)
  const prev = tasks[key]
  if (prev && prev.version >= view.version) return { tasks, applied: false, prev }
  return { tasks: pruneTasks({ ...tasks, [key]: view }), applied: true, prev }
}

/** 一次合并多份快照，返回真正生效的那些 */
export function mergeTasks(tasks: TaskMap, views: TaskView[]) {
  let next = tasks
  const applied: Array<{ view: TaskView; prev?: TaskView }> = []
  for (const view of views) {
    const result = mergeTask(next, view)
    next = result.tasks
    if (result.applied) applied.push({ view, prev: result.prev })
  }
  return { tasks: next, applied }
}

const finishedAt = (view: TaskView) => Date.parse(view.finished_at ?? view.created_at) || 0

/** 终态任务超过上限时，丢掉最早结束的 */
export function pruneTasks(tasks: TaskMap, max = MAX_TERMINAL_TASKS): TaskMap {
  const terminal = Object.values(tasks).filter((task) => isTerminalStatus(task.status))
  if (terminal.length <= max) return tasks
  const drop = terminal
    .sort((a, b) => finishedAt(a) - finishedAt(b))
    .slice(0, terminal.length - max)
  const next = { ...tasks }
  for (const task of drop) delete next[taskKey(task.id)]
  return next
}
