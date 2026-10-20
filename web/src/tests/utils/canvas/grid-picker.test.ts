import { describe, expect, test } from "bun:test";

import {
  PICKER_DISABLED_TITLE,
  isCellHighlighted,
  isCellPickable,
  pickerCells,
  pickerLabel,
} from "@/utils/canvas/grid-picker";

describe("pickerCells 格子", () => {
  test("5×5 共 25 格，按行优先，行列从 1 起", () => {
    const cells = pickerCells();
    expect(cells).toHaveLength(25);
    expect(cells[0]).toEqual({ rows: 1, cols: 1 });
    expect(cells[4]).toEqual({ rows: 1, cols: 5 });
    expect(cells[5]).toEqual({ rows: 2, cols: 1 });
    expect(cells[24]).toEqual({ rows: 5, cols: 5 });
  });
});

describe("isCellHighlighted 高亮范围", () => {
  test("没有悬停时谁都不亮", () => {
    expect(pickerCells().some((cell) => isCellHighlighted(null, cell))).toBe(false);
  });

  test("悬停在 (r, c) 时，左上到该格的矩形高亮", () => {
    const hover = { rows: 2, cols: 3 };
    const lit = pickerCells()
      .filter((cell) => isCellHighlighted(hover, cell))
      .map((cell) => `${cell.rows}-${cell.cols}`);
    expect(lit).toEqual(["1-1", "1-2", "1-3", "2-1", "2-2", "2-3"]);
  });

  test("不可选的 (1,1) 在选区内时也高亮", () => {
    expect(isCellHighlighted({ rows: 3, cols: 3 }, { rows: 1, cols: 1 })).toBe(true);
    expect(isCellPickable({ rows: 1, cols: 1 })).toBe(false);
  });

  test("悬停在最右下角，整个 5×5 都亮", () => {
    const hover = { rows: 5, cols: 5 };
    expect(pickerCells().every((cell) => isCellHighlighted(hover, cell))).toBe(true);
  });
});

describe("isCellPickable 能否选", () => {
  test("只有 (1,1) 不可选，1×N 和 N×1 可选", () => {
    expect(pickerCells().filter((cell) => !isCellPickable(cell))).toEqual([{ rows: 1, cols: 1 }]);
    expect(isCellPickable({ rows: 1, cols: 5 })).toBe(true);
    expect(isCellPickable({ rows: 5, cols: 1 })).toBe(true);
  });

  test("不可选的提示说明至少 2 格", () => {
    expect(PICKER_DISABLED_TITLE).toContain("至少 2 格");
  });
});

describe("pickerLabel 行×列文字", () => {
  test("悬停时显示 行×列，没有悬停为空", () => {
    expect(pickerLabel({ rows: 3, cols: 4 })).toBe("3×4");
    expect(pickerLabel(null)).toBe("");
  });
});
