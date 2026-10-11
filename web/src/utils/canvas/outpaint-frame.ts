import { parseAspect } from "./node-frame";

/**
 * 扩图框的几何（设计稿 6.17）。全部是纯函数，坐标是「相对原图左上角」的画布坐标（缩放为 100% 时的像素）：
 * 原图占 (0, 0) 到 (origin.w, origin.h)，框 x0 / y0 ≤ 0，x1 / y1 ≥ 原图的宽 / 高，所以框始终包住原图。
 */

/** 原图在画布上的大小（画布坐标） */
export type OriginSize = { w: number; h: number };

/** 扩图框：相对原图左上角的四条边 */
export type OutpaintFrame = { x0: number; y0: number; x1: number; y1: number };

/** 8 个缩放手柄：四条边是自由比例，四个角锁定当前比例 */
export type FrameHandle = "t" | "b" | "l" | "r" | "tl" | "tr" | "bl" | "br";

/** 框的最长边最多是原图最长边的几倍 */
export const MAX_MULT = 3;

/** 倍数菜单里的选项（只在「原比例」下可选） */
export const OUTPAINT_MULTS = [1, 1.25, 1.5, 2, 3] as const;

/** 比例条上的预设；r 是宽 / 高 */
export const RATIO_PRESETS = [
  { key: "21:9", r: 21 / 9 },
  { key: "4:3", r: 4 / 3 },
  { key: "1:1", r: 1 },
  { key: "3:4", r: 3 / 4 },
  { key: "9:16", r: 9 / 16 },
] as const;

/** 判断「比例相同」的相对误差 */
const RATIO_EPS = 0.004;

/** 整数比的分子、分母最大取多少 */
const RATIO_MAX_TERM = 24;

export const frameWidth = (frame: OutpaintFrame) => frame.x1 - frame.x0;
export const frameHeight = (frame: OutpaintFrame) => frame.y1 - frame.y0;

/** 框的最长边上限（画布坐标） */
export const frameCap = (origin: OriginSize) => MAX_MULT * Math.max(origin.w, origin.h);

/** 以原图为中心、宽高为 w × h 的框 */
function centered(origin: OriginSize, w: number, h: number): OutpaintFrame {
  const dx = (w - origin.w) / 2;
  const dy = (h - origin.h) / 2;
  // 0 - dx 而不是 -dx：dx 为 0 时后者是 -0，比较和序列化都会出怪结果
  return { x0: 0 - dx, y0: 0 - dy, x1: origin.w + dx, y1: origin.h + dy };
}

/** 原比例下放大 mult 倍的框，原图居中 */
export function frameByMult(origin: OriginSize, mult: number): OutpaintFrame {
  return centered(origin, origin.w * mult, origin.h * mult);
}

/**
 * 比例预设的框：刚好包住原图的最小框，原图居中。
 * @param ratio 目标宽 / 高
 */
export function frameByRatio(origin: OriginSize, ratio: number): OutpaintFrame {
  return origin.w / origin.h >= ratio
    ? centered(origin, origin.w, origin.w / ratio)
    : centered(origin, origin.h * ratio, origin.h);
}

/** 框的最长边有没有超过 3 倍上限 */
export function fitsCap(origin: OriginSize, frame: OutpaintFrame): boolean {
  return Math.max(frameWidth(frame), frameHeight(frame)) <= frameCap(origin) + 0.01;
}

/** 框的最长边是原图最长边的几倍 */
export function longMult(origin: OriginSize, frame: OutpaintFrame): number {
  return Math.max(frameWidth(frame), frameHeight(frame)) / Math.max(origin.w, origin.h);
}

/**
 * 当前框的比例对应比例条上的哪个按钮：原图比例优先，其次是预设，都不是（手柄拖出了自定义比例）返回 null。
 * @returns "orig"、预设名（如 "21:9"）或 null
 */
export function detectRatio(origin: OriginSize, frame: OutpaintFrame): "orig" | string | null {
  const ratio = frameWidth(frame) / frameHeight(frame);
  if (Math.abs(ratio / (origin.w / origin.h) - 1) < RATIO_EPS) return "orig";
  const hit = RATIO_PRESETS.find((preset) => Math.abs(ratio / preset.r - 1) < RATIO_EPS);
  return hit ? hit.key : null;
}

const clamp = (value: number, min: number, max: number) => Math.min(Math.max(value, min), max);

/** 平移框：大小不变，只能在「仍然包住原图」的范围里动 */
export function moveFrame(
  origin: OriginSize,
  frame: OutpaintFrame,
  dx: number,
  dy: number,
): OutpaintFrame {
  const w = frameWidth(frame);
  const h = frameHeight(frame);
  const x0 = clamp(frame.x0 + dx, origin.w - w, 0);
  const y0 = clamp(frame.y0 + dy, origin.h - h, 0);
  return { x0, y0, x1: x0 + w, y1: y0 + h };
}

