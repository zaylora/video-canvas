/** 桌宠和浮窗离区域边缘的最小距离 */
export const PET_MARGIN = 12;
/** 浮窗与桌宠之间的间距 */
export const PANEL_GAP = 8;

export type Size = { width: number; height: number };
/** 区域内坐标，原点在区域左上角 */
export type Rect = { x: number; y: number; width: number; height: number };

export type PetCorner = "br" | "bl" | "tr" | "tl";
/** 相对最近一个角的偏移：桌宠到该角两条边的距离 */
export type PetAnchor = { corner: PetCorner; dx: number; dy: number };

/** 没记录过位置时的默认锚点：右下角 */
export const DEFAULT_ANCHOR: PetAnchor = { corner: "br", dx: PET_MARGIN, dy: PET_MARGIN };

/** 浮窗的 transform-origin，指向桌宠所在的方向 */
export type PanelOrigin = `${"top" | "center" | "bottom"} ${"left" | "right"}`;

/** 把 v 夹进 [min, max]；放不下（max < min）时贴 min */
const clamp = (v: number, min: number, max: number) => Math.max(min, Math.min(v, max));

/** 浮窗尺寸：宽 400，高取 600 和「区域高度减 24」中较小的一个 */
export const panelSize = (area: Size): Size => ({
  width: 400,
  height: Math.min(600, area.height - PET_MARGIN * 2),
});

/**
 * 计算浮窗放在哪里：桌宠上方放得下就放上方，否则放下方，
 * 上下都放不下就放到左右空间较大的一侧；水平方向对齐桌宠所在的那一边，
 * 最后整体夹回区域内（留 PET_MARGIN 边距）。
 */
export function placePanel(
  pet: Rect,
  area: Size,
  want: Size,
): { x: number; y: number; origin: PanelOrigin } {
  const maxX = area.width - PET_MARGIN - want.width;
  const maxY = area.height - PET_MARGIN - want.height;
  const clampX = (x: number) => clamp(x, PET_MARGIN, maxX);
  const clampY = (y: number) => clamp(y, PET_MARGIN, maxY);

  const petOnRight = pet.x + pet.width / 2 >= area.width / 2;
  const alignedX = clampX(petOnRight ? pet.x + pet.width - want.width : pet.x);
  const side = petOnRight ? "right" : "left";

  const above = pet.y - PANEL_GAP - want.height;
  if (above >= PET_MARGIN) return { x: alignedX, y: above, origin: `bottom ${side}` };

  const below = pet.y + pet.height + PANEL_GAP;
  if (below + want.height <= area.height - PET_MARGIN) {
    return { x: alignedX, y: below, origin: `top ${side}` };
  }

  const rightRoom = area.width - (pet.x + pet.width) - PANEL_GAP - PET_MARGIN;
  const leftRoom = pet.x - PANEL_GAP - PET_MARGIN;
  const y = clampY(pet.y + pet.height / 2 - want.height / 2);
  if (rightRoom >= leftRoom) {
    return { x: clampX(pet.x + pet.width + PANEL_GAP), y, origin: "center left" };
  }
  return { x: clampX(pet.x - PANEL_GAP - want.width), y, origin: "center right" };
}

/**
 * 把桌宠位置换算成相对最近一个角的偏移。位置先夹进边距内再记录，
 * 正中间落在右下角。与 rectOf 互逆。
 */
export function anchorOf(rect: Rect, area: Size): PetAnchor {
  const left = rect.x + rect.width / 2 < area.width / 2;
  const top = rect.y + rect.height / 2 < area.height / 2;
  const dx = left ? rect.x : area.width - (rect.x + rect.width);
  const dy = top ? rect.y : area.height - (rect.y + rect.height);
  return {
    corner: `${top ? "t" : "b"}${left ? "l" : "r"}` as PetCorner,
    dx: Math.max(PET_MARGIN, dx),
    dy: Math.max(PET_MARGIN, dy),
  };
}

/** 按锚点还原桌宠位置；区域缩小时夹回区域内，留 PET_MARGIN 边距 */
export function rectOf(anchor: PetAnchor, area: Size, size: Size): Rect {
  const fromLeft = anchor.corner === "tl" || anchor.corner === "bl";
  const fromTop = anchor.corner === "tl" || anchor.corner === "tr";
  const x = fromLeft ? anchor.dx : area.width - anchor.dx - size.width;
  const y = fromTop ? anchor.dy : area.height - anchor.dy - size.height;
  return {
    x: clamp(x, PET_MARGIN, area.width - PET_MARGIN - size.width),
    y: clamp(y, PET_MARGIN, area.height - PET_MARGIN - size.height),
    ...size,
  };
}
