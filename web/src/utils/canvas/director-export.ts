import { ANIMATED_EDGE_OPTIONS } from "@/constants/canvas";
import type { CanvasEdge, CanvasNode, FlowNode } from "@/types";

import { newNodeLabel } from "./node-label";
import { nodeFrameSize } from "./node-frame";
import { placeBeside } from "./place-beside";

/** 一份要放回画布的导出物 */
export type DirectorExportItem = {
  /** 预览视频或首帧图 */
  kind: "video" | "image";
  cameraName: string;
  /** 画幅（宽 / 高） */
  aspect: number;
  /** 产出文件名，上传流程拿它当素材名 */
  fileName: string;
};

/**
 * 导演台导出要建的节点和来源线（设计稿 docs/design/3D导演台 §6.6）。
 * 每份导出物一个「上传中」的素材节点（uploaded: true，不出生成面板），按顺序摆在导演台右侧、
 * 互相避让；每个节点一根从导演台出发的线，只表示出处。重复导出不覆盖，名字自动编号。
 * @param input.newId 生成节点 id（测试里可注入）
 * @param input.edgeId 生成连线 id
 */
export function buildDirectorExportNodes(input: {
  source: CanvasNode;
  nodes: FlowNode[];
  items: DirectorExportItem[];
  newId?: () => string;
  edgeId?: () => string;
}): { nodes: CanvasNode[]; edges: CanvasEdge[] } {
  const { source, items } = input;
  const newId = input.newId ?? (() => crypto.randomUUID());
  const edgeId = input.edgeId ?? (() => crypto.randomUUID());
  // 已摆好的新节点也要参与避让：带上尺寸放进避让清单（只用于计算，不返回）
  const occupied: FlowNode[] = [...input.nodes];
  const labels = input.nodes.map((node) => node.data.label);
  const nodes: CanvasNode[] = [];
  const edges: CanvasEdge[] = [];

  for (const item of items) {
    const size = nodeFrameSize(item.aspect);
    const { x, y } = placeBeside({ source, nodes: occupied, size });
    const label = newNodeLabel(
      `${source.data.label}·${item.cameraName}${item.kind === "image" ? "·首帧" : ""}`,
      labels,
    );
    labels.push(label);
    const node: CanvasNode = {
      id: newId(),
      type: "canvas",
      position: { x, y },
      data: {
        kind: item.kind,
        label,
        status: "running",
        uploadProgress: 0,
        mediaType: item.kind,
        mediaAspect: item.aspect,
        uploaded: true,
        fileName: item.fileName,
      },
    };
    nodes.push(node);
    occupied.push({ ...node, ...size });
    edges.push({
      id: edgeId(),
      source: source.id,
      target: node.id,
      sourceHandle: null,
      targetHandle: null,
      ...ANIMATED_EDGE_OPTIONS,
    });
  }
  return { nodes, edges };
}
