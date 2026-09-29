import type { TaskView } from '@/api/generation-task/type'
import type { CanvasNodeData } from '@/types'
import { isTerminalStatus, taskKey } from './status'
import type { TaskMap } from './merge'

/**
 * 对账用：本地认为还在进行、但服务端「进行中」列表里已经没有的任务。
 * 它们多半是断线期间错过了终态推送，得按 id 再查一次拿到最终状态。
 */
export function findStaleActiveIds(local: TaskMap, active: TaskView[]): string[] {
  const alive = new Set(active.map((task) => taskKey(task.id)))
  return Object.values(local)
    .filter((task) => !isTerminalStatus(task.status) && !alive.has(taskKey(task.id)))
    .map((task) => taskKey(task.id))
}

/** 打开画布时要对账的任务：节点还在 running 且带 taskId，本地又没有终态快照 */
export function collectRunningTaskIds(
  nodes: Array<{ data: Pick<CanvasNodeData, 'status' | 'taskId'> }>,
  local: TaskMap = {},
): string[] {
  const ids = new Set<string>()
  for (const node of nodes) {
    const { status, taskId } = node.data
    if (status !== 'running' || !taskId) continue
    const known = local[taskId]
    if (known && isTerminalStatus(known.status)) continue
    ids.add(taskId)
  }
  return [...ids]
}
