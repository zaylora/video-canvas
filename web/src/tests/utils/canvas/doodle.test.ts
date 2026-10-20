import { describe, expect, test } from "bun:test";

import { COMPOSE_MAX_EDGE } from "@/utils/canvas/outpaint-compose";
import {
  DOODLE_COLORS,
  DOODLE_WIDTH_DEFAULT,
  DOODLE_WIDTH_MAX,
  DOODLE_WIDTH_MIN,
  createHistoryStack,
  doodleCanvasSize,
  doodleDisplayRect,
  doodleFontSize,
  doodleScale,
  doodleStageStyle,
  doodleStrokeWidth,
  hasDoodleContent,
} from "@/utils/canvas/doodle";

describe("色盘", () => {
  test("8 色，默认第 1 个是红色，没有重复", () => {
    expect(DOODLE_COLORS).toHaveLength(8);
    expect(DOODLE_COLORS[0]).toBe("#ef4444");
    expect(new Set(DOODLE_COLORS).size).toBe(8);
  });

  test("最后一个是白色", () => {
    expect(DOODLE_COLORS[7]).toBe("#ffffff");
  });
});

describe("粗细范围", () => {
  test("默认值落在最小最大之间", () => {
    expect(DOODLE_WIDTH_MIN).toBeLessThan(DOODLE_WIDTH_MAX);
    expect(DOODLE_WIDTH_DEFAULT).toBeGreaterThanOrEqual(DOODLE_WIDTH_MIN);
    expect(DOODLE_WIDTH_DEFAULT).toBeLessThanOrEqual(DOODLE_WIDTH_MAX);
  });
});

describe("doodleCanvasSize 画布逻辑尺寸", () => {
  test("不超过上限就是原图像素", () => {
    expect(doodleCanvasSize({ w: 1280, h: 720 })).toEqual({ width: 1280, height: 720 });
  });

  test("正好等于上限不缩", () => {
    expect(doodleCanvasSize({ w: COMPOSE_MAX_EDGE, h: 1000 })).toEqual({
      width: COMPOSE_MAX_EDGE,
      height: 1000,
    });
  });

  test("长边超上限按比例缩到上限，宽图", () => {
    expect(doodleCanvasSize({ w: 8192, h: 4096 })).toEqual({
      width: COMPOSE_MAX_EDGE,
      height: 2048,
    });
  });

  test("长边超上限按比例缩到上限，竖图", () => {
    expect(doodleCanvasSize({ w: 3000, h: 6000 })).toEqual({
      width: 2048,
      height: COMPOSE_MAX_EDGE,
    });
  });

  test("可以指定上限", () => {
    expect(doodleCanvasSize({ w: 2000, h: 1000 }, 1000)).toEqual({ width: 1000, height: 500 });
  });

  test("缩完最短边至少 1 像素", () => {
    expect(doodleCanvasSize({ w: 100000, h: 1 })).toEqual({ width: COMPOSE_MAX_EDGE, height: 1 });
  });
});

describe("doodleDisplayRect 画布在节点里的显示区域", () => {
  test("比例一致就铺满", () => {
    expect(doodleDisplayRect({ w: 1280, h: 720 }, { width: 576, height: 324 })).toEqual({
      x: 0,
      y: 0,
      width: 576,
      height: 324,
    });
  });

  test("节点比图更扁，图按高度 contain，水平居中", () => {
    const rect = doodleDisplayRect({ w: 100, h: 100 }, { width: 400, height: 200 });
    expect(rect).toEqual({ x: 100, y: 0, width: 200, height: 200 });
  });

  test("节点比图更高，图按宽度 contain，垂直居中", () => {
    const rect = doodleDisplayRect({ w: 200, h: 100 }, { width: 200, height: 300 });
    expect(rect).toEqual({ x: 0, y: 100, width: 200, height: 100 });
  });

  test("原图尺寸无效就铺满节点", () => {
    expect(doodleDisplayRect({ w: 0, h: 0 }, { width: 300, height: 200 })).toEqual({
      x: 0,
      y: 0,
      width: 300,
      height: 200,
    });
  });
});

describe("粗细与字号换算", () => {
  test("缩放比例是逻辑宽 / 显示宽", () => {
    expect(doodleScale(1280, 640)).toBe(2);
    expect(doodleScale(500, 1000)).toBe(0.5);
  });

  test("显示宽无效时比例按 1", () => {
    expect(doodleScale(1280, 0)).toBe(1);
    expect(doodleScale(1280, Number.NaN)).toBe(1);
  });

  test("屏幕粗细乘比例得到逻辑粗细", () => {
    expect(doodleStrokeWidth(6, 2)).toBe(12);
    expect(doodleStrokeWidth(6, 1)).toBe(6);
  });

  test("字号 = (12 + 粗细 × 1.2) × 比例", () => {
    expect(doodleFontSize(10, 1)).toBeCloseTo(24);
    expect(doodleFontSize(10, 2)).toBeCloseTo(48);
    expect(doodleFontSize(2, 1.5)).toBeCloseTo((12 + 2.4) * 1.5);
  });
});

