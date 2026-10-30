/** 比例图标的画布边长（px），所有比例都画在这么大的正方形里 */
export const RATIO_ICON_BOX = 20;

/** 比例框的长边（px），留 1px 给描边外沿和抗锯齿 */
const LONG_SIDE = 18;

/** 描边粗细（px） */
const STROKE = 1.5;

/** 圆角上限（px） */
const MAX_RADIUS = 2;

const RATIO_RE = /^(\d+(?:\.\d+)?):(\d+(?:\.\d+)?)$/;

/** 比例框：外沿的位置和尺寸，描边画在外沿以内，所以外沿的宽高比就是标称比例 */
export type RatioFrame = {
  /** 外沿左上角 x（px） */
  x: number;
  /** 外沿左上角 y（px） */
  y: number;
  /** 外沿宽（px） */
  width: number;
  /** 外沿高（px） */
  height: number;
  /** 描边粗细（px） */
  stroke: number;
  /** 外沿圆角（px），按短边取，扁的比例圆角也小，不会变成胶囊 */
  radius: number;
};

/**
 * 按「宽:高」算比例框，长边固定、在画布里居中。
 * @param ratio 比例值，如 16:9
 * @returns 比例框；不是「宽:高」（如 Auto）时为 null
 */
export function ratioFrame(ratio: string): RatioFrame | null {
  const match = RATIO_RE.exec(ratio.trim());
  if (!match) return null;
  const w = Number(match[1]);
  const h = Number(match[2]);
  if (!(w > 0 && h > 0)) return null;
  const scale = LONG_SIDE / Math.max(w, h);
  const width = w * scale;
  const height = h * scale;
  return {
    x: (RATIO_ICON_BOX - width) / 2,
    y: (RATIO_ICON_BOX - height) / 2,
    width,
    height,
    stroke: STROKE,
    radius: Math.min(MAX_RADIUS, Math.min(width, height) / 5),
  };
}
