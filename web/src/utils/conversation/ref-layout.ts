/** 参考图块的尺寸（px） */
export type RefSize = { w: number; h: number };

/** 展开时相邻参考图的间距 */
export const REF_GAP = 10;

/** 叠放时最多露出几张，更多的藏在后面 */
export const STACK_MAX = 3;

/** 叠放时每一张相对前一张向右错开多少 */
const STACK_DX = 6;

/** 叠放时各张的倾斜角度（度）：像随手摞起来的一叠 */
const STACK_ROTATE = [-6, 1, 7] as const;

/** 展开时各张的倾斜角度（度）：循环使用，每张略有不同 */
const SPREAD_ROTATE = [-4, 3, -2, 4] as const;

/** 收成小加号后的圆钮直径 */
export const BADGE_SIZE = 32;

/** 一张参考图的摆放：位置、倾斜、可见性和层级 */
export type RefSlot = {
  /** 相对容器左上角的横向偏移 */
  x: number;
  /** 相对容器左上角的纵向偏移 */
  y: number;
  /** 倾斜角度（度） */
  rotate: number;
  /** 透明度：叠放时超出 STACK_MAX 的藏起来 */
  opacity: number;
  /** 层级：后加的在上面 */
  z: number;
};

/**
 * 参考图的摆放。收起时叠成一摞（只露前 3 张，更多的藏在后面）；展开（悬停或聚焦）时一字排开。
 * @param index 第几张，从 0 开始
 * @param expanded 是否展开
 * @param size 单张尺寸
 * @returns 摆放
 */
export function refSlot(index: number, expanded: boolean, size: RefSize): RefSlot {
  if (expanded) {
    return {
      x: index * (size.w + REF_GAP),
      y: 0,
      rotate: SPREAD_ROTATE[index % SPREAD_ROTATE.length],
      opacity: 1,
      z: index,
    };
  }
  const rank = Math.min(index, STACK_MAX - 1);
  return {
    x: rank * STACK_DX,
    y: 0,
    rotate: STACK_ROTATE[rank],
    opacity: index < STACK_MAX ? 1 : 0,
    z: index,
  };
}

/**
 * 收起时占的宽度：不随展开变化，展开的内容浮在输入框文字上方，不把文字挤开。
 * @param count 参考图张数
 * @param size 单张尺寸
 * @returns 宽度
 */
export function collapsedWidth(count: number, size: RefSize): number {
  const stacked = Math.max(1, Math.min(count, STACK_MAX));
  return size.w + (stacked - 1) * STACK_DX;
}

/**
 * 展开后整行（所有参考图 + 最后的上传块）的宽度，含各张之间的间隙。
 * 展开内容溢出容器，间隙不属于任何一张，鼠标走过间隙会被当成离开；用这个宽度铺一块透明区域盖住它。
 * @param count 参考素材份数
 * @param size 单张尺寸
 * @returns 宽度
 */
export function expandedWidth(count: number, size: RefSize): number {
  return (count + 1) * size.w + count * REF_GAP;
}

/** 「+」按钮的摆放：整块上传块，或收成右下角的小圆钮 */
export type AddSlot = {
  x: number;
  y: number;
  w: number;
  h: number;
  /** 圆角半径 */
  radius: number;
  rotate: number;
  /** true 是整块上传块（带虚线框），false 是小圆钮 */
  tile: boolean;
};

/**
 * 「+」按钮的摆放：没有参考图或展开时是整块上传块（排在最后一张后面），
 * 有参考图且收起时缩成压在右下角的小圆钮。
 * @param count 参考图张数
 * @param expanded 是否展开
 * @param size 单张尺寸
 * @returns 摆放
 */
export function addSlot(count: number, expanded: boolean, size: RefSize): AddSlot {
  if (count === 0 || expanded) {
    return {
      x: count * (size.w + REF_GAP),
      y: 0,
      w: size.w,
      h: size.h,
      radius: 10,
      rotate: -6,
      tile: true,
    };
  }
  return {
    x: collapsedWidth(count, size) - BADGE_SIZE * 0.55,
    y: size.h - BADGE_SIZE * 0.55,
    w: BADGE_SIZE,
    h: BADGE_SIZE,
    radius: BADGE_SIZE / 2,
    rotate: 0,
    tile: false,
  };
}
