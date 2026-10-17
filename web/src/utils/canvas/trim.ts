import { ANIMATED_EDGE_OPTIONS } from "@/constants/canvas";
import type { CanvasEdge, CanvasNode, FlowNode } from "@/types";

import { newNodeLabel } from "./node-label";
import { nodeFrameSize } from "./node-frame";
import { placeBeside } from "./place-beside";

/**
 * 视频修剪（设计稿 docs/design/视频修剪）里所有能单独讲的算法：
 * 手柄时间、选区边界、确认条件、胶片条时间点、产出文件名、编码前的体积预估、修剪专用取流地址。
 * 都不碰 DOM 和视频元素，所以能直接测；真正读视频、编码在 trim-run.ts 里。
 */

/** 两端手柄的最小间隔（秒）：再短就没有剪的意义，也容易一拖就翻过去 */
export const TRIM_MIN_GAP = 0.5;

/** 手柄位置与显示都按 0.01 秒取整：比这更细手指也拖不出来，反而满屏浮点尾巴 */
const TIME_STEP = 100;

/** 输出文件名里标题最多留多少个字，和上传素材的文件名一起看 */
const MAX_TITLE_CHARS = 48;

/** 修剪模式的状态阶段：参数加载中、可以编辑、正在编码 */
export type TrimPhase = "loading" | "editing" | "encoding";

/** 两端手柄里的哪一端 */
export type TrimEnd = "start" | "end";

/** 一段选区（秒） */
export type TrimRange = { start: number; end: number };

/**
 * 取整到 0.01 秒并钳在 0 到总时长之间。总时长为 0（还没量出来）时一律为 0。
 * 浮点误差（0.1 + 0.2）也在这里收掉，免得时长显示成 0.30000000000000004 s。
 */
export function clampTrimTime(time: number, duration: number): number {
  if (!(duration > 0)) return 0;
  const clamped = Math.min(Math.max(time, 0), duration);
  return Math.round(clamped * TIME_STEP) / TIME_STEP;
}

/**
 * 拖动某一端手柄后的新选区。另一端不动，被拖的一端最多贴到「另一端 ± 最小间隔」，
 * 拖过头就停住（不反弹、不推着另一端走）。
 */
export function trimRange(
  start: number,
  end: number,
  duration: number,
  which: TrimEnd,
  value: number,
): TrimRange {
  const time = clampTrimTime(value, duration);
  if (which === "start") {
    return { start: Math.min(time, Math.max(end - TRIM_MIN_GAP, 0)), end };
  }
  return { start, end: Math.max(time, Math.min(start + TRIM_MIN_GAP, duration)) };
}

/** 时长按「28.87 s」显示：两位小数，和手柄位置同一套精度 */
export function formatTrimSeconds(seconds: number): string {
  return `${Math.max(seconds, 0).toFixed(2)} s`;
}

/** 选区是不是等于全长（等于没修剪）。0.02 秒的余量吃掉拖动与取整的误差 */
export function isFullTrim(start: number, end: number, duration: number): boolean {
  return start <= 0.02 && end >= duration - 0.02;
}

/** 算确认按钮状态要的几项 */
export type TrimGate = TrimRange & { duration: number; phase: TrimPhase };

/**
 * ✓ 能不能点：只有「正在编辑」「剪掉了一部分」「选区不短于最小间隔」三条都成立才行。
 * 总时长为 0 时也点不了（加载中或量不出时长）。
 */
export function canConfirmTrim(gate: TrimGate): boolean {
  if (gate.phase !== "editing") return false;
  if (!(gate.duration > 0)) return false;
  if (isFullTrim(gate.start, gate.end, gate.duration)) return false;
  return gate.end - gate.start >= TRIM_MIN_GAP - 1e-6;
}

/** ✓ 的悬浮提示：点不了时写清为什么 */
export function confirmHint(gate: TrimGate): string {
  if (gate.end - gate.start < TRIM_MIN_GAP - 1e-6) return `选区至少 ${TRIM_MIN_GAP} 秒`;
  if (gate.duration <= 0 || isFullTrim(gate.start, gate.end, gate.duration)) {
    return "拖动两端选择要保留的片段";
  }
  return "确认修剪";
}

/**
 * 胶片条每一格缩略图取哪个时刻：把整段视频均分成 count 段，各取中点。
 * 取中点而不是等分点，缩略图才不会挤在最前面；总时长为 0 或不分格时返回空数组。
 */
