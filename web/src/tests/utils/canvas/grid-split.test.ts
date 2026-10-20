import { describe, expect, test } from "bun:test";

import { COMPOSE_MAX_EDGE, type CompositeCanvas } from "@/utils/canvas/outpaint-compose";
import {
  MIN_GAP,
  clampLine,
  cutTile,
  cutTiles,
  evenLines,
  gridSplitLayerStyle,
  isValidGrid,
  planTiles,
  toBounds,
} from "@/utils/canvas/grid-split";

describe("evenLines 均分", () => {
  test("n 份有 n-1 条线", () => {
    expect(evenLines(1)).toEqual([]);
    expect(evenLines(2)).toEqual([0.5]);
    expect(evenLines(4)).toEqual([0.25, 0.5, 0.75]);
    expect(evenLines(5)).toHaveLength(4);
  });
});

describe("clampLine 拖线", () => {
  test("在空档里自由移动", () => {
    expect(clampLine([0.25, 0.5, 0.75], 1, 0.55)).toBe(0.55);
  });

  test("不能越过前一条线，留出最小间隔", () => {
    expect(clampLine([0.25, 0.5, 0.75], 1, 0.1)).toBeCloseTo(0.25 + MIN_GAP);
  });

  test("不能越过后一条线，留出最小间隔", () => {
    expect(clampLine([0.25, 0.5, 0.75], 1, 0.9)).toBeCloseTo(0.75 - MIN_GAP);
  });

  test("第一条线不能拖出图片左 / 上边，最后一条不能拖出右 / 下边", () => {
    expect(clampLine([0.5], 0, -1)).toBeCloseTo(MIN_GAP);
    expect(clampLine([0.5], 0, 2)).toBeCloseTo(1 - MIN_GAP);
  });

  test("最小间隔是 2%", () => {
    expect(MIN_GAP).toBe(0.02);
  });

  test("拖到极端位置后整组线依然严格递增、都在图内", () => {
    const lines = evenLines(5);
    lines[1] = clampLine(lines, 1, -5);
    lines[2] = clampLine(lines, 2, -5);
    lines[3] = clampLine(lines, 3, 9);
    const all = [0, ...lines, 1];
    for (let i = 1; i < all.length; i++) expect(all[i] - all[i - 1]).toBeGreaterThan(0.0199);
  });
});

describe("toBounds 累积整数边界", () => {
  test("1000 宽 3 列：边界 [0,333,667,1000]，总宽恰好等于原图", () => {
    const bounds = toBounds(evenLines(3), 1000);
    expect(bounds).toEqual([0, 333, 667, 1000]);
    const widths = bounds.slice(1).map((value, i) => value - bounds[i]);
    expect(widths).toEqual([333, 334, 333]);
    expect(widths.reduce((sum, value) => sum + value, 0)).toBe(1000);
  });

  test("逐格取整会少 1px，累积边界不会", () => {
    // 反例：每格宽 1000/3 各自取整，3 格加起来是 999
    expect(Math.round(1000 / 3) * 3).toBe(999);
    const bounds = toBounds(evenLines(3), 1000);
    expect(bounds[bounds.length - 1] - bounds[0]).toBe(1000);
  });

  test("首尾固定是 0 和总长", () => {
    const bounds = toBounds([0.3, 0.9], 777);
    expect(bounds[0]).toBe(0);
    expect(bounds[bounds.length - 1]).toBe(777);
  });

  test("图片很小、取整会让两条线重合时，每格仍至少 1px", () => {
    const bounds = toBounds([0.34, 0.341], 10);
    for (let i = 1; i < bounds.length; i++) expect(bounds[i]).toBeGreaterThan(bounds[i - 1]);
    expect(bounds[bounds.length - 1]).toBe(10);
  });

  test("像素数比格数还少时不强行凑，仍以总长收尾", () => {
    const bounds = toBounds(evenLines(5), 3);
    expect(bounds[0]).toBe(0);
    expect(bounds[bounds.length - 1]).toBe(3);
  });
});

