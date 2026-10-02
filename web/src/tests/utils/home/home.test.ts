import { describe, expect, test } from "bun:test";

import { COVER_LAYOUT_COUNT, coverLayoutOf, formatCanvasTime, greeting } from "@/utils/home/home";

describe("greeting：按小时问候", () => {
  test("各时段边界", () => {
    expect(greeting(4)).toBe("晚上好");
    expect(greeting(5)).toBe("早上好");
    expect(greeting(10)).toBe("早上好");
    expect(greeting(11)).toBe("中午好");
    expect(greeting(13)).toBe("下午好");
    expect(greeting(17)).toBe("下午好");
    expect(greeting(18)).toBe("晚上好");
    expect(greeting(0)).toBe("晚上好");
  });
});

describe("coverLayoutOf：线框封面布局", () => {
  test("同一个 id 结果稳定，且落在范围内", () => {
    for (const id of ["1", "a3f9", "6650e1b2c0ffee", ""]) {
      const index = coverLayoutOf(id);
      expect(index).toBe(coverLayoutOf(id));
      expect(index).toBeGreaterThanOrEqual(0);
      expect(index).toBeLessThan(COVER_LAYOUT_COUNT);
    }
  });

  test("不同 id 会分散到不同布局", () => {
    const seen = new Set(Array.from({ length: 30 }, (_, i) => coverLayoutOf(`canvas-${i}`)));
    expect(seen.size).toBe(COVER_LAYOUT_COUNT);
  });
});

describe("formatCanvasTime：卡片时间", () => {
  const now = new Date(2026, 9, 3, 20, 0);

  test("当天显示「今天 HH:mm」", () => {
    expect(formatCanvasTime(new Date(2026, 9, 3, 4, 52).toISOString(), now)).toBe("今天 04:52");
  });

  test("其他日子显示完整日期", () => {
    expect(formatCanvasTime(new Date(2026, 8, 10, 10, 24).toISOString(), now)).toBe(
      "2026-09-10 10:24",
    );
  });

  test("解析失败原样返回", () => {
    expect(formatCanvasTime("坏数据", now)).toBe("坏数据");
  });
});
