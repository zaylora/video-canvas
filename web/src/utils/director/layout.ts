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

/** 可以拖动调节的面板：资产面板宽、属性栏宽、时间轴高、机位画面在分栏里占的比例 */
export type PanelKind = "assets" | "sidebar" | "timeline" | "split";

/** 各面板的尺寸；assets / sidebar 是宽（像素），timeline 是高（像素），split 是机位画面占视口区的比例 */
export type PanelSizes = Record<PanelKind, number>;

/** 各面板的范围和默认值 */
export const PANEL_LIMITS: Record<PanelKind, { min: number; max: number; default: number }> = {
  assets: { min: 200, max: 480, default: 272 },
  sidebar: { min: 240, max: 480, default: 280 },
  timeline: { min: 140, max: 480, default: 200 },
  split: { min: 0.25, max: 0.75, default: 0.5 },
};

export const DEFAULT_PANELS: PanelSizes = {
  assets: PANEL_LIMITS.assets.default,
  sidebar: PANEL_LIMITS.sidebar.default,
  timeline: PANEL_LIMITS.timeline.default,
  split: PANEL_LIMITS.split.default,
};

/** 左侧工具竖条的宽、顶栏的高（固定，不可调） */
export const NAV_WIDTH = 52;
export const HEADER_HEIGHT = 48;

/** 无论面板怎么拖，3D 视口区至少留这么大 */
export const MIN_VIEWPORT = { width: 360, height: 220 } as const;

/** 夹在这个面板自己的范围里；不是有限的数字就回默认值 */
export function clampPanel(kind: PanelKind, value: number): number {
  const { min, max, default: fallback } = PANEL_LIMITS[kind];
  if (!Number.isFinite(value)) return fallback;
  return Math.min(max, Math.max(min, value));
}

/**
 * 按窗口大小把一组面板尺寸收到放得下：视口区至少留 MIN_VIEWPORT。
 * 窗口变小（或还原了很大的偏好）时用；宽不够先收资产面板、再收属性栏，高不够收时间轴，
 * 都不会收到比各自的最小值还小。
 * @param assetsOpen 资产面板展开着才占宽度
 */
export function fitPanels(
  sizes: PanelSizes,
  area: { width: number; height: number },
  assetsOpen: boolean,
): PanelSizes {
  let assets = clampPanel("assets", sizes.assets);
  let sidebar = clampPanel("sidebar", sizes.sidebar);
  let timeline = clampPanel("timeline", sizes.timeline);
  let overflow = NAV_WIDTH + (assetsOpen ? assets : 0) + sidebar + MIN_VIEWPORT.width - area.width;
  if (overflow > 0 && assetsOpen) {
    const cut = Math.min(overflow, assets - PANEL_LIMITS.assets.min);
    assets -= cut;
    overflow -= cut;
  }
  if (overflow > 0) {
    sidebar -= Math.min(overflow, sidebar - PANEL_LIMITS.sidebar.min);
  }
  const maxTimeline = area.height - HEADER_HEIGHT - MIN_VIEWPORT.height;
  timeline = Math.max(PANEL_LIMITS.timeline.min, Math.min(timeline, maxTimeline));
  return { assets, sidebar, timeline, split: clampPanel("split", sizes.split) };
}

/**
 * 拖某个面板时它能拉到多大：不超过自己的最大值，也要给视口区留够 MIN_VIEWPORT（拉到别的面板的尺寸不动）。
 * 比最小值还小时就取最小值。分栏比例没有这种限制，用它自己的范围。
 */
export function panelMax(
  kind: PanelKind,
  sizes: PanelSizes,
  area: { width: number; height: number },
  assetsOpen: boolean,
): number {
  const { min, max } = PANEL_LIMITS[kind];
  let room = max;
  if (kind === "assets") room = area.width - NAV_WIDTH - sizes.sidebar - MIN_VIEWPORT.width;
  if (kind === "sidebar") {
    room = area.width - NAV_WIDTH - (assetsOpen ? sizes.assets : 0) - MIN_VIEWPORT.width;
  }
  if (kind === "timeline") room = area.height - HEADER_HEIGHT - MIN_VIEWPORT.height;
  return Math.max(min, Math.min(max, room));
}
