import { describe, expect, test } from "bun:test";

import {
  DEFAULT_PANELS,
  FLOATING_FRAME,
  HEADER_HEIGHT,
  MIN_VIEWPORT,
  NAV_WIDTH,
  PANEL_LIMITS,
  clampPanel,
  fitFrame,
  fitPanels,
  floatingFrame,
  panelMax,
} from "@/utils/director/layout";

describe("取景框尺寸：在给定的空间里放下最大的、保持画幅的矩形", () => {
  test("空间比画幅更扁：高度顶满，两边留白", () => {
    expect(fitFrame(1000, 300, 16 / 9)).toEqual({ width: 533, height: 300 });
  });

  test("空间比画幅更窄：宽度顶满，上下留白", () => {
    expect(fitFrame(400, 600, 16 / 9)).toEqual({ width: 400, height: 225 });
  });

  test("竖画幅放进横向空间：高度顶满", () => {
    expect(fitFrame(800, 400, 9 / 16)).toEqual({ width: 225, height: 400 });
  });

  test("方画幅", () => {
    expect(fitFrame(500, 300, 1)).toEqual({ width: 300, height: 300 });
  });

  test("还没量出空间（0 或负数）时给 0，不画", () => {
    expect(fitFrame(0, 300, 16 / 9)).toEqual({ width: 0, height: 0 });
    expect(fitFrame(300, 0, 16 / 9)).toEqual({ width: 0, height: 0 });
    expect(fitFrame(-5, -5, 16 / 9)).toEqual({ width: 0, height: 0 });
  });

  test("结果都是整数，且不超出空间", () => {
    for (const ratio of [16 / 9, 9 / 16, 1, 4 / 3, 21 / 9]) {
      const frame = fitFrame(777, 433, ratio);
      expect(Number.isInteger(frame.width)).toBe(true);
      expect(Number.isInteger(frame.height)).toBe(true);
      expect(frame.width).toBeLessThanOrEqual(777);
      expect(frame.height).toBeLessThanOrEqual(433);
    }
  });
});

describe("浮窗的取景框", () => {
  test("横画幅按宽度定", () => {
    expect(floatingFrame(16 / 9)).toEqual({ width: FLOATING_FRAME.width, height: 167 });
  });

  test("竖画幅太高时按最大高度收，免得浮窗占满视口", () => {
    const frame = floatingFrame(9 / 16);
    expect(frame.height).toBe(FLOATING_FRAME.maxHeight);
    expect(frame.width).toBe(Math.round(FLOATING_FRAME.maxHeight * (9 / 16)));
  });
});

describe("面板尺寸", () => {
  const AREA = { width: 1400, height: 800 };

  test("默认值都在各自的范围内", () => {
    for (const kind of ["assets", "sidebar", "timeline", "split"] as const) {
      const { min, max, default: value } = PANEL_LIMITS[kind];
      expect(value).toBeGreaterThanOrEqual(min);
      expect(value).toBeLessThanOrEqual(max);
      expect(DEFAULT_PANELS[kind]).toBe(value);
    }
  });

  test("夹在各自的范围里；不是数字就回默认值", () => {
    expect(clampPanel("assets", 10)).toBe(PANEL_LIMITS.assets.min);
    expect(clampPanel("assets", 9999)).toBe(PANEL_LIMITS.assets.max);
    expect(clampPanel("assets", 300)).toBe(300);
    expect(clampPanel("split", 0.9)).toBe(PANEL_LIMITS.split.max);
    expect(clampPanel("sidebar", Number.NaN)).toBe(PANEL_LIMITS.sidebar.default);
  });

  test("窗口够大时尺寸原样不动", () => {
    expect(fitPanels({ assets: 300, sidebar: 320, timeline: 240, split: 0.4 }, AREA, true)).toEqual(
      { assets: 300, sidebar: 320, timeline: 240, split: 0.4 },
    );
  });

  test("窗口变窄：先收资产面板，再收属性栏，保证视口至少 MIN_VIEWPORT.width 宽", () => {
    const area = { width: 900, height: 800 };
    const fitted = fitPanels({ assets: 480, sidebar: 480, timeline: 200, split: 0.5 }, area, true);
    expect(NAV_WIDTH + fitted.assets + fitted.sidebar + MIN_VIEWPORT.width).toBeLessThanOrEqual(
      900,
    );
    expect(fitted.assets).toBeGreaterThanOrEqual(PANEL_LIMITS.assets.min);
    expect(fitted.sidebar).toBeGreaterThanOrEqual(PANEL_LIMITS.sidebar.min);
    // 先收资产面板
    expect(fitted.assets).toBe(PANEL_LIMITS.assets.min);
  });

  test("资产面板折叠时它不占宽度，不用为它让路", () => {
    const area = { width: 900, height: 800 };
    const fitted = fitPanels({ assets: 480, sidebar: 300, timeline: 200, split: 0.5 }, area, false);
    expect(fitted.sidebar).toBe(300);
  });

  test("窗口变矮：时间轴收一收，保证视口至少 MIN_VIEWPORT.height 高", () => {
    const area = { width: 1400, height: 500 };
    const fitted = fitPanels({ assets: 272, sidebar: 280, timeline: 400, split: 0.5 }, area, true);
    expect(HEADER_HEIGHT + fitted.timeline + MIN_VIEWPORT.height).toBeLessThanOrEqual(500);
  });

  test("再小的窗口也不会收到比最小值还小", () => {
    const fitted = fitPanels(DEFAULT_PANELS, { width: 300, height: 200 }, true);
    expect(fitted.assets).toBe(PANEL_LIMITS.assets.min);
    expect(fitted.sidebar).toBe(PANEL_LIMITS.sidebar.min);
    expect(fitted.timeline).toBe(PANEL_LIMITS.timeline.min);
  });

  test("拖动的上限：给视口留够最小宽度，不超过自己的最大值", () => {
    const sizes = { ...DEFAULT_PANELS, sidebar: 300 };
    expect(panelMax("assets", sizes, AREA, true)).toBe(PANEL_LIMITS.assets.max);
    const narrow = { width: 1000, height: 800 };
    expect(panelMax("assets", sizes, narrow, true)).toBe(
      1000 - NAV_WIDTH - 300 - MIN_VIEWPORT.width,
    );
    expect(panelMax("timeline", sizes, { width: 1400, height: 520 }, true)).toBe(
      520 - HEADER_HEIGHT - MIN_VIEWPORT.height,
    );
  });

  test("上限不会小于下限", () => {
    expect(panelMax("assets", DEFAULT_PANELS, { width: 100, height: 100 }, true)).toBe(
      PANEL_LIMITS.assets.min,
    );
  });
});
