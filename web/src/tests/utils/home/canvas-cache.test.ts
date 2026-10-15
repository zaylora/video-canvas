import { beforeEach, describe, expect, test } from "bun:test";

import type { CanvasListItemDto } from "@/api/canvas/type";
import {
  clearCanvasCache,
  readCanvasCache,
  removeFromCanvasCache,
  writeCanvasCache,
} from "@/utils/home/canvas-cache";
import { canvasTransitionName } from "@/utils/home/canvas-transition";

const item = (id: string): CanvasListItemDto => ({
  id,
  title: `画布 ${id}`,
  coverUrl: null,
  revision: 1,
  updatedAt: "2026-10-15T00:00:00Z",
});

describe("画布列表缓存：转场要在页面第一次渲染时就有卡片", () => {
  beforeEach(() => clearCanvasCache());

  test("没写过就读不到", () => {
    expect(readCanvasCache("1")).toBeNull();
  });

  test("写进去的列表能按同一账号读回", () => {
    writeCanvasCache("1", [item("a"), item("b")]);
    expect(readCanvasCache("1")?.map((canvas) => canvas.id)).toEqual(["a", "b"]);
  });

  test("换账号读不到上一个人的画布", () => {
    writeCanvasCache("1", [item("a")]);
    expect(readCanvasCache("2")).toBeNull();
  });

  test("没登录（没有 user_id）既不写也不读", () => {
    writeCanvasCache(null, [item("a")]);
    expect(readCanvasCache(null)).toBeNull();
    expect(readCanvasCache("1")).toBeNull();
  });

  test("删除一张画布后缓存里也没有它，其余保持顺序", () => {
    writeCanvasCache("1", [item("a"), item("b"), item("c")]);
    removeFromCanvasCache("1", "b");
    expect(readCanvasCache("1")?.map((canvas) => canvas.id)).toEqual(["a", "c"]);
  });

  test("账号对不上时删除不影响别人的缓存", () => {
    writeCanvasCache("1", [item("a")]);
    removeFromCanvasCache("2", "a");
    expect(readCanvasCache("1")?.map((canvas) => canvas.id)).toEqual(["a"]);
  });
});

describe("canvasTransitionName：转场名字", () => {
  test("加固定前缀，保留字母数字、下划线和连字符", () => {
    expect(canvasTransitionName("a1-B_2")).toBe("canvas-card-a1-B_2");
  });

  test("其余字符去掉，避免生成非法的 CSS 标识符", () => {
    expect(canvasTransitionName("a/b c.1")).toBe("canvas-card-abc1");
  });

  test("新建画布卡有自己固定的名字", () => {
    expect(canvasTransitionName("new")).toBe("canvas-card-new");
  });
});
