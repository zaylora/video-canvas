/** 资产列表框选：按下后移动超过这么多像素才算开始框选，否则当作点了一下空白 */
export const MARQUEE_THRESHOLD = 4;

export type Point = { x: number; y: number };
export type Box = { left: number; top: number; width: number; height: number };

/**
 * 按下点和当前点围成的框（两点可以是任意方向）。
 * @returns 移动还没超过阈值时为 null
 */
export function marqueeBox(start: Point, current: Point): Box | null {
  const width = Math.abs(current.x - start.x);
  const height = Math.abs(current.y - start.y);
  if (Math.max(width, height) < MARQUEE_THRESHOLD) return null;
  return { left: Math.min(start.x, current.x), top: Math.min(start.y, current.y), width, height };
}

/**
 * 框碰到的卡片：有交叠就算，碰到一角也选中。
 * @param box 选框（和卡片用同一套坐标）
 * @param cards 可选的卡片及其位置
 * @returns 碰到的卡片 id，按传入顺序
 */
export function marqueeHits(box: Box, cards: readonly { id: string; rect: Box }[]): string[] {
  return cards
    .filter(
      ({ rect }) =>
        rect.left < box.left + box.width &&
        box.left < rect.left + rect.width &&
        rect.top < box.top + box.height &&
        box.top < rect.top + rect.height,
    )
    .map((card) => card.id);
}

/**
 * 框选后的选中集合。
 * @param base 开始框选前已经选中的
 * @param hits 这次框到的
 * @param additive 按住 Shift / Ctrl：在原来的选择上追加；否则只留框到的
 */
export function marqueeSelection(
  base: ReadonlySet<string>,
  hits: readonly string[],
  additive: boolean,
): ReadonlySet<string> {
  return new Set(additive ? [...base, ...hits] : hits);
}
