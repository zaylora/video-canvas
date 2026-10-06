/** 断开按钮在屏幕上的缩放系数的上下限：缩到很小也点得到，放到很大也不抢素材的戏 */
const CUT_SCALE_MIN = 0.8;
const CUT_SCALE_MAX = 1.2;
/** 按钮跟着画布缩放的灵敏度：0 完全不跟，1 和画布一样大小 */
const CUT_SCALE_FOLLOW = 0.35;

/**
 * 决定当前连线是否显示断开按钮，避免多选连线时每条线都浮出按钮。
 */
export const shouldShowCutButton = (selected: boolean, selectedEdgeCount: number) =>
  selected && selectedEdgeCount === 1;

/**
 * 断开按钮在屏幕上的缩放系数（相对原大小）：轻微跟着画布缩放，并夹在 0.8 到 1.2 之间。
 * 按钮挂在随画布缩放的图层里，实际要乘 `系数 / zoom` 才能抵掉画布自己的缩放。
 */
export const cutButtonScale = (zoom: number) =>
  Math.min(CUT_SCALE_MAX, Math.max(CUT_SCALE_MIN, zoom ** CUT_SCALE_FOLLOW));

type Point = { x: number; y: number };

const sqDist = (a: Point, b: Point) => (a.x - b.x) ** 2 + (a.y - b.y) ** 2;

/**
 * 把一个点投影到曲线上，返回离它最近的位置，用 0 到 1 的路径比例表示。
 * sample(t) 给出曲线上比例 t 处的点（SVG 路径用 getPointAtLength）。
 * 先粗采样找最近的一段，再在这一段里细采样，不用解曲线方程。
 */
export function nearestRatio(sample: (t: number) => Point, point: Point, steps = 64): number {
  let best = 0;
  let bestDist = Infinity;
  for (let i = 0; i <= steps; i += 1) {
    const dist = sqDist(sample(i / steps), point);
    if (dist < bestDist) {
      bestDist = dist;
      best = i / steps;
    }
  }
  const lo = Math.max(0, best - 1 / steps);
  const hi = Math.min(1, best + 1 / steps);
  for (let i = 0; i <= 16; i += 1) {
    const t = lo + ((hi - lo) * i) / 16;
    const dist = sqDist(sample(t), point);
    if (dist < bestDist) {
      bestDist = dist;
      best = t;
    }
  }
  return best;
}
