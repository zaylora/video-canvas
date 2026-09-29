import type { TaskView } from '@/api/generation-task/type'
import type { CanvasNodeData } from '@/types'
import { firstOutput, isTerminalStatus, taskKey } from './status'
import type { TaskMap } from './merge'

/** 节点回填只关心这几项 */
type BackfillNode = { id: string; data: CanvasNodeData }

export type BackfillEvent = {
  nodeId: string
  view: TaskView
  patch: Partial<CanvasNodeData>
}

const mediaTypeOf = (mediaType: string | undefined): 'image' | 'video' =>
  mediaType?.toLowerCase().startsWith('image') ? 'image' : 'video'

/**
 * 任务进入终态、且节点的 taskId 与任务一致时，算出要写回节点的补丁；
 * 节点已经是这个结果就返回 null（幂等，重复推送、重复对账都不会反复写）。
 *
 * - succeeded：done + 产物地址 + assetId
 * - failed / expired：error + 文案（「积分已退回」由展示层根据 taskId 补）
 * - canceled：回到 idle，taskId 一并清掉
 * taskId 在成功、失败时保留，方便追溯；只有 running 的节点才会被重开时对账。
 */
export function planBackfill(
  data: CanvasNodeData,
  view: TaskView,
): Partial<CanvasNodeData> | null {
  if (!data.taskId || data.taskId !== taskKey(view.id)) return null
  if (!isTerminalStatus(view.status)) return null

  switch (view.status) {
    case 'succeeded': {
      const output = firstOutput(view)
      if (!output?.url) {
        const message = '生成结果为空，请重试'
        return data.status === 'error' && data.error === message
          ? null
          : { status: 'error', error: message, src: null, assetId: undefined }
      }
      const assetId = String(output.asset_id)
      if (data.status === 'done' && data.assetId === assetId) return null
      return {
        status: 'done',
        src: output.url,
        assetId,
        mediaType: mediaTypeOf(output.media_type),
        uploaded: false,
        fileName: undefined,
        error: null,
      }
    }
    case 'failed':
    case 'expired': {
      const message =
        view.error_message || (view.status === 'expired' ? '生成超时' : '生成失败')
      if (data.status === 'error' && data.error === message) return null
      return { status: 'error', error: message, src: null, assetId: undefined }
    }
    case 'canceled':
      return { status: 'idle', taskId: undefined, error: null, src: null, assetId: undefined }
    default:
      return null
  }
}

/**
 * 把 store 里所有终态任务回填到节点。没有任何变化时原样返回同一个数组（引用相等）。
 * 已被删除的节点自然不在数组里，直接忽略。
 */
export function applyBackfill<N extends BackfillNode>(
  nodes: N[],
  tasks: TaskMap,
): { nodes: N[]; events: BackfillEvent[] } {
  const events: BackfillEvent[] = []
  let changed = false
  const next = nodes.map((node) => {
    const taskId = node.data.taskId
    const view = taskId ? tasks[taskId] : undefined
    if (!view) return node
    const patch = planBackfill(node.data, view)
    if (!patch) return node
    changed = true
    events.push({ nodeId: node.id, view, patch })
    return { ...node, data: { ...node.data, ...patch } }
  })
  return { nodes: changed ? next : nodes, events }
}
