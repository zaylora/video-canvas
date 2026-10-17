import { ANIMATED_EDGE_OPTIONS } from "@/constants/canvas";
import type { CanvasEdge, CanvasNodeData, FlowNode } from "@/types";

import { isGroupNode } from "./group";
import { copyLabels } from "./node-label";

/** 复制出来的节点不带任务：在跑的任务属于原节点，副本回到「有结果就 done，没有就 idle」 */
function cloneData(data: CanvasNodeData): CanvasNodeData {
  const next = structuredClone(data);
  delete next.taskId;
  delete next.pendingTaskIds;
  delete next.uploadProgress;
  if (next.status === "running" || next.status === "error") {
    next.status = next.src ? "done" : "idle";
    next.error = null;
  }
  return next;
}

/**
 * 把一组节点和它们之间的连线复制一份，整体平移 offset，新副本处于选中状态。
 * 给了 onCanvas（画布上眼下的节点）就按「X 副本」「X 副本二」起名，避开已有的名字；
 * 只是挪剪贴板里的位置时不给，名字原样留着。
 * 组：复制一个组连同它的成员，成员相对组的位置不变；只复制了成员（组没一起复制）时，
 * 副本留在原来的组里，按原位置平移。
 */
export function cloneGroup(
  source: { nodes: FlowNode[]; edges: CanvasEdge[] },
  offset: { x: number; y: number },
  onCanvas?: FlowNode[],
) {
  const ids = new Map(source.nodes.map((node) => [node.id, crypto.randomUUID()]));
  const used = onCanvas?.map((node) => node.data.label) ?? [];
  const nodes = source.nodes.map((node): FlowNode => {
    const id = ids.get(node.id) as string;
    const label = onCanvas ? copyLabels(node.data.label, used, 1)[0] : node.data.label;
    if (onCanvas) used.push(label);
    // 父组也在这次复制里：成员跟着新组走，相对位置不变，不再单独平移
    const parentCloned = !!node.parentId && ids.has(node.parentId);
    const position = parentCloned
      ? node.position
      : { x: node.position.x + offset.x, y: node.position.y + offset.y };
    if (isGroupNode(node)) {
      return {
        id,
        type: "group",
        position,
        width: node.width,
        height: node.height,
        zIndex: node.zIndex,
        selected: true,
        data: { ...node.data, label },
      };
    }
    const data = cloneData(node.data);
    data.label = label;
    return {
      id,
      type: node.type,
      position,
      selected: !parentCloned,
      ...(node.parentId ? { parentId: parentCloned ? ids.get(node.parentId) : node.parentId } : {}),
      data,
    };
  });
  // 引用连线：源头不在这次复制里，副本仍连回原来的源头；画布上已经没有那个源头就不连
  const alive = onCanvas && new Set(onCanvas.map((node) => node.id));
  const edges = source.edges
    .filter((edge) => ids.has(edge.source) || !alive || alive.has(edge.source))
    .map((edge) => ({
      ...edge,
      ...ANIMATED_EDGE_OPTIONS,
      id: crypto.randomUUID(),
      source: ids.get(edge.source) ?? edge.source,
      target: ids.get(edge.target) as string,
      selected: false,
    }));
  // xyflow 要求父节点在子节点之前
  return {
    nodes: [...nodes.filter(isGroupNode), ...nodes.filter((node) => !isGroupNode(node))],
    edges,
  };
}

/**
 * 复制范围内要带上的连线：两头都在范围内的，加上从范围外连进来的引用连线。
 * 连出去的线不带，免得副本凭空成了别的节点的上游。
 */
export function selectionEdges(ids: Set<string>, edges: CanvasEdge[]): CanvasEdge[] {
  return edges.filter((edge) => ids.has(edge.target));
}
