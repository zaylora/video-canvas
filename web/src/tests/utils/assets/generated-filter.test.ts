import { describe, expect, test } from "bun:test";

import {
  DEFAULT_GENERATED_FILTER,
  buildGeneratedQuery,
  hasActiveFilter,
  sourceFromValue,
  sourceToValue,
} from "@/utils/assets/generated-filter";

const NOW = new Date(2026, 9, 16, 15, 30, 0);

describe("buildGeneratedQuery：筛选状态换成接口参数", () => {
  test("默认筛选只带类型、来源 all 和排序，不带画布、时间、关键字", () => {
    const query = buildGeneratedQuery("image", DEFAULT_GENERATED_FILTER, NOW);
    expect(query).toEqual({
      kind: "image",
      source: "all",
      canvasId: undefined,
      createdFrom: undefined,
      order: "desc",
      q: undefined,
    });
  });

  test("选了某张画布：source=canvas 并带 canvas_id", () => {
    const query = buildGeneratedQuery(
      "video",
      { ...DEFAULT_GENERATED_FILTER, source: { type: "canvas", canvasId: "1f" } },
      NOW,
    );
    expect(query.source).toBe("canvas");
    expect(query.canvasId).toBe("1f");
  });

  test("首页对话来源不带 canvas_id", () => {
    const query = buildGeneratedQuery(
      "image",
      { ...DEFAULT_GENERATED_FILTER, source: { type: "conversation" } },
      NOW,
    );
    expect(query.source).toBe("conversation");
    expect(query.canvasId).toBeUndefined();
  });

  test("时间范围换成 created_from，最早优先换成 asc，关键字去首尾空白", () => {
    const query = buildGeneratedQuery(
      "audio",
      { ...DEFAULT_GENERATED_FILTER, range: "today", order: "asc", q: "  雪地 " },
      NOW,
    );
    expect(new Date(query.createdFrom as string).getDate()).toBe(16);
    expect(query.order).toBe("asc");
    expect(query.q).toBe("雪地");
  });

  test("纯空白关键字当作没有", () => {
    expect(buildGeneratedQuery("image", { ...DEFAULT_GENERATED_FILTER, q: "   " }, NOW).q).toBe(
      undefined,
    );
  });
});

describe("hasActiveFilter：是否有生效的筛选", () => {
  test("默认没有；排序不算筛选", () => {
    expect(hasActiveFilter(DEFAULT_GENERATED_FILTER)).toBe(false);
    expect(hasActiveFilter({ ...DEFAULT_GENERATED_FILTER, order: "asc" })).toBe(false);
  });

  test("来源、时间、关键字任一有值就算", () => {
    expect(hasActiveFilter({ ...DEFAULT_GENERATED_FILTER, source: { type: "conversation" } })).toBe(
      true,
    );
    expect(hasActiveFilter({ ...DEFAULT_GENERATED_FILTER, range: "week" })).toBe(true);
    expect(hasActiveFilter({ ...DEFAULT_GENERATED_FILTER, q: "猫" })).toBe(true);
    expect(hasActiveFilter({ ...DEFAULT_GENERATED_FILTER, q: "  " })).toBe(false);
  });
});

describe("来源与单选值互转", () => {
  test("往返一致", () => {
    for (const value of ["all", "conversation", "canvas:1f"]) {
      expect(sourceToValue(sourceFromValue(value))).toBe(value);
    }
  });

  test("不认识的值退回全部", () => {
    expect(sourceFromValue("whatever")).toEqual({ type: "all" });
    expect(sourceFromValue("canvas:")).toEqual({ type: "all" });
  });
});
