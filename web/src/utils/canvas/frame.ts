import { ANIMATED_EDGE_OPTIONS } from "@/constants/canvas";
import type { CanvasEdge, CanvasNode, FlowNode } from "@/types";

import { nodeFrameSize } from "./node-frame";
import { newNodeLabel, uploadLabel } from "./node-label";
import { placeBeside } from "./place-beside";

/**
 * 视频截取帧里所有能单独讲的算法：时刻显示、文件名、暂存规则、方向键步进、帧图节点和连线。
 * 都不碰 DOM 和视频元素，所以能直接测；真正取帧在 frame-run.ts、frame-engine.ts 里。
 */

/** 要截的那一帧：首帧、尾帧，或某个时刻（秒） */
export type FrameSpec = "first" | "last" | number;

/** 一次自定义截帧最多暂存几帧：再多面板一排放不下 */
export const MAX_STAGED_FRAMES = 9;

/** 方向键微调一次走多少秒：帧率不知道，按常见的 30fps 取一帧 */
export const FRAME_STEP = 1 / 30;

/** 比 60fps 的一帧（约 0.0167 秒）还近才算同一个时刻；相邻的两帧不会被误当成重复 */
const DUPLICATE_WINDOW = 0.015;

/** 帧图节点的默认标题（文件名取不出来时用） */
export const FRAME_NODE_LABEL = "帧图";

/** 时间显示成 mm:ss，不满一秒向下取整 */
export function formatFrameTime(seconds: number): string {
  const total = Math.max(0, Math.floor(seconds));
  const minutes = String(Math.floor(total / 60)).padStart(2, "0");
  return `${minutes}:${String(total % 60).padStart(2, "0")}`;
}

/**
 * 截出来的图片文件名：视频名加帧的叫法。文件名里不能出现的字符换成下划线，
 * 自定义帧的时刻写成 00-04。节点标题由上传流程按文件名取，所以名字直接决定节点叫什么。
 */
export function frameFileName(videoLabel: string, spec: FrameSpec): string {
  const safe = videoLabel.replace(/[\\/:*?"<>|]/g, "_").trim();
  const tail =
    spec === "first" ? "首帧" : spec === "last" ? "尾帧" : formatFrameTime(spec).replace(":", "-");
  return `${safe}-${tail}.png`;
}

/** 能不能再暂存一帧：已经到上限，或这个时刻已经收过（一帧之内），都不行 */
export function canStageFrame(
  staged: readonly { time: number }[],
  time: number,
): { ok: true } | { ok: false; reason: "limit" | "duplicate" } {
  if (staged.length >= MAX_STAGED_FRAMES) return { ok: false, reason: "limit" };
  if (staged.some((item) => Math.abs(item.time - time) < DUPLICATE_WINDOW)) {
    return { ok: false, reason: "duplicate" };
  }
  return { ok: true };
}

/**
 * 键盘微调播放头：方向键走一帧，按住 Shift 走一秒，钳在 0 到总时长之间。
 * @param direction 1 往后，-1 往前
 */
export function stepFrameTime(
  time: number,
  direction: 1 | -1,
  big: boolean,
  duration: number,
): number {
  const next = time + direction * (big ? 1 : FRAME_STEP);
  return Math.min(Math.max(next, 0), Math.max(duration, 0));
}

/**
 * 规划截出来的帧落到画布上的样子：每个文件一个上传型图片节点（uploaded: true，没有任何生成字段，
 * 所以选中后不出生成面板），摆在视频右侧、同一列从上往下按顺序排并避开已有节点，
 * 每个节点一根从视频出来的普通连线，和视频修剪的剪辑节点一样。
 * 只用到文件名，所以截取之前就能先建出节点，让用户立刻看到进度。
 * @param input.source 被截帧的视频节点
 * @param input.nodes 画布上现有的全部节点，用来避让和避开重名
 * @param input.files 要建的帧图，按截取顺序；只读 name
 * @param input.aspect 帧图节点的画幅（宽 / 高），取源视频的
 * @param input.newId 生成节点和连线 id（测试里可以注入）
 */
export function buildFrameNodes(input: {
  source: CanvasNode;
  nodes: FlowNode[];
  files: { name: string }[];
  aspect: number;
  newId?: () => string;
}): { nodes: CanvasNode[]; edges: CanvasEdge[] } {
  const { source, files, aspect } = input;
  const newId = input.newId ?? (() => crypto.randomUUID());
  const size = nodeFrameSize(aspect);
  const labels = input.nodes.map((node) => node.data.label);
  // 每放一个就当成已有节点，下一个会自己往下错开
  const taken: FlowNode[] = [...input.nodes];

  const nodes: CanvasNode[] = [];
  const edges: CanvasEdge[] = [];
  for (const file of files) {
    const label = newNodeLabel(uploadLabel(file.name, FRAME_NODE_LABEL), labels);
    labels.push(label);
    const { x, y } = placeBeside({ source, nodes: taken, size });
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
        fileName: file.name,
      },
    };
    nodes.push(node);
    taken.push(node);
    edges.push({
      id: newId(),
      source: source.id,
      target: id,
      sourceHandle: null,
      targetHandle: null,
      ...ANIMATED_EDGE_OPTIONS,
    });
  }
  return { nodes, edges };
}
