import { describe, expect, test } from "bun:test";

import type { ModelLimits } from "@/api/admin/ai/type.d";
import {
  limitsDiffer,
  shouldAutoFill,
  sourceLabel,
  suggestedMediaCapabilities,
} from "@/utils/admin/model-limits";

const found = (window: number, output: number, source: ModelLimits["source"] = "builtin") =>
  ({ found: true, context_window: window, max_output: output, source }) satisfies ModelLimits;

test("手动应用输入能力时只填已知项，OpenAI 不能应用看视频", () => {
  const limits = { ...found(128000, 16384), vision: true, video: true };
  expect(suggestedMediaCapabilities(limits, false)).toEqual({ vision: true });
  expect(suggestedMediaCapabilities(limits, true)).toEqual({ vision: true, video: true });
  expect(suggestedMediaCapabilities(found(128000, 16384), true)).toEqual({});
  expect(
    limitsDiffer(limits, { window: 128000, output: 16384 }, { vision: true, video: false }, true),
  ).toBe(true);
  expect(
    limitsDiffer(limits, { window: 128000, output: 16384 }, { vision: true, video: false }, false),
  ).toBe(false);
});

describe("shouldAutoFill：只有运营还没填过时才自动填", () => {
  test("没有 context、或窗口和输出都是 0：可以自动填", () => {
    expect(shouldAutoFill(undefined)).toBe(true);
    expect(shouldAutoFill({ window: 0, output: 0 })).toBe(true);
  });

  test("填过任意一项就不自动填，免得覆盖运营的手填值", () => {
    expect(shouldAutoFill({ window: 128000, output: 0 })).toBe(false);
    expect(shouldAutoFill({ window: 0, output: 4096 })).toBe(false);
    expect(shouldAutoFill({ window: 128000, output: 16384 })).toBe(false);
  });
});

describe("limitsDiffer：识别值和当前值是否不同", () => {
  test("没认出来：永远不算不同（没有可应用的值）", () => {
    expect(
      limitsDiffer({ found: false, context_window: 0, max_output: 0 }, { window: 1, output: 1 }),
    ).toBe(false);
  });

  test("当前值和识别值一样：不同为 false", () => {
    expect(limitsDiffer(found(128000, 16384), { window: 128000, output: 16384 })).toBe(false);
  });

  test("窗口或输出有一项不同、或当前没填：为 true", () => {
    expect(limitsDiffer(found(128000, 16384), { window: 64000, output: 16384 })).toBe(true);
    expect(limitsDiffer(found(128000, 16384), { window: 128000, output: 4096 })).toBe(true);
    expect(limitsDiffer(found(128000, 16384), undefined)).toBe(true);
  });
});

describe("sourceLabel", () => {
  test("两种来源有各自的中文名，未知来源不显示", () => {
    expect(sourceLabel("upstream")).toBe("渠道上游返回");
    expect(sourceLabel("builtin")).toBe("内置模型库");
    expect(sourceLabel(undefined)).toBe("");
  });
});
