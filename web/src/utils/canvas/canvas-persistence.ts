import type { Viewport } from '@xyflow/react'
import type { CanvasGraphDto } from '@/api/canvas/type'
import { ANIMATED_EDGE_OPTIONS } from '@/constants/canvas'
import type { CanvasEdge, CanvasNode } from '@/types'

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
        ...(status ? { status: status === 'running' ? 'idle' as const : status } : {}),
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
    nodes: nodes as CanvasNode[],
    edges: edges.map((edge) => ({ ...edge, ...ANIMATED_EDGE_OPTIONS })) as CanvasEdge[],
    viewport,
  }
}
