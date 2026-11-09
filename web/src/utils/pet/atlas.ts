import type { PetRow } from "./rows";

/** 默认选择器：渲染层的雪碧图元素带这个类名 */
export const PET_SPRITE_CLASS = "pet-sprite";

const fmt = (n: number) => String(Math.round(n * 10000) / 10000);
/** 偏移取负，0 不写成 -0 */
const px = (n: number) => (n === 0 ? "0px" : `${-n}px`);

/**
 * 生成每行动作的 @keyframes 和按 data-row 选中动画的规则。
 *
 * CSS 的 steps() 只能做等长帧，而每行最后一帧停留更久，所以按帧时长累加出百分比，
 * 再配合 step-end 让每个关键帧一直保持到下一个：
 * 最后一帧从自己的百分比一直保持到 100%，停留时间正好是它的时长。
 *
 * 元素上的数据属性约定：
 * - data-row：当前动作行号；
 * - data-loop="false"：只播一轮并停在最后一帧；
 * - data-paused="true"：暂停（页面在后台时）。
 * 系统开启「减少动态效果」时不播动画，每行只显示第 0 帧。
 */
export function buildKeyframes(
  rows: readonly PetRow[],
  cell: { width: number; height: number },
  opts: { selector?: string } = {},
): string {
  const sel = opts.selector ?? `.${PET_SPRITE_CLASS}`;
  const out: string[] = [
    `${sel}{animation-timing-function:step-end;animation-iteration-count:infinite}`,
  ];

  for (const r of rows) {
    const y = r.row * cell.height;
    const total = r.durations.reduce((a, b) => a + b, 0);
    let acc = 0;
    const frames = r.durations.map((d, i) => {
      const at = `${fmt((acc / total) * 100)}%`;
      acc += d;
      return `${at}{background-position:${px(i * cell.width)} ${px(y)}}`;
    });
    frames.push(`100%{background-position:${px((r.frames - 1) * cell.width)} ${px(y)}}`);
    out.push(`@keyframes pet-row-${r.row}{${frames.join("")}}`);
  }
  for (const r of rows) {
    const total = r.durations.reduce((a, b) => a + b, 0);
    out.push(
      `${sel}[data-row="${r.row}"]{animation-name:pet-row-${r.row};animation-duration:${total}ms}`,
    );
  }

  out.push(`${sel}[data-loop="false"]{animation-iteration-count:1;animation-fill-mode:forwards}`);
  out.push(`${sel}[data-paused="true"]{animation-play-state:paused}`);
  out.push(
    `@media (prefers-reduced-motion:reduce){${sel}[data-row]{animation:none}${rows
      .map((r) => `${sel}[data-row="${r.row}"]{background-position:0px ${px(r.row * cell.height)}}`)
      .join("")}}`,
  );
  return out.join("\n") + "\n";
}
