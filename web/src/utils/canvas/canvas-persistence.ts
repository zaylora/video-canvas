import type { Viewport } from "@xyflow/react";
import type { CanvasGraphDto } from "@/api/canvas/type";
import { ANIMATED_EDGE_OPTIONS } from "@/constants/canvas";
import type { CanvasEdge, FlowNode } from "@/types";

import { createDefaultScene, normalizeScene } from "@/utils/director/scene";

import { isGroupNode, normalizeFlowNodes } from "./group";
import { isUploading } from "./upload-state";
import { dropNodeOrigin } from "./placement";

/** 扩图中途被打断（刷新、关页）后，节点上显示的话：合成图还没传好，只能回原图重新扩 */
export const OUTPAINT_INTERRUPTED = "扩图被中断，请回到原图重新扩图";
/** 合成图已经传好、只差提交时被打断：点重试就能接着提交 */
export const OUTPAINT_RESUBMIT = "扩图被中断，点重试可以重新提交";

/**
 * 扩图结果节点在「读图、合成、上传、提交」这几步里的样子：生成中、还没有 taskId。
 * 这段进度只活在当前页面的内存里（outpaintRunner），刷新后没人接着跑，所以存档时要说清楚被打断了，
 * 不能当成普通的「生成中」静悄悄改成空壳。
 */
function isOutpaintInFlight(data: { status?: string; taskId?: string; outpaintFrom?: string }) {
  return data.status === "running" && !data.taskId && !!data.outpaintFrom;
}

/** 被打断的扩图节点要写的话：合成图已经上传（params.images 有值）就能重试提交，否则要回原图 */
function outpaintInterruptedMessage(params: Record<string, unknown> | undefined) {
  const images = params?.images;
  return Array.isArray(images) && images.length > 0 ? OUTPAINT_RESUBMIT : OUTPAINT_INTERRUPTED;
}

/**
 * 是否还有「存下来也没有意义」的本地生成中节点，保存要等它们收尾。
 * 上传中的节点不算：它们本来就不存，不该拖住整张画布的保存。
 * 扩图中途的节点也不算：它会被存成「被中断」的失败态，刷新后用户才看得到发生了什么。
 */
export function hasVolatileRunning(nodes: FlowNode[]) {
  return nodes.some(
    (node) =>
      !isGroupNode(node) &&
      node.data.status === "running" &&
      !node.data.taskId &&
      !isUploading(node.data) &&
      !isOutpaintInFlight(node.data),
  );
}

export function serializeGraph(
  nodes: FlowNode[],
  edges: CanvasEdge[],
  viewport: Viewport,
): CanvasGraphDto {
  return {
    // 上传中的节点还没有正式素材，存下来重开画布也是空壳，直接不存
    nodes: nodes
      .filter((node) => isGroupNode(node) || !isUploading(node.data))
      .map((node) => {
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
        const interrupted = isOutpaintInFlight(node.data);
        const data = {
          kind: rest.kind,
          label: rest.label,
          ...(rest.prompt !== undefined ? { prompt: rest.prompt } : {}),
          ...(rest.model !== undefined ? { model: rest.model } : {}),
          ...(status
            ? {
                status: interrupted
                  ? ("error" as const)
                  : status === "running" && !rest.taskId
                    ? ("idle" as const)
                    : status,
              }
            : {}),
          ...(rest.taskId !== undefined ? { taskId: rest.taskId } : {}),
          ...(rest.pendingTaskIds?.length ? { pendingTaskIds: rest.pendingTaskIds } : {}),
          ...(rest.params !== undefined ? { params: rest.params } : {}),
          ...(rest.paramAssets !== undefined ? { paramAssets: rest.paramAssets } : {}),
          ...(rest.src !== undefined ? { src: rest.src } : {}),
          ...(rest.mediaType !== undefined ? { mediaType: rest.mediaType } : {}),
          ...(rest.mediaAspect !== undefined ? { mediaAspect: rest.mediaAspect } : {}),
          ...(rest.assetId !== undefined ? { assetId: rest.assetId } : {}),
          ...(rest.uploaded !== undefined ? { uploaded: rest.uploaded } : {}),
          ...(rest.fileName !== undefined ? { fileName: rest.fileName } : {}),
          ...(rest.text !== undefined ? { text: rest.text } : {}),
          ...(interrupted
            ? { error: outpaintInterruptedMessage(rest.params) }
            : rest.error !== undefined
              ? { error: rest.error }
              : {}),
          ...(rest.outputs?.length ? { outputs: rest.outputs } : {}),
          ...(rest.activeOutputId !== undefined ? { activeOutputId: rest.activeOutputId } : {}),
          ...(rest.outpaintFrom !== undefined ? { outpaintFrom: rest.outpaintFrom } : {}),
          ...(rest.scene !== undefined ? { scene: rest.scene } : {}),
          ...(rest.characterId !== undefined ? { characterId: rest.characterId } : {}),
          ...(rest.labelCache !== undefined ? { labelCache: rest.labelCache } : {}),
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
    edges: edges.map((edge) => ({
      id: edge.id,
      source: edge.source,
      target: edge.target,
      ...(edge.sourceHandle !== undefined ? { sourceHandle: edge.sourceHandle } : {}),
      ...(edge.targetHandle !== undefined ? { targetHandle: edge.targetHandle } : {}),
    })),
    viewport,
  };
}

/**
 * 导演台节点读档时校验场景：结构不对或缺失就换成默认场景，节点照常能打开；
 * 别的种类的节点原样返回。
 */
function withDirectorScene<T extends { data: unknown }>(node: T): T {
  const data = node.data as { kind?: string; scene?: unknown };
  if (data.kind !== "director") return node;
  return { ...node, data: { ...data, scene: normalizeScene(data.scene) ?? createDefaultScene() } };
}

export function deserializeGraph(graph?: Partial<CanvasGraphDto> | null) {
  const nodes = Array.isArray(graph?.nodes) ? graph.nodes : [];
  const edges = Array.isArray(graph?.edges) ? graph.edges : [];
  const viewport = graph?.viewport ?? { x: 0, y: 0, zoom: 1 };
  return {
    // 组排在成员之前、指向不存在的组的 parentId 清掉：xyflow 要求父节点在前，也防坏数据
    nodes: normalizeFlowNodes(
      nodes.map((node) => {
        const data = node.data as {
          status?: string;
          taskId?: string;
          outpaintFrom?: string;
          params?: Record<string, unknown>;
        };
        // 兜底：旧数据或异常数据里没有 taskId 的 running 没人来回填，按 idle 处理；
        // 扩图中途残留的 running 说明被打断了，读成失败态并写明原因
        const fixed = isOutpaintInFlight(data)
          ? {
              ...node,
              data: {
                ...node.data,
                status: "error" as const,
                error: outpaintInterruptedMessage(data.params),
              },
            }
          : data.status === "running" && !data.taskId
            ? { ...node, data: { ...node.data, status: "idle" as const } }
            : node;
        // 老存档里带 origin 的节点，position 统一换算成左上角
        return dropNodeOrigin(withDirectorScene(fixed) as FlowNode);
      }) as FlowNode[],
    ),
    edges: edges.map((edge) => ({ ...edge, ...ANIMATED_EDGE_OPTIONS })) as CanvasEdge[],
    viewport,
  };
}
