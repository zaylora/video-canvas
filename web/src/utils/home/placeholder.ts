/**
 * 占位封面的渐变：接口还没有真实封面时，用色相做一块稳定的底，避免外链图片。
 * @param hue 色相，0–360，超出范围会按圆周取模
 * @returns 可直接放进 CSS background 的渐变
 */
export const placeholderBackground = (hue: number) => {
  const base = ((hue % 360) + 360) % 360;
  return `linear-gradient(135deg, oklch(0.66 0.13 ${base}), oklch(0.4 0.11 ${(base + 48) % 360}))`;
};

/**
 * 日期标题：「10月6日」
 * @param date 日期
 * @returns 月日文案
 */
export const formatDayTitle = (date: Date) => `${date.getMonth() + 1}月${date.getDate()}日`;
