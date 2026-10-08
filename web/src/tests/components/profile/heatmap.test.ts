import { describe, expect, test } from "bun:test";

import type { ActivityDay } from "@/api/me/type";
import {
  addDays,
  buildHeatmap,
  cellLabel,
  heatLevels,
  moveCell,
  recentYearStart,
  streaks,
  tooltipLines,
} from "@/components/profile/heatmap";

/** 造一天的数据，默认全是图片 */
const day = (date: string, count: number, part: Partial<ActivityDay> = {}): ActivityDay => ({
  date,
  count,
  image: count,
  video: 0,
  audio: 0,
  text: 0,
  ...part,
});

describe("日期工具", () => {
  test("addDays 跨月跨年", () => {
    expect(addDays("2025-12-31", 1)).toBe("2026-01-01");
    expect(addDays("2024-03-01", -1)).toBe("2024-02-29");
  });
  test("最近一年从 52 周前的周日开始", () => {
    /** 2026-10-09 是周五，所在周的周日是 10-04，往前 52 周是 2025-10-05 */
    expect(recentYearStart("2026-10-09")).toBe("2025-10-05");
    /** 当天就是周日 */
    expect(recentYearStart("2026-10-04")).toBe("2025-10-05");
  });
});

describe("补齐网格", () => {
  test("最近一年是 7 行 × 53 列，按周日对齐，未来日期不渲染", () => {
    const model = buildHeatmap({ start: "2025-10-05", end: "2026-10-09", days: [] });
    expect(model.columns.length).toBe(53);
    expect(model.columns.every((column) => column.length === 7)).toBe(true);
    /** 第一列第一格是周日 2025-10-05 */
    expect(model.columns[0][0]?.date).toBe("2025-10-05");
    /** 最后一列：周日到周五有格子，周六（10-10）是未来，不渲染 */
    const last = model.columns[52];
    expect(last[0]?.date).toBe("2026-10-04");
    expect(last[5]?.date).toBe("2026-10-09");
    expect(last[6]).toBeNull();
  });

  test("自然年：1 月 1 日之前的格子是占位，不渲染", () => {
    /** 2026-01-01 是周四，第一列的周日到周三为空 */
    const model = buildHeatmap({ start: "2026-01-01", end: "2026-12-31", days: [] });
    expect(model.columns[0].slice(0, 4)).toEqual([null, null, null, null]);
    expect(model.columns[0][4]?.date).toBe("2026-01-01");
    expect(model.columns.at(-1)?.some((cell) => cell?.date === "2026-12-31")).toBe(true);
  });

  test("自然年首尾都是周六：2022 年 53 列，闰年 2028 年 54 列", () => {
    const model = buildHeatmap({ start: "2022-01-01", end: "2022-12-31", days: [] });
    expect(model.columns.length).toBe(53);
    expect(model.columns[0][6]?.date).toBe("2022-01-01");
    expect(model.columns[52][6]?.date).toBe("2022-12-31");
    const leap = buildHeatmap({ start: "2028-01-01", end: "2028-12-31", days: [] });
    expect(leap.columns.length).toBe(54);
    expect(leap.columns[53][0]?.date).toBe("2028-12-31");
  });

  test("只返回有生成的日子，其余补 0；区间外的数据忽略", () => {
    const model = buildHeatmap({
      start: "2025-10-05",
      end: "2026-10-09",
      days: [day("2026-10-08", 12), day("2024-01-01", 99)],
    });
    expect(model.total).toBe(12);
    expect(model.activeDays).toBe(1);
    const cell = model.columns[52][4];
    expect(cell?.date).toBe("2026-10-08");
    expect(cell?.count).toBe(12);
    expect(model.columns[52][3]?.count).toBe(0);
  });

  test("月份标签在每月第一周所在列，首列总有标签", () => {
    const model = buildHeatmap({ start: "2025-10-05", end: "2026-10-09", days: [] });
    expect(model.months[0]).toEqual({ col: 0, label: "10月" });
    expect(model.months.map((m) => m.label)).toContain("1月");
    expect(model.months.find((m) => m.label === "11月")?.col).toBe(4);
  });
});

describe("四分位分档", () => {
  test("0 次是 0 档；≤Q1、≤Q2、≤Q3、>Q3 依次 1–4 档", () => {
    const levels = heatLevels([1, 2, 3, 4, 5, 6, 7, 8]);
    /** 排序后 Q1=sorted[1]=2，Q2=sorted[3]=4，Q3=sorted[5]=6 */
    expect(levels(0)).toBe(0);
    expect(levels(1)).toBe(1);
    expect(levels(2)).toBe(1);
    expect(levels(3)).toBe(2);
    expect(levels(4)).toBe(2);
    expect(levels(6)).toBe(3);
    expect(levels(7)).toBe(4);
    expect(levels(8)).toBe(4);
  });

  test("非零天少于 4 天：1 次是 1 档，其余按最大值线性分到 2–4 档", () => {
    const levels = heatLevels([1, 5, 9]);
    expect(levels(1)).toBe(1);
    expect(levels(5)).toBe(3);
    expect(levels(9)).toBe(4);
    expect(heatLevels([10])(10)).toBe(4);
    expect(heatLevels([1, 1])(1)).toBe(1);
    expect(heatLevels([2])(2)).toBe(4);
  });

  test("网格里每格带档位", () => {
    const model = buildHeatmap({
      start: "2026-10-01",
      end: "2026-10-09",
      days: [day("2026-10-08", 1), day("2026-10-09", 9)],
    });
    const cells = model.columns.flat().filter((cell) => cell && cell.count > 0);
    expect(cells.map((cell) => cell?.level)).toEqual([1, 4]);
  });
});

