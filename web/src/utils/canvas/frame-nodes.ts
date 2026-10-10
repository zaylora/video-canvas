import type { CanvasEdge, CanvasNode, FlowNode } from "@/types";

import { absolutePosition } from "./group";
import { newNodeLabel, uploadLabel } from "./node-label";
import { DEFAULT_NODE_SIZE } from "./placement";
import { makeSourceEdge } from "./source-edge";

/** 视频和帧图之间、帧图上下之间留的空隙，来源线要有地方走 */
const GAP = 60;
/** 同一列最多往下排几行，再多就换到更靠右的一列 */
const MAX_ROWS = 8;
/** 往右最多找几列，再找不到就叠在第一列（极端拥挤时叠放也比卡死好） */
const MAX_COLUMNS = 20;

type Rect = { x: number; y: number; width: number; height: number };

const overlaps = (a: Rect, b: Rect) =>
  a.x < b.x + b.width + GAP / 2 &&
  b.x < a.x + a.width + GAP / 2 &&
  a.y < b.y + b.height + GAP / 2 &&
  b.y < a.y + a.height + GAP / 2;

const sizeOf = (node: FlowNode) => ({
  width: node.measured?.width ?? node.width ?? DEFAULT_NODE_SIZE.width,
  height: node.measured?.height ?? node.height ?? DEFAULT_NODE_SIZE.height,
});

/**
 * 规划从视频截出来的帧图怎么落到画布上：每个文件一个上传型图片节点（带上传进度，传完变成正式素材），
 * 放在视频右侧，同一列从上往下按截取顺序排，避开画布上已有的节点；每个都挂一根从视频出来的来源线。
 * 只算出要加的节点和连线，上传由调用方交给 uploadRunner，写回画布也由调用方做。
 * 只用到文件名，所以截取之前就能先建出节点，让用户立刻看到进度。
 * @param video 被截帧的视频节点
 * @param nodes 画布上现有的全部节点（算避让、避开重名用）
 * @param files 要建的帧图，按截取顺序；只读 name，截取还没完成时给 { name } 就行
 * @param newId 生成节点 / 连线 id（测试里可以注入确定的 id）
 */
export function planFrameNodes(
  video: CanvasNode,
  nodes: FlowNode[],
  files: { name: string }[],
  newId: () => string = () => crypto.randomUUID(),
): { nodes: CanvasNode[]; edges: CanvasEdge[] } {
  if (files.length === 0) return { nodes: [], edges: [] };

  // 组内节点的 position 是相对组的，摆放一律按画布上的绝对位置算；帧图落在画布上，不入组
  const origin = absolutePosition(video, nodes);
  const videoSize = sizeOf(video);
  const { width, height } = DEFAULT_NODE_SIZE;
  const taken: Rect[] = nodes.map((node) => ({
    ...absolutePosition(node, nodes),
    ...sizeOf(node),
  }));
  const labels = nodes.map((node) => node.data.label);

  const outNodes: CanvasNode[] = [];
  const outEdges: CanvasEdge[] = [];
  for (const file of files) {
    let position = { x: origin.x + videoSize.width + GAP, y: origin.y };
    search: for (let col = 0; col < MAX_COLUMNS; col++) {
      for (let row = 0; row < MAX_ROWS; row++) {
        const candidate = {
          x: origin.x + videoSize.width + GAP + col * (width + GAP),
          y: origin.y + row * (height + GAP),
        };
        if (!taken.some((rect) => overlaps({ ...candidate, width, height }, rect))) {
          position = candidate;
          break search;
        }
      }
    }
    taken.push({ ...position, width, height });

    const label = newNodeLabel(uploadLabel(file.name, "图片"), labels);
    labels.push(label);
    const id = newId();
    outNodes.push({
      id,
      type: "canvas",
      position,
      data: {
        kind: "image",
        label,
        status: "running",
        uploadProgress: 0,
        mediaType: "image",
        uploaded: true,
        fileName: file.name,
      },
    });
    outEdges.push(makeSourceEdge(newId(), video.id, id));
  }
  return { nodes: outNodes, edges: outEdges };
}
