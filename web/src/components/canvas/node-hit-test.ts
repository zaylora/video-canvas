import type { InternalNode, Node, XYPosition } from "@xyflow/react";

/**
 * 画布坐标下，某个点落在节点身上的哪个位置。
 *
 * 返回值是相对节点中心的偏移，按半宽半高归一化到 [-1, 1]：
 * 正中是 (0, 0)，右下角是 (1, 1)。点在节点外面，或节点还没量出尺寸，都返回 null。
 *
 * 拉线时的倾斜反馈和松手时的落点判定共用这一把尺子，
 * 才不会出现“节点沉下去了却接不上”的错位。
 */
export function getNodeHit(node: InternalNode<Node>, point: XYPosition): XYPosition | null {
  const { width, height } = node.measured;
  if (!width || !height) return null;

  const { x, y } = node.internals.positionAbsolute;
  const offsetX = (point.x - (x + width / 2)) / (width / 2);
  const offsetY = (point.y - (y + height / 2)) / (height / 2);
  if (Math.abs(offsetX) > 1 || Math.abs(offsetY) > 1) return null;

  return { x: offsetX, y: offsetY };
}
