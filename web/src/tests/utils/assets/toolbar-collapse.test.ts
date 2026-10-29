import { describe, expect, test } from "bun:test";

import { COLLAPSE_AT, collapseProgress } from "@/utils/assets/toolbar-collapse";

/** 工具条在文档里的顶边 400px、高 48px，顶栏高 56px */
const at = (scrollY: number) => collapseProgress({ scrollY, top: 400, height: 48, offset: 56 });

describe("工具条随滚动收起的进度", () => {
  test("工具条离顶栏还远时不收", () => {
    expect(at(0)).toBe(0);
    expect(at(300)).toBe(0);
  });

  test("从碰到顶栏开始，按滚动距离线性增长", () => {
    const start = at(344);
    const mid = at(370);
    expect(start).toBe(0);
    expect(mid).toBeGreaterThan(0);
    expect(mid).toBeLessThan(1);
    expect(at(380)).toBeGreaterThan(mid);
  });

  test("完全滑过顶栏后封顶为 1，再往下滚也不超过", () => {
    expect(at(392)).toBe(1);
    expect(at(2000)).toBe(1);
  });

  test("进度到阈值才换成顶部胶囊", () => {
    expect(COLLAPSE_AT).toBeGreaterThan(0.5);
    expect(COLLAPSE_AT).toBeLessThanOrEqual(1);
    expect(at(392) >= COLLAPSE_AT).toBe(true);
    expect(at(350) >= COLLAPSE_AT).toBe(false);
  });

  test("高度为 0（还没布局）时不收，也不出 NaN", () => {
    expect(collapseProgress({ scrollY: 999, top: 0, height: 0, offset: 56 })).toBe(0);
  });
});
