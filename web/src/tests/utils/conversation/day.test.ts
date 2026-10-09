import { describe, expect, test } from "bun:test";

import { dayTitle } from "@/utils/conversation/day";

const NOW = new Date(2026, 9, 9, 12, 0, 0);

describe("dayTitle：记录的日期分组标题", () => {
  test("今年的只写月日", () => {
    expect(dayTitle(new Date(2026, 9, 9, 8, 30).toISOString(), NOW)).toBe("10月9日");
    expect(dayTitle(new Date(2026, 0, 1, 0, 5).toISOString(), NOW)).toBe("1月1日");
  });

  test("不是今年的带上年份", () => {
    expect(dayTitle(new Date(2025, 11, 31, 23, 59).toISOString(), NOW)).toBe("2025年12月31日");
  });

  test("时间解析不了是空串", () => {
    expect(dayTitle("不是时间", NOW)).toBe("");
  });
});
