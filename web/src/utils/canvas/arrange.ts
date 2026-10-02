import type { XYPosition } from "@xyflow/react";

import type { CanvasNode } from "@/types";

export type ArrangeMode = "row" | "column" | "grid";

/** 节点之间的空隙：横向要让出连接点的 ⊕，纵向要让出卡片上方的标题行 */
export const ARRANGE_GAP = { x: 120, y: 80 };

/** 还没测量过的节点按这个尺寸排 */
const FALLBACK = { width: 384, height: 216 };

const sizeOf = (node: CanvasNode) => ({
  width: node.measured?.width ?? node.width ?? FALLBACK.width,
  height: node.measured?.height ?? node.height ?? FALLBACK.height,
});

/**
 * 整理布局：以选区左上角为起点，把节点排成一行、一列或近似方形的网格。
 * 保留原来的大致先后：横排按 x 排序，竖排按 y，网格按先行后列。返回 id -> 新位置。
 */
export function arrangeNodes(nodes: CanvasNode[], mode: ArrangeMode): Map<string, XYPosition> {
  const result = new Map<string, XYPosition>();
  if (nodes.length === 0) return result;
  const left = Math.min(...nodes.map((node) => node.position.x));
  const top = Math.min(...nodes.map((node) => node.position.y));

  const sorted = [...nodes].sort((a, b) =>
    mode === "row"
      ? a.position.x - b.position.x || a.position.y - b.position.y
      : a.position.y - b.position.y || a.position.x - b.position.x,
  );

  if (mode === "row" || mode === "column") {
    let cursor = mode === "row" ? left : top;
    for (const node of sorted) {
      const size = sizeOf(node);
      result.set(node.id, mode === "row" ? { x: cursor, y: top } : { x: left, y: cursor });
      cursor += mode === "row" ? size.width + ARRANGE_GAP.x : size.height + ARRANGE_GAP.y;
    }
    return result;
  }

  // 网格：列数取 ⌈√n⌉，每列宽取该列最宽的节点，每行高取该行最高的节点
  const columns = Math.ceil(Math.sqrt(sorted.length));
  const colWidth: number[] = [];
  const rowHeight: number[] = [];
  sorted.forEach((node, index) => {
    const size = sizeOf(node);
    const col = index % columns;
    const row = Math.floor(index / columns);
    colWidth[col] = Math.max(colWidth[col] ?? 0, size.width);
    rowHeight[row] = Math.max(rowHeight[row] ?? 0, size.height);
  });
  sorted.forEach((node, index) => {
    const col = index % columns;
    const row = Math.floor(index / columns);
    const x = left + colWidth.slice(0, col).reduce((sum, w) => sum + w + ARRANGE_GAP.x, 0);
    const y = top + rowHeight.slice(0, row).reduce((sum, h) => sum + h + ARRANGE_GAP.y, 0);
    result.set(node.id, { x, y });
  });
  return result;
}