describe("hasDoodleContent 有没有标注内容", () => {
  test("空就是没有", () => {
    expect(hasDoodleContent([])).toBe(false);
  });

  test("有笔画或框（没有 text 字段）就是有", () => {
    expect(hasDoodleContent([{}])).toBe(true);
  });

  test("有非空文字就是有", () => {
    expect(hasDoodleContent([{ text: "你好" }])).toBe(true);
  });

  test("只有空白文字不算", () => {
    expect(hasDoodleContent([{ text: "" }, { text: "  \n " }])).toBe(false);
  });

  test("空白文字加一笔，算有", () => {
    expect(hasDoodleContent([{ text: " " }, {}])).toBe(true);
  });
});

describe("createHistoryStack 撤销重做栈", () => {
  test("新栈什么都不能做，undo / redo 返回 undefined", () => {
    const stack = createHistoryStack<string>();
    expect(stack.canUndo()).toBe(false);
    expect(stack.canRedo()).toBe(false);
    expect(stack.undo()).toBeUndefined();
    expect(stack.redo()).toBeUndefined();
    expect(stack.items()).toEqual([]);
  });

  test("push 后可撤销，不可重做", () => {
    const stack = createHistoryStack<string>();
    stack.push("a");
    expect(stack.canUndo()).toBe(true);
    expect(stack.canRedo()).toBe(false);
    expect(stack.items()).toEqual(["a"]);
  });

  test("undo 从后往前取，redo 按撤销的逆序放回", () => {
    const stack = createHistoryStack<string>();
    stack.push("a");
    stack.push("b");
    stack.push("c");
    expect(stack.undo()).toBe("c");
    expect(stack.undo()).toBe("b");
    expect(stack.items()).toEqual(["a"]);
    expect(stack.canRedo()).toBe(true);
    expect(stack.redo()).toBe("b");
    expect(stack.redo()).toBe("c");
    expect(stack.items()).toEqual(["a", "b", "c"]);
    expect(stack.redo()).toBeUndefined();
  });

  test("撤销到空后再撤销返回 undefined，不会坏掉", () => {
    const stack = createHistoryStack<number>();
    stack.push(1);
    expect(stack.undo()).toBe(1);
    expect(stack.undo()).toBeUndefined();
    expect(stack.canUndo()).toBe(false);
    expect(stack.redo()).toBe(1);
  });

  test("push 新对象清空重做栈", () => {
    const stack = createHistoryStack<string>();
    stack.push("a");
    stack.push("b");
    stack.undo();
    expect(stack.canRedo()).toBe(true);
    stack.push("c");
    expect(stack.canRedo()).toBe(false);
    expect(stack.redo()).toBeUndefined();
    expect(stack.items()).toEqual(["a", "c"]);
  });

  test("可以放对象，取回来是同一个引用", () => {
    const stack = createHistoryStack<{ id: number }>();
    const shape = { id: 1 };
    stack.push(shape);
    expect(stack.undo()).toBe(shape);
    expect(stack.redo()).toBe(shape);
  });

  test("items 返回的是副本，外面改它不影响栈", () => {
    const stack = createHistoryStack<string>();
    stack.push("a");
    const copy = stack.items() as string[];
    copy.push("x");
    expect(stack.items()).toEqual(["a"]);
  });
});

describe("doodleStageStyle 涂鸦画布的定位样式", () => {
  const rect = { x: 10, y: 20, width: 300, height: 200 };

  test("按节点左上角加上画布内的偏移定位，大小等于显示区域", () => {
    const style = doodleStageStyle({ x: 100, y: 50 }, rect);
    expect(style.transform).toBe("translate(110px, 70px)");
    expect(style.width).toBe(300);
    expect(style.height).toBe(200);
  });

  test("层级高于被选中的节点：xyflow 会给选中节点加 1000，涂鸦时节点一定是选中的", () => {
    // 低了画布会被节点压在下面，指针事件落到节点上，Fabric 收不到，表现为“画不上去”
    expect(doodleStageStyle({ x: 0, y: 0 }, rect).zIndex).toBeGreaterThan(1000);
  });
});
