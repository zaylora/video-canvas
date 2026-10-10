import type { Viewport } from "@xyflow/react";
import type { CanvasGraphDto } from "@/api/canvas/type";
import { ANIMATED_EDGE_OPTIONS } from "@/constants/canvas";
import type { CanvasEdge, FlowNode } from "@/types";

import { isGroupNode, normalizeFlowNodes } from "./group";
import { isUploading } from "./upload-state";
import { dropNodeOrigin } from "./placement";
import { SOURCE_EDGE_TYPE, SOURCE_RELATION, isSourceEdge } from "./source-edge";

/** 存档里的连线还原成画布上的连线：都套流动高亮，来源线只是类型不同（语义上不参与生成） */
function asFlowEdge(edge: CanvasGraphDto["edges"][number]) {
  return edge.relation === SOURCE_RELATION
    ? { ...edge, ...ANIMATED_EDGE_OPTIONS, type: SOURCE_EDGE_TYPE }
    : { ...edge, ...ANIMATED_EDGE_OPTIONS };
}

/**
 * 是否还有「存下来也没有意义」的本地生成中节点，保存要等它们收尾。
 * 上传中的节点不算：它们本来就不存，不该拖住整张画布的保存。
 */
export function hasVolatileRunning(nodes: FlowNode[]) {
  return nodes.some(
    (node) =>
      !isGroupNode(node) &&
      node.data.status === "running" &&
      !node.data.taskId &&
      !isUploading(node.data),
  );
}

export function serializeGraph(
  nodes: FlowNode[],
  edges: CanvasEdge[],
  viewport: Viewport,
): CanvasGraphDto {
  // 上传中的节点还没有正式素材，存下来重开画布也是空壳，直接不存
  const saved = nodes.filter((node) => isGroupNode(node) || !isUploading(node.data));
  const omitted = new Set(nodes.filter((node) => !saved.includes(node)).map((node) => node.id));
  return {
    nodes: saved.map((node) => {
      if (isGroupNode(node)) {
        const { label, color, labelColor } = node.data;
        return {
          id: node.id,
          type: "group" as const,
          position: { x: node.position.x, y: node.position.y },
          width: node.width ?? node.measured?.width ?? 0,
          height: node.height ?? node.measured?.height ?? 0,
          data: {
            label,
            ...(color ? { color } : {}),
            ...(labelColor ? { labelColor } : {}),
          },
        };
      }
      const { status, ...rest } = node.data;
      const data = {
        kind: rest.kind,
        label: rest.label,
        ...(rest.prompt !== undefined ? { prompt: rest.prompt } : {}),
        ...(rest.model !== undefined ? { model: rest.model } : {}),
        ...(status
          ? { status: status === "running" && !rest.taskId ? ("idle" as const) : status }
          : {}),
        ...(rest.taskId !== undefined ? { taskId: rest.taskId } : {}),
        ...(rest.params !== undefined ? { params: rest.params } : {}),
        ...(rest.paramAssets !== undefined ? { paramAssets: rest.paramAssets } : {}),
        ...(rest.src !== undefined ? { src: rest.src } : {}),
        ...(rest.mediaType !== undefined ? { mediaType: rest.mediaType } : {}),
        ...(rest.assetId !== undefined ? { assetId: rest.assetId } : {}),
        ...(rest.aspect !== undefined ? { aspect: rest.aspect } : {}),
        ...(rest.uploaded !== undefined ? { uploaded: rest.uploaded } : {}),
        ...(rest.fileName !== undefined ? { fileName: rest.fileName } : {}),
        ...(rest.text !== undefined ? { text: rest.text } : {}),
        ...(rest.error !== undefined ? { error: rest.error } : {}),
        ...(rest.outputs?.length ? { outputs: rest.outputs } : {}),
        ...(rest.activeOutputId !== undefined ? { activeOutputId: rest.activeOutputId } : {}),
      };
      if (data.src?.startsWith("blob:") && !data.assetId) delete data.src;
      return {
        id: node.id,
        type: "canvas",
        position: { x: node.position.x, y: node.position.y },
        ...(node.parentId ? { parentId: node.parentId } : {}),
        ...(node.origin ? { origin: node.origin as [number, number] } : {}),
        data,
      };
    }),
    // 端点是没存下来的上传中节点（比如刚截出来、还在上传的帧图连着的来源线），连线也不存，免得留下悬空的线
    edges: edges
      .filter((edge) => !omitted.has(edge.source) && !omitted.has(edge.target))
      .map((edge) => ({
        id: edge.id,
        source: edge.source,
        target: edge.target,
        ...(edge.sourceHandle !== undefined ? { sourceHandle: edge.sourceHandle } : {}),
        ...(edge.targetHandle !== undefined ? { targetHandle: edge.targetHandle } : {}),
        ...(isSourceEdge(edge) ? { relation: SOURCE_RELATION } : {}),
      })),
    viewport,
  };
}

export function deserializeGraph(graph?: Partial<CanvasGraphDto> | null) {
  const nodes = Array.isArray(graph?.nodes) ? graph.nodes : [];
  const edges = Array.isArray(graph?.edges) ? graph.edges : [];
  const viewport = graph?.viewport ?? { x: 0, y: 0, zoom: 1 };
  return {
    // 组排在成员之前、指向不存在的组的 parentId 清掉：xyflow 要求父节点在前，也防坏数据
    nodes: normalizeFlowNodes(
      nodes.map((node) => {
        const data = node.data as { status?: string; taskId?: string };
        // 兜底：旧数据或异常数据里没有 taskId 的 running 没人来回填，按 idle 处理
        const fixed =
          data.status === "running" && !data.taskId
            ? { ...node, data: { ...node.data, status: "idle" as const } }
            : node;
        // 老存档里带 origin 的节点，position 统一换算成左上角
        return dropNodeOrigin(fixed as FlowNode);
      }) as FlowNode[],
    ),
    edges: edges.map((edge) => asFlowEdge(edge)) as CanvasEdge[],
    viewport,
  };
}
