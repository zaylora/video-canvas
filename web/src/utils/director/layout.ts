/** 取景框的宽高（像素） */
export type Frame = { width: number; height: number };

/** 浮窗里取景框的上限：宽 296；竖画幅时最高 236，免得浮窗把视口占满 */
export const FLOATING_FRAME = { width: 296, maxHeight: 236 } as const;

/**
 * 在给定的空间里放下最大的、保持画幅的取景框（左右分栏时用），结果取整。
 * 空间还没量出来（宽或高不是正数）时返回 0×0，调用方不画。
 * @param boxWidth 可用宽度
 * @param boxHeight 可用高度
 * @param ratio 画幅宽高比
 */
export function fitFrame(boxWidth: number, boxHeight: number, ratio: number): Frame {
  if (!(boxWidth > 0) || !(boxHeight > 0) || !(ratio > 0)) return { width: 0, height: 0 };
  if (boxWidth / boxHeight > ratio) {
    // 空间比画幅更扁：高度顶满
    return { width: Math.round(boxHeight * ratio), height: Math.round(boxHeight) };
  }
  return { width: Math.round(boxWidth), height: Math.round(boxWidth / ratio) };
}

/** 浮窗里的取景框：按宽度定高，竖画幅太高就按最大高度收 */
export function floatingFrame(ratio: number): Frame {
  const width = FLOATING_FRAME.width;
  const height = Math.round(width / ratio);
  if (height <= FLOATING_FRAME.maxHeight) return { width, height };
  return {
    width: Math.round(FLOATING_FRAME.maxHeight * ratio),
    height: FLOATING_FRAME.maxHeight,
  };
}
