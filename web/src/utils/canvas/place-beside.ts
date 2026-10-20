import type { FlowNode } from "@/types";

import { absolutePosition } from "./group";
import { DEFAULT_NODE_SIZE } from "./placement";

/** 新节点离源节点的水平间距（画布坐标） */
export const NODE_GAP_X = 80;
/** 新节点之间、与已有节点之间至少留的空隙 */
export const AVOID_GAP = 40;
/** 找空位最多往下试几次，极端拥挤时不再找，叠放也比卡死好 */
export const MAX_SHIFTS = 60;

/** 画布坐标里的矩形：左上角加宽高 */
export type Rect = { x: number; y: number; width: number; height: number };

/** 节点此刻的大小：优先量到的，其次声明的，最后默认大小 */
export const sizeOfNode = (node: FlowNode) => ({
  width: node.measured?.width ?? node.width ?? DEFAULT_NODE_SIZE.width,
  height: node.measured?.height ?? node.height ?? DEFAULT_NODE_SIZE.height,
});

/** 两个矩形是否撞上：含 AVOID_GAP 的空隙，间隔不够也算撞 */
export const overlaps = (a: Rect, b: Rect) =>
  a.x < b.x + b.width + AVOID_GAP &&
  b.x < a.x + a.width + AVOID_GAP &&
  a.y < b.y + b.height + AVOID_GAP &&
  b.y < a.y + a.height + AVOID_GAP;

/**
 * 把一个由源节点衍生出来的新节点摆在它右侧：水平隔 80px、垂直居中，
 * 撞到别的节点就往下错开一格（新节点的高度加间隔），最多试 60 次。
 * 扩图和视频修剪共用；返回的是新节点左上角的画布坐标（绝对坐标，不是组内相对坐标）。
 * @param input.source 源节点
 * @param input.nodes 画布上所有节点，用来避让
 * @param input.size 新节点的框大小
 */
export function placeBeside(input: {
  source: FlowNode;
  nodes: FlowNode[];
  size: { width: number; height: number };
}): { x: number; y: number } {
  const { source, nodes, size } = input;
  const origin = absolutePosition(source, nodes);
  const sourceSize = sizeOfNode(source);
  const taken: Rect[] = nodes.map((node) => ({
    ...absolutePosition(node, nodes),
    ...sizeOfNode(node),
  }));

  const x = origin.x + sourceSize.width + NODE_GAP_X;
  let y = origin.y + sourceSize.height / 2 - size.height / 2;
  for (let step = 0; step < MAX_SHIFTS; step++) {
    if (!taken.some((rect) => overlaps({ x, y, ...size }, rect))) break;
    y += size.height + AVOID_GAP;
  }
  return { x, y };
}
