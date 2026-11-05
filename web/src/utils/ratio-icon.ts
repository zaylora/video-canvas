/** 比例框的短边（px）：所有比例短边一样长，就是 1:1 方框的边长 */
const SHORT_SIDE = 12;

/** 图标留给描边外沿和抗锯齿的边距（px），四周各一份 */
const PAD = 1;

/** 横向比例的长边上限（px）：再扁的比例也不会把图标拉得无限长 */
const MAX_WIDE = 30;

/** 比例图标的高度（px），所有比例一样高，放在一排时下面的文字对齐；够放下 9:16 */
export const RATIO_ICON_HEIGHT = 24;

/** 竖向比例的长边上限（px）：受图标高度限制 */
const MAX_TALL = RATIO_ICON_HEIGHT - PAD * 2;

/** Auto 等没有比例框的图标宽度（px），和 1:1 一样宽 */
const PLAIN_WIDTH = SHORT_SIDE + PAD * 2 + 2;

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

/** 解析「宽:高」，不是比例值时为 null */
function parseRatio(ratio: string): { w: number; h: number } | null {
  const match = RATIO_RE.exec(ratio.trim());
  if (!match) return null;
  const w = Number(match[1]);
  const h = Number(match[2]);
  return w > 0 && h > 0 ? { w, h } : null;
}

/** 比例框的外沿宽高：短边固定，长边按比例，超过上限时封顶 */
function frameSize(ratio: string): { width: number; height: number } | null {
  const parsed = parseRatio(ratio);
  if (!parsed) return null;
  const { w, h } = parsed;
  return w >= h
    ? { width: Math.min(MAX_WIDE, (SHORT_SIDE * w) / h), height: SHORT_SIDE }
    : { width: SHORT_SIDE, height: Math.min(MAX_TALL, (SHORT_SIDE * h) / w) };
}

/**
 * 比例图标的尺寸：高度固定，宽度随比例变（横的宽、竖的和 1:1 一样窄）。
 * @param ratio 比例值，如 16:9；Auto 等非比例值给一个和 1:1 差不多宽的格子
 */
export function ratioIconBox(ratio: string): { width: number; height: number } {
  const size = frameSize(ratio);
  return {
    width: size ? Math.ceil(size.width) + PAD * 2 : PLAIN_WIDTH,
    height: RATIO_ICON_HEIGHT,
  };
}

/**
 * 按「宽:高」算比例框：短边固定（和 1:1 一样大），长边按比例，在图标里居中。
 * @param ratio 比例值，如 16:9
 * @returns 比例框；不是「宽:高」（如 Auto）时为 null
 */
export function ratioFrame(ratio: string): RatioFrame | null {
  const size = frameSize(ratio);
  if (!size) return null;
  const box = ratioIconBox(ratio);
  const { width, height } = size;
  return {
    x: (box.width - width) / 2,
    y: (box.height - height) / 2,
    width,
    height,
    stroke: STROKE,
    radius: Math.min(MAX_RADIUS, Math.min(width, height) / 5),
  };
}
