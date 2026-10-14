import type { NodeChange, NodePositionChange } from "@xyflow/react";

import type { FlowNode } from "@/types";

import {
  ENTER_PX,
  RELEASE_PX,
  alignmentGuides,
  findAxisSnap,
  type AxisLock,
  type Guide,
  type Rect,
  type SnapTarget,
} from "./alignment";
import { isGroupNode } from "./group";

/**
 * 一次拖动的对齐会话：拖动开始时建好，拖动中只读（锁和修正量除外）。
 * 位置变更里的 position 对组成员是相对坐标，所以用「开始时的相对位置 → 现在的相对位置」得到位移，
 * 叠到开始时的绝对矩形上，就是原始（未吸附）的绝对矩形，不必读 positionAbsolute。
 */
export type AlignSession = {
  nodeId: string;
  /** 开始时被拖节点的绝对矩形 */
  rect: Rect;
  /** 开始时被拖节点的 position（组成员是相对坐标） */
  start: { x: number; y: number };
  targets: SnapTarget[];
  /** 目标 id → 矩形，给每帧生成辅助线用，免得在 targets 里线性找 */
  rects: Map<string, Rect>;
  locks: { x?: AxisLock; y?: AxisLock };
  /** 最近一帧的修正量：松手那一帧不重算，直接复用它 */
  lastDelta: { x: number; y: number };
};

/** 宽高都量出来了才算：没量出来的不能当 0×0 去吸，也不能让别人吸它 */
function measuredSize(node: FlowNode): { width: number; height: number } | null {
  const width = node.measured?.width ?? node.width;
  const height = node.measured?.height ?? node.height;
  return typeof width === "number" && typeof height === "number" && width > 0 && height > 0
    ? { width, height }
    : null;
}

/**
 * 拖动开始：只有单个、非组、已量出尺寸的节点才建会话，其余（多选、拖组）返回 null，行为和以前一样。
 * 目标是其余已测量、可见的普通节点，组框不当目标；位置换算成画布绝对坐标。
 * 整批只遍历一次，父节点用 Map 取，节点再多也是 O(n)。
 */
export function startAlignSession(nodes: FlowNode[], dragged: FlowNode[]): AlignSession | null {
  if (dragged.length !== 1) return null;
  const [moving] = dragged;
  if (isGroupNode(moving)) return null;
  const size = measuredSize(moving);
  if (!size) return null;

  const byId = new Map(nodes.map((node) => [node.id, node]));
  /** 成员的 position 是相对组左上角的，加上组的位置才是画布坐标；父组找不到就按原位置 */
  const absolute = (node: FlowNode) => {
    const parent = node.parentId ? byId.get(node.parentId) : undefined;
    return parent
      ? { x: parent.position.x + node.position.x, y: parent.position.y + node.position.y }
      : node.position;
  };

  const targets: SnapTarget[] = [];
  const rects = new Map<string, Rect>();
  for (const node of nodes) {
    if (node.id === moving.id || node.hidden || isGroupNode(node)) continue;
    const targetSize = measuredSize(node);
    if (!targetSize) continue;
    const rect = { ...absolute(node), ...targetSize };
    targets.push({ id: node.id, rect });
    rects.set(node.id, rect);
  }
  return {
    nodeId: moving.id,
    rect: { ...absolute(moving), ...size },
    start: { ...moving.position },
    targets,
    rects,
    locks: {},
    lastDelta: { x: 0, y: 0 },
  };
}

const shift = (change: NodePositionChange, delta: { x: number; y: number }): NodePositionChange =>
  delta.x === 0 && delta.y === 0
    ? change
    : {
        ...change,
        position: change.position && {
          x: change.position.x + delta.x,
          y: change.position.y + delta.y,
        },
        positionAbsolute: change.positionAbsolute && {
          x: change.positionAbsolute.x + delta.x,
          y: change.positionAbsolute.y + delta.y,
        },
      };

/**
 * 改写这一批节点变更里被拖节点的位置变更，其余变更原样返回。
 * - zoom：阈值是屏幕像素，除以缩放换成画布坐标；
 * - 松手那一条（dragging: false）：xyflow 给的仍是原始坐标，必须复用最后一次的修正量，否则节点会在松手瞬间跳回去。
 * guides 为 null 表示这批变更与被拖节点的位置无关，界面上的辅助线不用动。
 * 会改 session 里的锁和 lastDelta。
 */
export function adjustForAlign(
  session: AlignSession,
  changes: NodeChange<FlowNode>[],
  zoom: number,
): { changes: NodeChange<FlowNode>[]; guides: Guide[] | null } {
  const index = changes.findIndex(
    (change) => change.type === "position" && change.id === session.nodeId && change.position,
  );
  if (index < 0) return { changes, guides: null };
  const change = changes[index] as NodePositionChange;
  const replace = (next: NodePositionChange) =>
    next === change ? changes : changes.map((item, i) => (i === index ? next : item));

  if (change.dragging === false) {
    return { changes: replace(shift(change, session.lastDelta)), guides: [] };
  }

  const scale = Math.max(zoom, 0.01);
  const enter = ENTER_PX / scale;
  const release = RELEASE_PX / scale;
  const raw: Rect = {
    ...session.rect,
    x: session.rect.x + (change.position!.x - session.start.x),
    y: session.rect.y + (change.position!.y - session.start.y),
  };
  const x = findAxisSnap(raw, session.targets, "x", enter, release, session.locks.x);
  const y = findAxisSnap(raw, session.targets, "y", enter, release, session.locks.y);
  session.locks = { x: x?.lock, y: y?.lock };
  session.lastDelta = { x: x?.delta ?? 0, y: y?.delta ?? 0 };

  // 辅助线按吸附之后的位置算，只连锁定的那个目标
  const snapped: Rect = { ...raw, x: raw.x + session.lastDelta.x, y: raw.y + session.lastDelta.y };
  const guides: Guide[] = [];
  for (const [axis, snap] of [
    ["x", x],
    ["y", y],
  ] as const) {
    const target = snap && session.rects.get(snap.lock.targetId);
    if (target) guides.push(...alignmentGuides(snapped, target, axis));
  }
  return { changes: replace(shift(change, session.lastDelta)), guides };
}