/**
 * 按手柄和位移算新框。边手柄只动一条边（自由比例），角手柄锁定当前比例、对角固定；
 * 框始终包住原图、最长边不超过 3 倍。
 * @param start 开始拖动时的框
 * @param dx 横向位移（画布坐标，已除过缩放）
 * @param dy 纵向位移
 * @returns 新框，以及这次是不是被 3 倍上限拦住了（用来弹提示）
 */
export function resizeFrame(
  handle: FrameHandle,
  origin: OriginSize,
  start: OutpaintFrame,
  dx: number,
  dy: number,
): { frame: OutpaintFrame; limited: boolean } {
  const cap = frameCap(origin);
  let { x0, y0, x1, y1 } = start;
  let limited = false;

  if (handle.length === 1) {
    if (handle === "l") {
      const want = start.x0 + dx;
      x0 = clamp(want, start.x1 - cap, 0);
      limited = want < start.x1 - cap;
    } else if (handle === "r") {
      const want = start.x1 + dx;
      x1 = clamp(want, origin.w, start.x0 + cap);
      limited = want > start.x0 + cap;
    } else if (handle === "t") {
      const want = start.y0 + dy;
      y0 = clamp(want, start.y1 - cap, 0);
      limited = want < start.y1 - cap;
    } else {
      const want = start.y1 + dy;
      y1 = clamp(want, origin.h, start.y0 + cap);
      limited = want > start.y0 + cap;
    }
    return { frame: { x0, y0, x1, y1 }, limited };
  }

  // 角：锁定当前比例，对角固定
  const sw = frameWidth(start);
  const sh = frameHeight(start);
  const aspect = sw / sh;
  const hx = handle.includes("l") ? -1 : 1;
  const hy = handle.includes("t") ? -1 : 1;
  const ax = hx < 0 ? start.x1 : start.x0;
  const ay = hy < 0 ? start.y1 : start.y0;
  const want = (sw + hx * dx + (sh + hy * dy) * aspect) / 2;
  // 最小：刚好包住原图；最大：最长边不超过上限
  const lo = Math.max(hx > 0 ? origin.w - ax : ax, (hy > 0 ? origin.h - ay : ay) * aspect);
  const hi = Math.min(cap, cap * aspect);
  const w = clamp(want, lo, Math.max(lo, hi));
  if (want > hi + 0.5) limited = true;
  const h = w / aspect;
  return {
    frame: {
      x0: hx > 0 ? ax : ax - w,
      x1: hx > 0 ? ax + w : ax,
      y0: hy > 0 ? ay : ay - h,
      y1: hy > 0 ? ay + h : ay,
    },
    limited,
  };
}

/**
 * 宽高比转整数比（如 16:9、24:1），模型的比例参数不收小数。
 * 分子、分母都在 1–24 里找数值误差最小的，并列取和更小的（所以 2 是 2:1 而不是 4:2）。
 * @param ratio 宽 / 高
 */
export function toRatio(ratio: number): string {
  let best = { p: 1, q: 1, err: Number.POSITIVE_INFINITY };
  for (let p = 1; p <= RATIO_MAX_TERM; p++) {
    for (let q = 1; q <= RATIO_MAX_TERM; q++) {
      const err = Math.abs(Math.log(p / q / ratio));
      if (err < best.err - 1e-9 || (Math.abs(err - best.err) <= 1e-9 && p + q < best.p + best.q)) {
        best = { p, q, err };
      }
    }
  }
  return `${best.p}:${best.q}`;
}

/**
 * 当前框要写进模型比例参数的值：预设按名字原样写（21:9 不化简成 7:3），
 * 原比例按原图宽高比，自定义比例取最接近的整数比。
 */
export function ratioOfFrame(origin: OriginSize, frame: OutpaintFrame): string {
  const detected = detectRatio(origin, frame);
  if (detected === "orig") return toRatio(origin.w / origin.h);
  if (detected) return detected;
  return toRatio(frameWidth(frame) / frameHeight(frame));
}

/**
 * 模型的比例参数是枚举时，取数值上最接近 ratio 的一项；选项里读不出比例的（Auto 等）跳过。
 * 没有可用选项就原样返回。
 * @param ratio 形如 "16:9"
 * @param options 模型比例参数的可选值
 */
export function snapRatio(ratio: string, options: readonly unknown[]): string {
  const target = parseAspect(ratio);
  if (target === null) return ratio;
  let best: { option: string; err: number } | null = null;
  for (const option of options) {
    const value = parseAspect(option);
    if (value === null) continue;
    const err = Math.abs(Math.log(value / target));
    if (!best || err < best.err) best = { option: String(option), err };
  }
  return best?.option ?? ratio;
}
