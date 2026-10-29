/** 松手时按速度往前「推」多远（秒）：速度 × 这个时间 = 惯性滑出的距离 */
const PROJECT_SECONDS = 0.2;

/** 速度超过这个（px/s）算「拨了一下」：哪怕没拖过半格也换到下一版 */
const FLICK_SPEED = 400;

/**
 * 版本轮播打开时停在哪一版：有采用的停在采用的那一版，没有（或采用的已不在列表里）停在最新一版。
 * @param takes 这个片段的各版结果，按创建时间正序
 * @param chosenTaskId 采用的那一版；空串表示没有
 */
export function initialTakeIndex(
  takes: readonly { taskId: string }[],
  chosenTaskId: string,
): number {
  const chosen = takes.findIndex((take) => take.taskId === chosenTaskId);
  if (chosen >= 0) return chosen;
  return Math.max(0, takes.length - 1);
}

/**
 * 松手后被「吸」到哪一版：按当前位移加上惯性预测的落点，取离中间最近的一格，夹在两头之间；
 * 拨得够快时至少换一格（不然轻轻一拨会被弹回原处，手感发黏）。
 * @param x 轨道当前位移（停在第 i 版时 x = -i * step）
 * @param velocity 松手时的速度（px/s，向左为负）
 * @param step 相邻两版的间距（px）
 * @param count 一共几版
 * @param from 开始拖动时停在第几版；不给就不做「至少一格」
 */
export function snapTakeIndex(
  x: number,
  velocity: number,
  step: number,
  count: number,
  from?: number,
): number {
  if (count <= 1 || step <= 0) return 0;
  const clamp = (index: number) => Math.min(count - 1, Math.max(0, index));
  let index = clamp(Math.round(-(x + velocity * PROJECT_SECONDS) / step));
  if (from !== undefined && index === from && Math.abs(velocity) > FLICK_SPEED) {
    index = clamp(from + (velocity < 0 ? 1 : -1));
  }
  return index;
}
