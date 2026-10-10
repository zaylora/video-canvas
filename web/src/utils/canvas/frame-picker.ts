import { clampFrameTime, lastFrameTime } from "./frame-capture";

/** 一次自定义截帧最多暂存几帧：和胶片的格数差不多，再多一屏放不下 */
export const MAX_STAGED_FRAMES = 9;

/** 方向键微调一次走多少秒：视频帧率不知道，按常见的 30fps 取一帧 */
export const FRAME_STEP = 1 / 30;

/** 胶片每一格大约多宽（像素） */
const THUMB_CELL_WIDTH = 64;
const THUMB_MIN = 4;
const THUMB_MAX = 14;

/** 胶片一共几格：按轨道宽度算，有上下限 */
export const thumbCount = (trackWidth: number): number =>
  Math.min(THUMB_MAX, Math.max(THUMB_MIN, Math.round(trackWidth / THUMB_CELL_WIDTH)));

/** 每一格取该段正中间的画面，所以第一格不是 0 秒、最后一格也不是尾 */
export function thumbTimes(duration: number, count: number): number[] {
  if (duration <= 0 || count <= 0) return [];
  return Array.from({ length: count }, (_, i) => ((i + 0.5) * duration) / count);
}

/** 轨道上的比例（0 到 1）换成时刻：1 是尾帧，略早于时长 */
export const timeFromRatio = (ratio: number, duration: number): number =>
  clampFrameTime(Math.min(Math.max(ratio, 0), 1) * duration, duration);

/** 时刻换成轨道上的比例（0 到 1）；时长未知为 0 */
export const ratioOfTime = (time: number, duration: number): number =>
  duration > 0 ? Math.min(Math.max(time / duration, 0), 1) : 0;

/**
 * 键盘微调播放头：方向键走一帧，按住 Shift 走一秒。
 * @param direction 1 往后，-1 往前
 */
export function stepFrameTime(
  time: number,
  direction: 1 | -1,
  big: boolean,
  duration: number,
): number {
  const next = time + direction * (big ? 1 : FRAME_STEP);
  return Math.min(Math.max(next, 0), lastFrameTime(duration));
}

/** 比 60fps 的一帧（约 0.0167 秒）还近，才算同一个时刻；相邻的两帧不会被误当成重复 */
const DUPLICATE_WINDOW = 0.015;

/**
 * 能不能再暂存一帧：已经到上限，或者这个时刻已经收过（一帧之内），都不行。
 */
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
