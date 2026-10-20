import { ANIMATED_EDGE_OPTIONS } from "@/constants/canvas";
import type { CanvasEdge, CanvasNode, FlowNode } from "@/types";

import type { Tile } from "./grid-split";
import { nodeFrameSize } from "./node-frame";
import { NODE_LABEL_MAX, newNodeLabel } from "./node-label";
import { placeGroup } from "./place-group";

/** 标题去重时「 2」「 12」这类编号后缀最多占的字数：截断源标题时给它留位置 */
const LABEL_DEDUPE_ROOM = 3;

/**
 * 切出来的某一格的文件名：grid-行-列.png，行列从 1 起。
 * 上传完成后素材就叫这个名字。
 * @param row 第几行，从 0 起
 * @param col 第几列，从 0 起
 */
export const gridTileFileName = (row: number, col: number) => `grid-${row + 1}-${col + 1}.png`;

/**
 * 某一格的标题：源标题加「行-列」。源标题太长时截短它，保住行列后缀。
 * @param label 源节点标题
 * @param row 第几行，从 0 起
 * @param col 第几列，从 0 起
 */
function tileLabel(label: string, row: number, col: number): string {
  const suffix = ` ${row + 1}-${col + 1}`;
  const room = NODE_LABEL_MAX - [...suffix].length - LABEL_DEDUPE_ROOM;
  const chars = [...label];
  return (chars.length > room ? chars.slice(0, room).join("") : label) + suffix;
}

/**
 * 规划宫格切分落到画布上的样子：每格一个上传型图片节点（uploaded: true，没有任何生成字段，
 * 所以选中后不出生成面板），按行列排成网格摆在源节点右侧并避开已有节点，
 * 每格一根从源节点出来的普通连线，和扩图、截取帧一样。
 * 只用到各格的宽高比，所以切图之前就能先建出节点，让用户立刻看到进度。
 * @param input.source 被切分的图片节点
 * @param input.nodes 画布上现有的全部节点，用来避让和避开重名
 * @param input.tiles 各格，按行优先；只读 row、col、w、h（宽高比），不要求是真实像素
 * @param input.rows 行数
 * @param input.cols 列数
 * @param input.newId 生成节点和连线 id（测试里可以注入）
 */
export function buildGridSplitNodes(input: {
  source: CanvasNode;
  nodes: FlowNode[];
  tiles: Pick<Tile, "row" | "col" | "w" | "h">[];
  rows: number;
  cols: number;
  newId?: () => string;
}): { nodes: CanvasNode[]; edges: CanvasEdge[] } {
  const { source, tiles, rows, cols } = input;
  const newId = input.newId ?? (() => crypto.randomUUID());
  if (tiles.length === 0) return { nodes: [], edges: [] };

  const sizes = tiles.map((tile) => nodeFrameSize(tile.w / tile.h));
  const positions = placeGroup({ source, nodes: input.nodes, rows, cols, sizes });
  const labels = input.nodes.map((node) => node.data.label);

  const nodes: CanvasNode[] = [];
  const edges: CanvasEdge[] = [];
  tiles.forEach((tile, index) => {
    const label = newNodeLabel(tileLabel(source.data.label, tile.row, tile.col), labels);
    labels.push(label);
    const id = newId();
    nodes.push({
      id,
      type: "canvas",
      position: positions[index],
      width: sizes[index].width,
      height: sizes[index].height,
      data: {
        kind: "image",
        label,
        status: "running",
        uploadProgress: 0,
        mediaType: "image",
        mediaAspect: tile.w / tile.h,
        uploaded: true,
        fileName: gridTileFileName(tile.row, tile.col),
      },
    });
    edges.push({
      id: newId(),
      source: source.id,
      target: id,
      sourceHandle: null,
      targetHandle: null,
      ...ANIMATED_EDGE_OPTIONS,
    });
  });
  return { nodes, edges };
}
