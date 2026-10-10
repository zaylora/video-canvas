import type { XYPosition } from "@xyflow/react";

import type { FlowNode, NodeKind } from "@/types";

/** 节点还没测量时的默认尺寸（16:9），要和节点外壳 NodeCard 的宽度 w-xl 保持一致 */
export const DEFAULT_NODE_SIZE = { width: 576, height: 324 } as const;

/** 新节点还没测量时的默认尺寸，所有种类一样大 */
export function defaultNodeSize(_kind: NodeKind) {
  return { ...DEFAULT_NODE_SIZE };
}

/**
 * 让落点（画布坐标）落在新节点自己的某个点上，反推节点左上角。
 * anchor 是该点在节点内的比例位置：[0, 0.5] 左侧中点，[1, 0.5] 右侧中点。
 * 节点的 position 一律存左上角，不用 xyflow 的 origin，免得别处（比如打组）还得认锚点。
 */
export function topLeftFromAnchor(
  kind: NodeKind,
  point: XYPosition,
  anchor: readonly [number, number],
): XYPosition {
  const { width, height } = defaultNodeSize(kind);
  return { x: point.x - anchor[0] * width, y: point.y - anchor[1] * height };
}

/**
 * 老存档里拉线新建的节点带 origin（position 是锚点），统一换算成左上角并去掉 origin。
 * 读档时还没有测量尺寸，按默认尺寸算；组节点和没有 origin 的节点原样返回。
 */
export function dropNodeOrigin(node: FlowNode): FlowNode {
  if (node.type === "group" || !node.origin) return node;
  const { origin, ...rest } = node;
  const { width, height } = defaultNodeSize(node.data.kind);
  return {
    ...rest,
    position: { x: node.position.x - origin[0] * width, y: node.position.y - origin[1] * height },
  } as FlowNode;
}