describe("连续天数", () => {
  const range = { start: "2026-09-01", end: "2026-10-09" };
  test("今天有生成：当前连续从今天往前数", () => {
    const counts = new Map([
      ["2026-10-07", 1],
      ["2026-10-08", 2],
      ["2026-10-09", 3],
    ]);
    expect(streaks({ ...range, counts })).toEqual({ longest: 3, current: 3 });
  });
  test("今天为 0 时从昨天算起", () => {
    const counts = new Map([
      ["2026-10-07", 1],
      ["2026-10-08", 2],
    ]);
    expect(streaks({ ...range, counts }).current).toBe(2);
  });
  test("昨天也为 0：当前连续为 0，最长连续照算", () => {
    const counts = new Map([
      ["2026-09-01", 1],
      ["2026-09-02", 1],
      ["2026-09-03", 1],
      ["2026-10-07", 1],
    ]);
    expect(streaks({ ...range, counts })).toEqual({ longest: 3, current: 0 });
  });
  test("跨年也连续", () => {
    const counts = new Map([
      ["2025-12-31", 1],
      ["2026-01-01", 1],
    ]);
    expect(streaks({ start: "2025-12-01", end: "2026-01-01", counts })).toEqual({
      longest: 2,
      current: 2,
    });
  });
});

describe("tooltip 与 aria-label 文案", () => {
  test("有生成：日期 + 星期 + 次数，第二行按类型拆分，只列非零类型", () => {
    expect(
      tooltipLines("2026-10-08", day("2026-10-08", 12, { image: 8, video: 3, audio: 1 })),
    ).toEqual(["2026年10月8日 星期四 · 12 次生成", "图片 8 · 视频 3 · 音频 1"]);
  });
  test("0 次：只有日期和「无生成」", () => {
    expect(tooltipLines("2026-10-08", undefined)).toEqual(["2026年10月8日 · 无生成", ""]);
  });
  test("aria-label 与 tooltip 一致，两行用逗号连起来", () => {
    expect(cellLabel("2026-10-08", day("2026-10-08", 2, { image: 1, text: 1 }))).toBe(
      "2026年10月8日 星期四 · 2 次生成，图片 1 · 文本 1",
    );
    expect(cellLabel("2026-10-08", undefined)).toBe("2026年10月8日 · 无生成");
  });
});

describe("方向键移动", () => {
  /** 2026-01-01（周四）到 2026-01-14（周三）：第一列只有周四到周六 */
  const model = buildHeatmap({ start: "2026-01-01", end: "2026-01-14", days: [] });

  test("上下左右逐格移动，越界或占位不动", () => {
    expect(moveCell(model, { row: 4, col: 1 }, "ArrowLeft")).toEqual({ row: 4, col: 0 });
    expect(moveCell(model, { row: 4, col: 0 }, "ArrowRight")).toEqual({ row: 4, col: 1 });
    expect(moveCell(model, { row: 4, col: 1 }, "ArrowUp")).toEqual({ row: 3, col: 1 });
    expect(moveCell(model, { row: 4, col: 1 }, "ArrowDown")).toEqual({ row: 5, col: 1 });
    /** 第一列周三是占位 */
    expect(moveCell(model, { row: 4, col: 0 }, "ArrowUp")).toBeNull();
    expect(moveCell(model, { row: 4, col: 0 }, "ArrowLeft")).toBeNull();
    expect(moveCell(model, { row: 6, col: 1 }, "ArrowDown")).toBeNull();
  });

  test("Home / End 到本行首尾，PageUp / PageDown 到本列首尾", () => {
    expect(moveCell(model, { row: 1, col: 1 }, "Home")).toEqual({ row: 1, col: 1 });
    expect(moveCell(model, { row: 5, col: 1 }, "Home")).toEqual({ row: 5, col: 0 });
    expect(moveCell(model, { row: 1, col: 1 }, "End")).toEqual({ row: 1, col: 2 });
    expect(moveCell(model, { row: 6, col: 0 }, "PageUp")).toEqual({ row: 4, col: 0 });
    expect(moveCell(model, { row: 0, col: 2 }, "PageDown")).toEqual({ row: 3, col: 2 });
  });

  test("其他按键不处理", () => {
    expect(moveCell(model, { row: 4, col: 1 }, "Enter")).toBeNull();
  });

  test("默认可聚焦的格子是最后一个（最近一天）", () => {
    expect(model.lastCell).toEqual({ row: 3, col: 2 });
  });
});
