import { describe, expect, test } from "bun:test";

import { buildSampleConversations } from "@/constants/conversation-sample";
import { filterHistoryAssets, flattenHistoryAssets } from "@/utils/home/assets";
import { formatDayTitle, placeholderBackground } from "@/utils/home/placeholder";

const NOW = new Date(2026, 9, 8, 12, 0, 0);

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

describe("buildSampleConversations：对话样例", () => {
  test("对话 ID 不重复", () => {
    const ids = buildSampleConversations(NOW).map((item) => item.id);
    expect(new Set(ids).size).toBe(ids.length);
  });

  test("记录 ID 不重复，今天的记录用今天的日期标题", () => {
    const records = buildSampleConversations(NOW).flatMap((item) => item.records);
    expect(new Set(records.map((record) => record.id)).size).toBe(records.length);
    expect(records.some((record) => record.day === "10月8日")).toBe(true);
  });
});

describe("flattenHistoryAssets：生成历史", () => {
  const assets = flattenHistoryAssets(buildSampleConversations(NOW));

  test("生成中的记录不进资产", () => {
    const pending = buildSampleConversations(NOW)
      .flatMap((item) => item.records)
      .filter((record) => record.progress !== null);
    expect(pending.length).toBeGreaterThan(0);
    for (const record of pending) {
      expect(assets.some((asset) => asset.id.startsWith(`${record.id}-`))).toBe(false);
    }
  });

  test("最新的排在前面", () => {
    const days = assets.map((asset) => asset.day);
    expect(days[0]).toBe("10月8日");
    expect(days[days.length - 1]).toBe("10月6日");
  });

  test("按类型筛：图片、视频各自只留自己的，文档永远为空", () => {
    expect(filterHistoryAssets(assets, "image", "").every((item) => item.type === "image")).toBe(
      true,
    );
    expect(filterHistoryAssets(assets, "video", "").every((item) => item.type === "video")).toBe(
      true,
    );
    expect(filterHistoryAssets(assets, "doc", "")).toEqual([]);
  });

  test("关键字只匹配提示词，空白不筛", () => {
    expect(filterHistoryAssets(assets, "video", "  ").length).toBe(
      filterHistoryAssets(assets, "video", "").length,
    );
    expect(filterHistoryAssets(assets, "video", "门框").length).toBe(1);
    expect(filterHistoryAssets(assets, "video", "不存在的词")).toEqual([]);
  });
});
