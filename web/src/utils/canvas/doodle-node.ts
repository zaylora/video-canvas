import { ANIMATED_EDGE_OPTIONS } from "@/constants/canvas";
import type { CanvasEdge, CanvasNode, FlowNode } from "@/types";

import { nodeFrameSize } from "./node-frame";
import { newNodeLabel } from "./node-label";
import { placeBeside } from "./place-beside";

/** 涂鸦结果上传时用的文件名 */
export const DOODLE_FILE_NAME = "doodle.png";

/** 涂鸦结果节点标题的后缀：「源标题 标注」 */
const DOODLE_LABEL_SUFFIX = "标注";

/**
 * 造涂鸦保存后的新节点和连线：一个上传型图片节点（uploaded: true，没有任何生成字段，
 * 选中后不出生成面板），摆在源节点右侧并避开已有节点，加一条从源节点出来的普通连线。
 * 节点先以「上传中」建出来，图片导出好再交给上传流程。源节点不改。
 * @param input.source 被涂鸦的图片节点
 * @param input.nodes 画布上现有的全部节点，用来避让和避开重名
 * @param input.aspect 新图的画幅（宽 / 高），取源图原始像素的
 * @param input.newId 生成节点和连线 id（测试里可以注入）；先取节点 id，再取连线 id
 */
export function buildDoodleNode(input: {
  source: CanvasNode;
  nodes: FlowNode[];
  aspect: number;
  newId?: () => string;
}): { node: CanvasNode; edge: CanvasEdge } {
  const { source, aspect } = input;
  const newId = input.newId ?? (() => crypto.randomUUID());
  const size = nodeFrameSize(aspect);
  const label = newNodeLabel(
    `${source.data.label} ${DOODLE_LABEL_SUFFIX}`,
    input.nodes.map((node) => node.data.label),
  );
  const { x, y } = placeBeside({ source, nodes: input.nodes, size });
  const id = newId();
  const node: CanvasNode = {
    id,
    type: "canvas",
    position: { x, y },
    width: size.width,
    height: size.height,
    data: {
      kind: "image",
      label,
      status: "running",
      uploadProgress: 0,
      mediaType: "image",
      mediaAspect: aspect,
      uploaded: true,
      fileName: DOODLE_FILE_NAME,
    },
  };
  const edge: CanvasEdge = {
    id: newId(),
    source: source.id,
    target: id,
    sourceHandle: null,
    targetHandle: null,
    ...ANIMATED_EDGE_OPTIONS,
  };
  return { node, edge };
}
