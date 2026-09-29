import type { TaskStatus, TaskView } from '@/api/generation-task/type'

/** 终态：进入后不会再变 */
export const TERMINAL_STATUSES: readonly TaskStatus[] = ['succeeded', 'failed', 'canceled', 'expired']

export const isTerminalStatus = (status: TaskStatus) => TERMINAL_STATUSES.includes(status)

export const isActiveStatus = (status: TaskStatus) => !isTerminalStatus(status)

/** 排队 / 生成中都可以取消，转存中不行 */
export const isCancelableStatus = (status: TaskStatus) =>
  status === 'pending' || status === 'queued' || status === 'running'

/** 后端 id 是数字，前端一律当字符串用 */
export const taskKey = (id: number | string) => String(id)

/** 任务的第一份产物；没有产物时为 undefined */
export const firstOutput = (view: TaskView) => view.outputs?.[0]
