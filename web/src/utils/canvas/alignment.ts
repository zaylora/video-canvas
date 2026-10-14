export type Axis = "x" | "y";
/** 0 = 起点（左/上），1 = 中心，2 = 终点（右/下） */
export type AnchorIndex = 0 | 1 | 2;

export type Rect = { x: number; y: number; width: number; height: number };
export type SnapTarget = { id: string; rect: Rect };

/** 一条辅助线：axis 为 x 是竖线（x = value），y 是横线；from/to 是线沿另一轴的起止 */
export type Guide = { axis: Axis; value: number; from: number; to: number };

/** 上一帧吸住的是谁的哪个锚点：已吸住时用更大的释放距离判断，边界附近才不会来回跳 */
export type AxisLock = { targetId: string; movingAnchor: AnchorIndex; value: number };

export type AxisSnap = {
  /** 要加到原始坐标上的修正量 */
  delta: number;
  lock: AxisLock;
};

/** 进入吸附、释放吸附的距离，都是屏幕像素，用时除以缩放换成画布坐标 */
export const ENTER_PX = 4;
export const RELEASE_PX = 8;
/** 两个锚点相差不到这么多（画布坐标）就算对齐，吸附后的浮点误差在这之内 */
const ALIGNED_EPSILON = 0.5;

const ANCHORS: readonly AnchorIndex[] = [0, 1, 2];

export function anchorValue(rect: Rect, axis: Axis, anchor: AnchorIndex): number {
  const start = axis === "x" ? rect.x : rect.y;
  const size = axis === "x" ? rect.width : rect.height;
  return anchor === 0 ? start : anchor === 2 ? start + size : start + size / 2;
}

/**
 * 在一条轴上找吸附：被拖矩形的左/中/右（上/中/下）对其他节点的左/中/右，取差最小的一对。
 * - 已有锁、目标还在、差不超过 release：保持，不重新选；
 * - 否则只接受差严格小于 enter 的，平手取先遇到的（目标按传入顺序，锚点按 起点、中心、终点）。
 * moving 必须是原始（未吸附）的位置，别在已吸附的位置上再算，否则会越吸越偏。
 */
export function findAxisSnap(
  moving: Rect,
  targets: readonly SnapTarget[],
  axis: Axis,
  enter: number,
  release: number,
  lock?: AxisLock,
): AxisSnap | null {
  if (lock && targets.some((target) => target.id === lock.targetId)) {
    const delta = lock.value - anchorValue(moving, axis, lock.movingAnchor);
    if (Math.abs(delta) <= release) return { delta, lock };
  }

  let best: AxisSnap | null = null;
  let nearest = enter;
  for (const target of targets) {
    for (const movingAnchor of ANCHORS) {
      const from = anchorValue(moving, axis, movingAnchor);
      for (const targetAnchor of ANCHORS) {
        const value = anchorValue(target.rect, axis, targetAnchor);
        const delta = value - from;
        const distance = Math.abs(delta);
        if (distance >= nearest) continue;
        nearest = distance;
        best = { delta, lock: { targetId: target.id, movingAnchor, value } };
      }
    }
  }
  return best;
}

/**
 * 吸附之后要画的线：被拖节点和目标在这条轴上所有对齐的锚点各一条（顶/中/底同时对齐就是三条）。
 * 线只画在两个节点之间的空隙里，不贯穿节点；另一轴上有重叠就画重叠的那一段；贴着没有空隙就不画。
 * moving 要传吸附修正之后的位置。
 */
export function alignmentGuides(moving: Rect, target: Rect, axis: Axis): Guide[] {
  const along = axis === "x" ? "y" : "x";
  const size = axis === "x" ? "height" : "width";
  const m0 = moving[along];
  const m1 = m0 + moving[size];
  const t0 = target[along];
  const t1 = t0 + target[size];
  const from = m1 < t0 ? m1 : t1 < m0 ? t1 : Math.max(m0, t0);
  const to = m1 < t0 ? t0 : t1 < m0 ? m0 : Math.min(m1, t1);
  if (to <= from) return [];

  const values: number[] = [];
  for (const movingAnchor of ANCHORS) {
    const mine = anchorValue(moving, axis, movingAnchor);
    for (const targetAnchor of ANCHORS) {
      const value = anchorValue(target, axis, targetAnchor);
      if (Math.abs(value - mine) <= ALIGNED_EPSILON && !values.includes(value)) values.push(value);
    }
  }
  return values.sort((a, b) => a - b).map((value) => ({ axis, value, from, to }));
}
