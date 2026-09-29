import type { Viewport } from '@xyflow/react'
import type { CanvasGraphDto } from '@/api/canvas/type'
import { ANIMATED_EDGE_OPTIONS } from '@/constants/canvas'
import type { CanvasEdge, CanvasNode } from '@/types'

/** 是否还有「存下来也没有意义」的本地生成中节点，保存要等它们收尾 */
export function hasVolatileRunning(nodes: CanvasNode[]) {
  return nodes.some((node) => node.data.status === 'running' && !node.data.taskId)
}

export function serializeGraph(
  nodes: CanvasNode[], edges: CanvasEdge[], viewport: Viewport,
): CanvasGraphDto {
  return {
    nodes: nodes.map((node) => {
      const { status, ...rest } = node.data
      const data = {
        kind: rest.kind,
        label: rest.label,
        ...(rest.prompt !== undefined ? { prompt: rest.prompt } : {}),
        ...(rest.model !== undefined ? { model: rest.model } : {}),
        ...(status
          ? { status: status === 'running' && !rest.taskId ? 'idle' as const : status }
          : {}),
        ...(rest.taskId !== undefined ? { taskId: rest.taskId } : {}),
        ...(rest.params !== undefined ? { params: rest.params } : {}),
        ...(rest.paramAssets !== undefined ? { paramAssets: rest.paramAssets } : {}),
        ...(rest.src !== undefined ? { src: rest.src } : {}),
        ...(rest.mediaType !== undefined ? { mediaType: rest.mediaType } : {}),
        ...(rest.assetId !== undefined ? { assetId: rest.assetId } : {}),
        ...(rest.uploaded !== undefined ? { uploaded: rest.uploaded } : {}),
        ...(rest.fileName !== undefined ? { fileName: rest.fileName } : {}),
        ...(rest.text !== undefined ? { text: rest.text } : {}),
        ...(rest.error !== undefined ? { error: rest.error } : {}),
      }
      if (data.src?.startsWith('blob:') && !data.assetId) delete data.src
      return {
        id: node.id,
        type: 'canvas',
        position: { x: node.position.x, y: node.position.y },
        ...(node.origin ? { origin: node.origin as [number, number] } : {}),
        data,
      }
    }),
    edges: edges.map((edge) => ({
      id: edge.id,
      source: edge.source,
      target: edge.target,
      ...(edge.sourceHandle !== undefined ? { sourceHandle: edge.sourceHandle } : {}),
      ...(edge.targetHandle !== undefined ? { targetHandle: edge.targetHandle } : {}),
    })),
    viewport,
  }
}

export function deserializeGraph(graph?: Partial<CanvasGraphDto> | null) {
  const nodes = Array.isArray(graph?.nodes) ? graph.nodes : []
  const edges = Array.isArray(graph?.edges) ? graph.edges : []
  const viewport = graph?.viewport ?? { x: 0, y: 0, zoom: 1 }
  return {
    nodes: nodes.map((node) => {
      const data = node.data as { status?: string; taskId?: string }
      // 兜底：旧数据或异常数据里没有 taskId 的 running 没人来回填，按 idle 处理
      return data.status === 'running' && !data.taskId
        ? { ...node, data: { ...node.data, status: 'idle' as const } }
        : node
    }) as CanvasNode[],
    edges: edges.map((edge) => ({ ...edge, ...ANIMATED_EDGE_OPTIONS })) as CanvasEdge[],
    viewport,
  }
}
