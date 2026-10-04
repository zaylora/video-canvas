import { describe, expect, test } from "bun:test";

import { isMediaPending } from "@/utils/canvas/media-pending";

describe("isMediaPending：节点内容是否还该显示加载占位", () => {
  test("所有图层都还在请求中，显示占位", () => {
    expect(isMediaPending(["pending"])).toBe(true);
    expect(isMediaPending(["pending", "pending"])).toBe(true);
  });

  test("任意一层已加载就是有内容了，不再占位", () => {
    expect(isMediaPending(["loaded"])).toBe(false);
    expect(isMediaPending(["loaded", "pending"])).toBe(false);
    expect(isMediaPending(["failed", "loaded"])).toBe(false);
  });

  test("缩略图失败、原图还在请求时仍然占位", () => {
    expect(isMediaPending(["failed", "pending"])).toBe(true);
  });

  test("全部失败交给破图占位，不再显示加载占位", () => {
    expect(isMediaPending(["failed"])).toBe(false);
    expect(isMediaPending(["failed", "failed"])).toBe(false);
  });

  test("没有任何图层时不占位", () => {
    expect(isMediaPending([])).toBe(false);
  });
});
