import { describe, expect, test } from "bun:test";

import { contextUsage, formatTokens } from "@/utils/agent/context-usage";

describe("formatTokens", () => {
  test("不到 1000 原样显示", () => {
    expect(formatTokens(0)).toBe("0");
    expect(formatTokens(999)).toBe("999");
  });

  test("千位用 K：1 万以内留一位小数，去掉多余的 .0", () => {
    expect(formatTokens(1000)).toBe("1K");
    expect(formatTokens(1500)).toBe("1.5K");
    expect(formatTokens(9990)).toBe("10K");
    expect(formatTokens(12_345)).toBe("12K");
    expect(formatTokens(200_000)).toBe("200K");
  });

  test("百万用 M，留一位小数", () => {
    expect(formatTokens(1_000_000)).toBe("1M");
    expect(formatTokens(1_250_000)).toBe("1.3M");
  });

  test("负数、非数字当 0", () => {
    expect(formatTokens(-5)).toBe("0");
    expect(formatTokens(Number.NaN)).toBe("0");
  });
});

describe("contextUsage", () => {
  test("已知窗口：给占比和整数百分比", () => {
    const u = contextUsage(50_000, 200_000);
    expect(u).toMatchObject({ known: true, ratio: 0.25, percent: 25, tone: "normal" });
  });

  test("接近上限变警示，到顶变危险；超出按 100% 封顶", () => {
    expect(contextUsage(150_000, 200_000).tone).toBe("warn"); // 75%
    expect(contextUsage(180_000, 200_000).tone).toBe("danger"); // 90%
    const over = contextUsage(250_000, 200_000);
    expect(over).toMatchObject({ ratio: 1, percent: 100, tone: "danger" });
  });

  test("没配窗口：只能显示已用，占比为 0、不报警", () => {
    expect(contextUsage(5000, 0)).toMatchObject({
      known: false,
      ratio: 0,
      percent: 0,
      tone: "normal",
    });
  });

  test("还没发过消息：已用 0", () => {
    expect(contextUsage(0, 200_000)).toMatchObject({
      known: true,
      ratio: 0,
      percent: 0,
      tone: "normal",
    });
  });

  test("占比不足 1% 但有用量时显示 1%，不显示成 0%", () => {
    expect(contextUsage(300, 200_000).percent).toBe(1);
  });
});
