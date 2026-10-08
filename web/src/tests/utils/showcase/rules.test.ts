import { describe, expect, test } from "bun:test";

import {
  HEAVY_VIDEO_BYTES,
  clampIndex,
  fallbackHue,
  nextIndex,
  shouldAutoAdvance,
  shouldPlayVideo,
  showcaseWarnings,
  validateClipSeconds,
} from "@/utils/showcase/rules";

describe("nextIndex：轮播下一条", () => {
  test("到头回到第一条", () => {
    expect(nextIndex(0, 3)).toBe(1);
    expect(nextIndex(2, 3)).toBe(0);
  });

  test("没有作品时停在 0，只有一条时还是它", () => {
    expect(nextIndex(0, 0)).toBe(0);
    expect(nextIndex(0, 1)).toBe(0);
  });
});

describe("clampIndex：下标收回合法范围", () => {
  test("作品被删后越界的下标落在最后一条", () => {
    expect(clampIndex(5, 3)).toBe(2);
    expect(clampIndex(-1, 3)).toBe(0);
    expect(clampIndex(1, 3)).toBe(1);
  });

  test("没有作品时为 0", () => {
    expect(clampIndex(4, 0)).toBe(0);
  });
});

describe("shouldPlayVideo：要不要加载播放视频", () => {
  test("减少动态效果时一律只显示封面", () => {
    expect(shouldPlayVideo(true, false, false)).toBe(false);
  });

  test("省流量只在后台勾了「省流量时只显示封面」时才不播", () => {
    expect(shouldPlayVideo(false, true, true)).toBe(false);
    expect(shouldPlayVideo(false, true, false)).toBe(true);
    expect(shouldPlayVideo(false, false, true)).toBe(true);
  });
});

describe("shouldAutoAdvance：要不要自动切下一条", () => {
  test("减少动态效果或只有一条时不自动切", () => {
    expect(shouldAutoAdvance(true, 5)).toBe(false);
    expect(shouldAutoAdvance(false, 1)).toBe(false);
    expect(shouldAutoAdvance(false, 0)).toBe(false);
  });

  test("多条且没有减少动态效果时自动切", () => {
    expect(shouldAutoAdvance(false, 2)).toBe(true);
  });
});

describe("fallbackHue：没有封面时的渐变色相", () => {
  test("同一个 ID 结果稳定，且落在 0–359", () => {
    for (const id of ["1", "12", "a-long-id-0001"]) {
      expect(fallbackHue(id)).toBe(fallbackHue(id));
      expect(fallbackHue(id)).toBeGreaterThanOrEqual(0);
      expect(fallbackHue(id)).toBeLessThan(360);
    }
  });
});

describe("showcaseWarnings：后台体积 / 画幅提示", () => {
  test("正好 15 MB 不提示，超过才提示", () => {
    expect(showcaseWarnings(HEAVY_VIDEO_BYTES, 1280, 720)).toEqual([]);
    expect(showcaseWarnings(HEAVY_VIDEO_BYTES + 1, 1280, 720)).toEqual(["heavy"]);
  });

  test("竖屏提示；尺寸未知（0）不提示", () => {
    expect(showcaseWarnings(1024, 720, 1280)).toEqual(["portrait"]);
    expect(showcaseWarnings(1024, 0, 0)).toEqual([]);
    expect(showcaseWarnings(1024, 1000, 1000)).toEqual([]);
  });

  test("体积和画幅都命中时按「体积 → 画幅」排", () => {
    expect(showcaseWarnings(HEAVY_VIDEO_BYTES + 1, 720, 1280)).toEqual(["heavy", "portrait"]);
  });
});

describe("validateClipSeconds：播放时长校验", () => {
  test("4 到 15 的整数合法", () => {
    expect(validateClipSeconds(4)).toBe("");
    expect(validateClipSeconds(7)).toBe("");
    expect(validateClipSeconds(15)).toBe("");
  });

  test("越界和小数都不合法", () => {
    expect(validateClipSeconds(3)).not.toBe("");
    expect(validateClipSeconds(16)).not.toBe("");
    expect(validateClipSeconds(7.5)).not.toBe("");
  });
});