export function thumbTimes(duration: number, count: number): number[] {
  if (!(duration > 0) || count <= 0) return [];
  const step = duration / count;
  return Array.from({ length: count }, (_, index) => Math.round(step * (index + 0.5) * 100) / 100);
}

/**
 * 产出文件名：源节点标题 + 「-剪辑」，扩展名照素材类型。
 * 文件名里不能出现的字符换成下划线，标题太长按键位截断（不能按字节截，会把汉字切坏）。
 */
export function trimFileName(label: string, ext: string): string {
  const safe = label.replace(/[\\/:*?"<>|]/g, "_").trim();
  const head = [...safe].slice(0, MAX_TITLE_CHARS).join("");
  return `${head}-剪辑.${ext}`;
}

/**
 * 编码前粗估产出体积：源文件大小 × 选区占全片的比例。
 * 只是粗估（转码后码率会变），用来在开始编码前拦下明显超上限的请求；
 * 总时长为 0 时按原大小算，宁可多拦一次，也不让用户白等一场编码再失败。
 */
export function estimatedTrimBytes(
  sourceBytes: number,
  start: number,
  end: number,
  duration: number,
): number {
  if (!(duration > 0)) return sourceBytes;
  const ratio = Math.min(Math.max((end - start) / duration, 0), 1);
  return Math.round(sourceBytes * ratio);
}

/** 匹配「可选的 http(s) 域名 + /files/ 路径」，和 media-lod、截帧里的写法一致 */
const FILES_URL = /^(?:https?:\/\/[^/?#]+)?\/files\//i;

/**
 * 修剪专用的取流地址：后端 /files/ 地址追加 trim=1，和画布播放、截帧的请求区分开。
 * 为什么：同一个地址若先被普通播放器加载过，浏览器可能把不带跨域头的缓存拿来复用，
 * 而修剪要在 WebCodecs 里读这段媒体、必须带跨域头，缓存一混就失败。
 * 对象存储会 302 到签名或公开地址，后端看到 trim=1 会跳到另一个地址，第二跳的缓存也分开。
 * 外链、blob 地址可能带签名，不能乱加参数；已经带过 trim=1 的地址不重复加。
 */
export function trimUrl(src: string): string {
  if (!FILES_URL.test(src)) return src;
  const hashAt = src.indexOf("#");
  const base = hashAt >= 0 ? src.slice(0, hashAt) : src;
  const hash = hashAt >= 0 ? src.slice(hashAt) : "";
  if (/(?:[?&])trim=1(?:&|$)/.test(base)) return src;
  return `${base}${base.includes("?") ? "&" : "?"}trim=1${hash}`;
}

/** 剪辑节点的默认标题 */
export const TRIM_NODE_LABEL = "剪辑";

/**
 * 修剪要建的新节点和它到源节点的连线。
 * 新节点是「上传中」的视频节点（uploaded: true，没有任何生成字段），所以选中后不出生成面板、
 * 不读入边当参考素材，连线只表示来源；编码和上传完成后由上传流程写回地址。
 * 摆在源节点右侧 80px 外、按新节点自己的画幅垂直居中，撞到别的节点就往下错开。
 * @param input.aspect 新节点的画幅（宽 / 高），取源视频的
 * @param input.fileName 产出文件名，上传流程会拿它当素材名
 * @param input.newId 生成节点 id（测试里可注入）
 * @param input.edgeId 生成连线 id
 */
export function buildTrimNode(input: {
  source: CanvasNode;
  nodes: FlowNode[];
  fileName: string;
  aspect: number;
  newId?: () => string;
  edgeId?: () => string;
}): { node: CanvasNode; edge: CanvasEdge } {
  const { source, nodes, fileName, aspect } = input;
  const newId = input.newId ?? (() => crypto.randomUUID());
  const edgeId = input.edgeId ?? (() => crypto.randomUUID());

  const label = newNodeLabel(
    TRIM_NODE_LABEL,
    nodes.map((node) => node.data.label),
  );
  const { x, y } = placeBeside({ source, nodes, size: nodeFrameSize(aspect) });
  const id = newId();
  const node: CanvasNode = {
    id,
    type: "canvas",
    position: { x, y },
    data: {
      kind: "video",
      label,
      status: "running",
      uploadProgress: 0,
      mediaType: "video",
      mediaAspect: aspect,
      uploaded: true,
      fileName,
    },
  };
  const edge: CanvasEdge = {
    id: edgeId(),
    source: source.id,
    target: id,
    sourceHandle: null,
    targetHandle: null,
    ...ANIMATED_EDGE_OPTIONS,
  };
  return { node, edge };
}
