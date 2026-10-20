import type { FlowNode } from "@/types";

import { absolutePosition } from "./group";
import { AVOID_GAP, MAX_SHIFTS, NODE_GAP_X, overlaps, sizeOfNode, type Rect } from "./place-beside";

/** 一个节点要占的框大小（画布坐标） */
export type GroupCellSize = { width: number; height: number };

/**
 * 把一组由源节点衍生出来的节点按行列摆成网格，放在源节点右侧（placeBeside 只能摆一个，这是成组的版本）。
 * 整组离源节点 NODE_GAP_X、垂直居中；列与列之间隔 NODE_GAP_X，行与行之间隔 AVOID_GAP。
 * 同一行顶对齐、行高取该行最高的；同一列左对齐、列宽取该列最宽的。
 * 整组的外框撞到别的节点就整组一起往下错开，刚好躲开撞到的节点并留出 AVOID_GAP，最多试 MAX_SHIFTS 次。
 * 返回的是绝对坐标（不是组内相对坐标）。
 * @param input.source 源节点
 * @param input.nodes 画布上所有节点，用来避让
 * @param input.rows 行数
 * @param input.cols 列数
 * @param input.sizes 每个节点的框大小，按行优先，长度应为 rows * cols
 * @returns 与 sizes 同序的左上角坐标；没有节点返回空数组
 */
export function placeGroup(input: {
  source: FlowNode;
  nodes: FlowNode[];
  rows: number;
  cols: number;
  sizes: GroupCellSize[];
}): { x: number; y: number }[] {
  const { source, nodes, rows, cols, sizes } = input;
  if (rows <= 0 || cols <= 0 || sizes.length === 0) return [];

  const colWidths = Array.from({ length: cols }, () => 0);
  const rowHeights = Array.from({ length: rows }, () => 0);
  sizes.forEach((size, index) => {
    const row = Math.floor(index / cols);
    const col = index % cols;
    colWidths[col] = Math.max(colWidths[col], size.width);
    rowHeights[row] = Math.max(rowHeights[row], size.height);
  });

  // 每行、每列相对整组左上角的起点
  const colOffsets: number[] = [];
  let offsetX = 0;
  for (const width of colWidths) {
    colOffsets.push(offsetX);
    offsetX += width + NODE_GAP_X;
  }
  const rowOffsets: number[] = [];
  let offsetY = 0;
  for (const height of rowHeights) {
    rowOffsets.push(offsetY);
    offsetY += height + AVOID_GAP;
  }
  const groupWidth = offsetX - NODE_GAP_X;
  const groupHeight = offsetY - AVOID_GAP;

  const origin = absolutePosition(source, nodes);
  const sourceSize = sizeOfNode(source);
  const taken: Rect[] = nodes.map((node) => ({
    ...absolutePosition(node, nodes),
    ...sizeOfNode(node),
  }));

  const x = origin.x + sourceSize.width + NODE_GAP_X;
  let y = origin.y + sourceSize.height / 2 - groupHeight / 2;
  for (let step = 0; step < MAX_SHIFTS; step++) {
    const frame: Rect = { x, y, width: groupWidth, height: groupHeight };
    const hits = taken.filter((rect) => overlaps(frame, rect));
    if (hits.length === 0) break;
    // 一步下移到所有撞到的节点之下，比逐格试快，也不会留下半截空隙
    y = Math.max(...hits.map((rect) => rect.y + rect.height)) + AVOID_GAP;
  }

  return sizes.map((_, index) => ({
    x: x + colOffsets[index % cols],
    y: y + rowOffsets[Math.floor(index / cols)],
  }));
}