describe("planTiles 切分方案", () => {
  test("按行优先给出每一格，位置和尺寸都是整数", () => {
    const tiles = planTiles(evenLines(3), evenLines(2), 1000, 600);
    expect(tiles).toHaveLength(6);
    expect(tiles[0]).toEqual({ row: 0, col: 0, x: 0, y: 0, w: 333, h: 300 });
    expect(tiles[1]).toEqual({ row: 0, col: 1, x: 333, y: 0, w: 334, h: 300 });
    expect(tiles[5]).toEqual({ row: 1, col: 2, x: 667, y: 300, w: 333, h: 300 });
  });

  test("3 列的每一行总宽等于原图宽，格与格严格相接", () => {
    const tiles = planTiles(evenLines(3), evenLines(3), 1000, 1000);
    for (let row = 0; row < 3; row++) {
      const cells = tiles.filter((tile) => tile.row === row);
      expect(cells.reduce((sum, tile) => sum + tile.w, 0)).toBe(1000);
      for (let i = 1; i < cells.length; i++)
        expect(cells[i].x).toBe(cells[i - 1].x + cells[i - 1].w);
    }
  });

  test("5×5 也严格相接：行、列的总长都等于原图，且没有缝和重叠", () => {
    const width = 1921;
    const height = 1083;
    const tiles = planTiles(evenLines(5), evenLines(5), width, height);
    expect(tiles).toHaveLength(25);
    for (let row = 0; row < 5; row++) {
      const cells = tiles.filter((tile) => tile.row === row);
      expect(cells.reduce((sum, tile) => sum + tile.w, 0)).toBe(width);
      expect(cells[0].x).toBe(0);
      for (let i = 1; i < cells.length; i++)
        expect(cells[i].x).toBe(cells[i - 1].x + cells[i - 1].w);
    }
    for (let col = 0; col < 5; col++) {
      const cells = tiles.filter((tile) => tile.col === col);
      expect(cells.reduce((sum, tile) => sum + tile.h, 0)).toBe(height);
      expect(cells[0].y).toBe(0);
      for (let i = 1; i < cells.length; i++)
        expect(cells[i].y).toBe(cells[i - 1].y + cells[i - 1].h);
    }
    // 总面积 = 原图面积，说明没有缝也没有重叠
    expect(tiles.reduce((sum, tile) => sum + tile.w * tile.h, 0)).toBe(width * height);
  });

  test("拖过的线：格子大小跟着线走，同一列的格子等宽", () => {
    const xs = [0.2, 0.9];
    const tiles = planTiles(xs, [0.5], 1000, 400);
    expect(tiles.filter((tile) => tile.row === 0).map((tile) => tile.w)).toEqual([200, 700, 100]);
    expect(tiles.filter((tile) => tile.row === 1).map((tile) => tile.w)).toEqual([200, 700, 100]);
  });

  test("1×N 横幅：只有竖线", () => {
    const tiles = planTiles(evenLines(2), evenLines(1), 600, 200);
    expect(tiles.map((tile) => [tile.x, tile.y, tile.w, tile.h])).toEqual([
      [0, 0, 300, 200],
      [300, 0, 300, 200],
    ]);
  });
});

describe("isValidGrid 行列合法性", () => {
  test("行列各 1 到 5 且至少 2 格", () => {
    expect(isValidGrid(2, 2)).toBe(true);
    expect(isValidGrid(5, 5)).toBe(true);
    expect(isValidGrid(1, 2)).toBe(true);
    expect(isValidGrid(5, 1)).toBe(true);
  });

  test("1×1 不可选", () => {
    expect(isValidGrid(1, 1)).toBe(false);
  });

  test("越界、小数、非数字都不行", () => {
    expect(isValidGrid(0, 3)).toBe(false);
    expect(isValidGrid(3, 6)).toBe(false);
    expect(isValidGrid(2.5, 2)).toBe(false);
    expect(isValidGrid(Number.NaN, 2)).toBe(false);
  });
});

/** 记录调用的假画布 */
function fakeCanvas() {
  const calls: unknown[][] = [];
  const made: Array<CompositeCanvas & { width: number; height: number }> = [];
  const makeCanvas = () => {
    const canvas: CompositeCanvas & { width: number; height: number } = {
      width: 0,
      height: 0,
      getContext: () => ({ drawImage: (...args: unknown[]) => calls.push(args) }),
      toBlob: (cb, type) => cb(new Blob(["png"], { type: type ?? "" })),
    };
    made.push(canvas);
    return canvas;
  };
  return { makeCanvas, calls, made };
}

