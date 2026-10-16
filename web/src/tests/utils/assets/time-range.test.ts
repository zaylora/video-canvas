import { describe, expect, test } from "bun:test";

import { timeRangeStart } from "@/utils/assets/time-range";

/** 本地时区 2026-10-16 15:30 */
const NOW = new Date(2026, 9, 16, 15, 30, 0);

/** 把结果当成时间戳解析，再取本地时区的年月日时分，断言就不依赖运行机器的时区 */
const local = (iso: string | undefined) => {
  const date = new Date(iso as string);
  return [
    date.getFullYear(),
    date.getMonth() + 1,
    date.getDate(),
    date.getHours(),
    date.getMinutes(),
  ];
};

describe("timeRangeStart：时间筛选换算成 created_from", () => {
  test("全部不传这个参数", () => {
    expect(timeRangeStart("all", NOW)).toBeUndefined();
  });

  test("今天 = 本地时区今天 0 点", () => {
    expect(local(timeRangeStart("today", NOW))).toEqual([2026, 10, 16, 0, 0]);
  });

  test("近 7 天 = 含今天往前 7 个本地日，从 0 点算起", () => {
    expect(local(timeRangeStart("week", NOW))).toEqual([2026, 10, 10, 0, 0]);
  });

  test("近 30 天跨月也按本地日历往前数", () => {
    expect(local(timeRangeStart("month", NOW))).toEqual([2026, 9, 17, 0, 0]);
  });

  test("返回 RFC3339（UTC，带 Z）", () => {
    expect(timeRangeStart("today", NOW)).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z$/);
  });
});
