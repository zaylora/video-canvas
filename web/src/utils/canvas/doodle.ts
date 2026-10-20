import { SELECTED_NODE_OVERLAY_Z_INDEX } from "./overlay-layer";
import { COMPOSE_MAX_EDGE } from "./outpaint-compose";

/**
 * 图片涂鸦里所有能单独讲的算法：色盘、画布逻辑尺寸、粗细与字号换算、撤销重做栈、
 * 有没有标注内容的判定。都不碰 Fabric 和 DOM，所以能直接测；真正的画布在 doodle-overlay.tsx。
 */

/** 涂鸦色盘：8 色，第 1 个红色是默认色 */
export const DOODLE_COLORS = [
  "#ef4444",
  "#f59e0b",
  "#facc15",
  "#22c55e",
  "#06b6d4",
  "#3b82f6",
  "#a855f7",
  "#ffffff",
] as const;

/** 默认颜色：色盘第 1 个（红） */
export const DEFAULT_DOODLE_COLOR: string = DOODLE_COLORS[0];

/** 粗细滑块的最小值（屏幕像素） */
export const DOODLE_WIDTH_MIN = 2;

/** 粗细滑块的最大值（屏幕像素） */
export const DOODLE_WIDTH_MAX = 20;

/** 粗细滑块的默认值（屏幕像素） */
export const DOODLE_WIDTH_DEFAULT = 6;

/** 一个像素尺寸 */
export type PixelSize = {
  /** 宽（像素） */
  width: number;
  /** 高（像素） */
  height: number;
};

/** 画布里的一块矩形区域 */
export type PixelRect = PixelSize & {
  /** 左上角的横坐标（相对节点左上角） */
  x: number;
  /** 左上角的纵坐标（相对节点左上角） */
  y: number;
};

/**
 * 涂鸦画布的逻辑尺寸：等于原图像素，长边超过上限时等比缩到上限（和扩图合成同一个口径）。
 * 逻辑尺寸就是笔迹坐标、粗细、字号的单位，屏幕上只用 CSS 缩小显示。
 * @param natural 原图真实像素
 * @param maxEdge 长边上限，默认 COMPOSE_MAX_EDGE
 * @returns 画布的宽高（整数，最短边至少 1）
 */
export function doodleCanvasSize(
  natural: { w: number; h: number },
  maxEdge: number = COMPOSE_MAX_EDGE,
): PixelSize {
  const longest = Math.max(natural.w, natural.h);
  const shrink = longest > maxEdge ? maxEdge / longest : 1;
  return {
    width: Math.max(1, Math.round(natural.w * shrink)),
    height: Math.max(1, Math.round(natural.h * shrink)),
  };
}

/**
 * 画布在节点框里怎么摆：按原图宽高比 contain 进节点框并居中。
 * 节点框的比例通常和原图一致（铺满），不一致时留边，保证笔迹不会被拉歪。
 * @param natural 原图真实像素；无效（非正数）时直接铺满节点框
 * @param box 节点框的大小（画布坐标）
 * @returns 画布的显示区域，相对节点左上角
 */
export function doodleDisplayRect(natural: { w: number; h: number }, box: PixelSize): PixelRect {
  if (!(natural.w > 0) || !(natural.h > 0)) return { x: 0, y: 0, ...box };
  const byWidth = (box.width * natural.h) / natural.w;
  const fitsHeight = byWidth <= box.height + 1e-6;
  const width = fitsHeight ? box.width : (box.height * natural.w) / natural.h;
  const height = fitsHeight ? byWidth : box.height;
  return { x: (box.width - width) / 2, y: (box.height - height) / 2, width, height };
}

/**
 * 涂鸦画布盖在节点上的定位样式（画布坐标）。层级要高于选中的节点，否则指针事件落到节点上，Fabric 收不到。
 * @param origin 节点左上角
 * @param rect doodleDisplayRect 的结果，相对节点左上角
 */
export function doodleStageStyle(
  origin: { x: number; y: number },
  rect: PixelRect,
): { transform: string; width: number; height: number; zIndex: number } {
  return {
    transform: `translate(${origin.x + rect.x}px, ${origin.y + rect.y}px)`,
    width: rect.width,
    height: rect.height,
    zIndex: SELECTED_NODE_OVERLAY_Z_INDEX,
  };
}

/**
 * 逻辑尺寸与显示尺寸的比：屏幕上 1 像素对应画布逻辑里多少像素。
 * @param logicalWidth 画布逻辑宽
 * @param displayWidth 画布在屏幕上的显示宽（含画布缩放）
 * @returns 比例；显示宽无效时按 1
 */
export function doodleScale(logicalWidth: number, displayWidth: number): number {
  return Number.isFinite(displayWidth) && displayWidth > 0 ? logicalWidth / displayWidth : 1;
}

/**
 * 笔画和框的逻辑粗细：用户在滑块上选的是屏幕粗细，乘比例才是导出时看到的同样比例。
 * @param screenWidth 滑块上的屏幕粗细
 * @param scale doodleScale 的结果
 */
export function doodleStrokeWidth(screenWidth: number, scale: number): number {
  return screenWidth * scale;
}

/**
 * 文字的逻辑字号：粗细越大字越大，字号 = (12 + 粗细 × 1.2) × 比例。
 * @param screenWidth 滑块上的屏幕粗细
 * @param scale doodleScale 的结果
 */
export function doodleFontSize(screenWidth: number, scale: number): number {
  return (12 + screenWidth * 1.2) * scale;
}

/** 判断有没有标注内容时只看的字段：文字对象有 text，笔画和框没有 */
export type DoodleItemLike = {
  /** 文字对象的内容；笔画和框没有这个字段 */
  text?: string;
};

/**
 * 有没有标注内容：笔画和框都算；文字只有非空白才算。
 * 保存前用它挡掉“什么都没画”的情况。
 * @param items 画布上已落定的对象
 */
export function hasDoodleContent(items: readonly DoodleItemLike[]): boolean {
  return items.some((item) => item.text === undefined || item.text.trim() !== "");
}

/** 撤销重做栈的操作集合 */
export type HistoryStack<T> = {
  /** 记下一个新落定的对象，同时清空重做栈 */
  push: (item: T) => void;
  /** 撤销最近的一个：从已落定里移到可重做；没有可撤销的返回 undefined */
  undo: () => T | undefined;
  /** 重做最近撤销的一个：放回已落定；没有可重做的返回 undefined */
  redo: () => T | undefined;
  /** 有没有可撤销的 */
  canUndo: () => boolean;
  /** 有没有可重做的 */
  canRedo: () => boolean;
  /** 当前已落定的对象，按落定顺序（副本） */
  items: () => T[];
};

/**
 * 撤销重做栈（Fabric 不自带）：一笔、一个框、一段文字各算一步。
 * 不依赖 Fabric，泛型放什么都行；调用方负责在画布上增删对应的对象。
 */
export function createHistoryStack<T>(): HistoryStack<T> {
  const done: T[] = [];
  const undone: T[] = [];
  return {
    push: (item) => {
      done.push(item);
      undone.length = 0;
    },
    undo: () => {
      if (done.length === 0) return undefined;
      const item = done.pop() as T;
      undone.push(item);
      return item;
    },
    redo: () => {
      if (undone.length === 0) return undefined;
      const item = undone.pop() as T;
      done.push(item);
      return item;
    },
    canUndo: () => done.length > 0,
    canRedo: () => undone.length > 0,
    items: () => [...done],
  };
}
