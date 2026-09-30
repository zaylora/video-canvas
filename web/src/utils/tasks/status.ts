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

/**
 * 文本任务的正文：取第一份带非空 text 的产出。
 * 原样返回，不做 trim，正文里的换行与缩进要保留。
 * @param view 任务快照
 * @returns 正文；没有文本产出或全是空白时为 undefined
 */
export const outputText = (view: TaskView): string | undefined =>
  view.outputs?.find((output) => typeof output.text === 'string' && output.text.trim() !== '')?.text
