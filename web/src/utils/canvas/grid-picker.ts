import { GRID_MAX, isValidGrid } from "./grid-split";

/**
 * 宫格切分「自定义」的 5×5 格子选择器里能单独讲的规则：有哪些格子、悬停时哪些高亮、哪格不能选。
 * 选择器是一个 5×5 的小格子阵，鼠标划过哪格，从左上到该格的矩形高亮；点击就是选了「行×列」。
 */

/** 选择器的标题 */
export const GRID_PICKER_TITLE = "自定义宫格";

/** 不可选格子的提示 */
export const PICKER_DISABLED_TITLE = "至少 2 格，1×1 不可选";

/** 选择器里的一格：选中它就是切成 rows 行 cols 列 */
export type PickerCell = {
  /** 行数，也就是这一格在选择器里的行号（从 1 起） */
  rows: number;
  /** 列数，也就是这一格在选择器里的列号（从 1 起） */
  cols: number;
};

/** 选择器的全部格子，按行优先 */
export function pickerCells(): PickerCell[] {
  return Array.from({ length: GRID_MAX * GRID_MAX }, (_, index) => ({
    rows: Math.floor(index / GRID_MAX) + 1,
    cols: (index % GRID_MAX) + 1,
  }));
}

/**
 * 这一格要不要高亮：在悬停格左上方的矩形里（含悬停格本身）就亮。
 * 不可选的 (1,1) 在矩形里时也亮，高亮只表示「选区会覆盖到这里」。
 * @param hover 鼠标正划过的格子；没有悬停为 null
 * @param cell 要判断的格子
 */
export function isCellHighlighted(hover: PickerCell | null, cell: PickerCell): boolean {
  return !!hover && cell.rows <= hover.rows && cell.cols <= hover.cols;
}

/** 这一格能不能选：和宫格切分的行列合法性一致（至少 2 格） */
export function isCellPickable(cell: PickerCell): boolean {
  return isValidGrid(cell.rows, cell.cols);
}

/** 选择器标题旁显示的「行×列」；没有悬停为空串 */
export function pickerLabel(hover: PickerCell | null): string {
  return hover ? `${hover.rows}×${hover.cols}` : "";
}
