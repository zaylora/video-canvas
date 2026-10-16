import { describe, expect, test } from "bun:test";

import { formatDayTitle, placeholderBackground } from "@/utils/home/placeholder";

describe("formatDayTitle：日期标题", () => {
  test("月份不补零", () => {
    expect(formatDayTitle(new Date(2026, 9, 8))).toBe("10月8日");
    expect(formatDayTitle(new Date(2026, 0, 3))).toBe("1月3日");
  });
});

describe("placeholderBackground：占位封面", () => {
  test("色相超出范围按圆周取模，负数也落在 0–360", () => {
    expect(placeholderBackground(370)).toBe(placeholderBackground(10));
    expect(placeholderBackground(-10)).toBe(placeholderBackground(350));
  });
});