const source = {} as CanvasImageSource;

describe("cutTile / cutTiles 切图", () => {
  test("按原图分辨率输出：画布和格子一样大，不缩放", async () => {
    const { makeCanvas, calls, made } = fakeCanvas();
    const blob = await cutTile(
      source,
      { row: 0, col: 1, x: 333, y: 0, w: 334, h: 300 },
      makeCanvas,
    );
    expect(blob.type).toBe("image/png");
    expect([made[0].width, made[0].height]).toEqual([334, 300]);
    // drawImage(源, 源x, 源y, 源w, 源h, 目标x, 目标y, 目标w, 目标h)
    expect(calls[0]).toEqual([source, 333, 0, 334, 300, 0, 0, 334, 300]);
  });

  test("单格长边超过上限才等比缩小", async () => {
    const { makeCanvas, calls, made } = fakeCanvas();
    await cutTile(source, { row: 0, col: 0, x: 0, y: 0, w: 8192, h: 4096 }, makeCanvas);
    expect([made[0].width, made[0].height]).toEqual([COMPOSE_MAX_EDGE, COMPOSE_MAX_EDGE / 2]);
    // 源区域仍然是整格，只是目标缩小
    expect(calls[0]).toEqual([
      source,
      0,
      0,
      8192,
      4096,
      0,
      0,
      COMPOSE_MAX_EDGE,
      COMPOSE_MAX_EDGE / 2,
    ]);
  });

  test("恰好等于上限不缩小", async () => {
    const { makeCanvas, made } = fakeCanvas();
    await cutTile(source, { row: 0, col: 0, x: 0, y: 0, w: COMPOSE_MAX_EDGE, h: 100 }, makeCanvas);
    expect(made[0].width).toBe(COMPOSE_MAX_EDGE);
  });

  test("拿不到绘图上下文时抛出能直接显示的错误", async () => {
    const makeCanvas = (): CompositeCanvas => ({
      width: 0,
      height: 0,
      getContext: () => null,
      toBlob: () => undefined,
    });
    await expect(
      cutTile(source, { row: 0, col: 0, x: 0, y: 0, w: 10, h: 10 }, makeCanvas),
    ).rejects.toThrow("切图失败");
  });

  test("导出失败（toBlob 给 null）时抛错", async () => {
    const makeCanvas = (): CompositeCanvas => ({
      width: 0,
      height: 0,
      getContext: () => ({ drawImage: () => undefined }),
      toBlob: (cb) => cb(null),
    });
    await expect(
      cutTile(source, { row: 0, col: 0, x: 0, y: 0, w: 10, h: 10 }, makeCanvas),
    ).rejects.toThrow("切图失败");
  });

  test("cutTiles 按传入顺序每格一个 PNG", async () => {
    const { makeCanvas, calls } = fakeCanvas();
    const tiles = planTiles(evenLines(2), evenLines(2), 100, 100);
    const blobs = await cutTiles(source, tiles, makeCanvas);
    expect(blobs).toHaveLength(4);
    expect(calls.map((args) => [args[1], args[2]])).toEqual([
      [0, 0],
      [50, 0],
      [0, 50],
      [50, 50],
    ]);
  });
});

describe("gridSplitLayerStyle 切分线图层的定位样式", () => {
  test("盖在图片节点上：位置和大小等于节点的画布坐标和大小", () => {
    const style = gridSplitLayerStyle({ x: 100, y: 50, w: 300, h: 200 });
    expect(style.transform).toBe("translate(100px, 50px)");
    expect(style.width).toBe(300);
    expect(style.height).toBe(200);
  });

  test("层级高于被选中的节点：xyflow 会给选中节点加 1000，切分时节点一定是选中的", () => {
    // 低了切分线和按钮被节点压在下面，看不见也拖不动
    expect(gridSplitLayerStyle({ x: 0, y: 0, w: 1, h: 1 }).zIndex).toBeGreaterThan(1000);
  });
});
