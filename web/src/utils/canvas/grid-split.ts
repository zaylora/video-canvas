import { COMPOSE_MAX_EDGE, type CompositeCanvas } from "./outpaint-compose";
import { SELECTED_NODE_OVERLAY_Z_INDEX } from "./overlay-layer";

/**
 * 图片宫格切分里所有能单独讲的算法：切分线的均分与约束、累积整数边界、切分方案、行列合法性、切图。
 * 切分线一律用 0 到 1 的小数表示（占图片对应边长的比例），和图片的真实像素、节点的显示大小无关；
 * 只有 planTiles 和切图才换算成像素。不碰 DOM，canvas 可注入，所以能直接测。
 */

/**
 * 切分线图层盖在图片节点上的定位样式（画布坐标）。层级要高于选中的节点，否则线和按钮被节点压在下面。
 * @param box 图片节点的位置和大小
 */
export function gridSplitLayerStyle(box: { x: number; y: number; w: number; h: number }): {
  transform: string;
  width: number;
  height: number;
  zIndex: number;
} {
  return {
    transform: `translate(${box.x}px, ${box.y}px)`,
    width: box.w,
    height: box.h,
    zIndex: SELECTED_NODE_OVERLAY_Z_INDEX,
  };
}

/** 相邻两条切分线（含图片边）最小间隔：图片对应边长的 2% */
export const MIN_GAP = 0.02;

/** 行数、列数的上限 */
export const GRID_MAX = 5;

/** 切分方案里的一格：位置和尺寸都是原图的整数像素 */
export type Tile = {
  /** 第几行，从 0 起 */
  row: number;
  /** 第几列，从 0 起 */
  col: number;
  /** 左上角 x（像素） */
  x: number;
  /** 左上角 y（像素） */
  y: number;
  /** 宽（像素） */
  w: number;
  /** 高（像素） */
  h: number;
};

/**
 * 均分：n 份需要 n-1 条线。
 * @param n 分成几份
 * @returns 0 到 1 之间严格递增的线位置；n 小于 2 时为空
 */
export function evenLines(n: number): number[] {
  return Array.from({ length: Math.max(0, n - 1) }, (_, i) => (i + 1) / n);
}

/**
 * 拖动第 i 条线时把目标位置限制到合法范围：不越过前后两条线（或图片边），并留出 MIN_GAP。
 * @param lines 同一方向上全部线的当前位置（0 到 1，递增）
 * @param index 正在拖的线的下标
 * @param value 想拖到的位置
 * @returns 限制之后的位置
 */
export function clampLine(lines: readonly number[], index: number, value: number): number {
  const lo = (index > 0 ? lines[index - 1] : 0) + MIN_GAP;
  const hi = (index < lines.length - 1 ? lines[index + 1] : 1) - MIN_GAP;
  return Math.min(hi, Math.max(lo, value));
}

/**
 * 累积整数边界：先把每条线换算成像素并取整，格宽就是相邻边界之差。
 * 这样格与格严格相接，总长恰好等于原图；逐格各自取整则会因为舍入少几个像素。
 * 图片很小、取整会让两条边界重合时，把边界顺延，保证每格至少 1px（像素数不足格数时不强求）。
 * @param lines 一个方向上的切分线（0 到 1，递增）
 * @param total 原图在这个方向上的像素数
 * @returns 长度为 lines.length + 2 的边界，首项 0，末项 total
 */
export function toBounds(lines: readonly number[], total: number): number[] {
  const segments = lines.length + 1;
  const bounds = [0];
  lines.forEach((fraction, i) => {
    const rounded = Math.round(fraction * total);
    if (total < segments) {
      bounds.push(rounded);
      return;
    }
    // 前面至少留 1px，后面每个剩余的格也至少留 1px
    const lo = bounds[i] + 1;
    const hi = total - (segments - 1 - i);
    bounds.push(Math.min(hi, Math.max(lo, rounded)));
  });
  bounds.push(total);
  return bounds;
}

/**
 * 算每一格在原图里的像素区域，按行优先排列。
 * @param xs 竖线位置（0 到 1）
 * @param ys 横线位置（0 到 1）
 * @param width 原图宽（像素）
 * @param height 原图高（像素）
 */
export function planTiles(
  xs: readonly number[],
  ys: readonly number[],
  width: number,
  height: number,
): Tile[] {
  const xb = toBounds(xs, width);
  const yb = toBounds(ys, height);
  const tiles: Tile[] = [];
  for (let row = 0; row < yb.length - 1; row++) {
    for (let col = 0; col < xb.length - 1; col++) {
      tiles.push({
        row,
        col,
        x: xb[col],
        y: yb[row],
        w: xb[col + 1] - xb[col],
        h: yb[row + 1] - yb[row],
      });
    }
  }
  return tiles;
}

/**
 * 行列数是否可以切：各自是 1 到 GRID_MAX 的整数，且至少两格（1×1 等于没切）。
 * @param rows 行数
 * @param cols 列数
 */
export function isValidGrid(rows: number, cols: number): boolean {
  const inRange = (n: number) => Number.isInteger(n) && n >= 1 && n <= GRID_MAX;
  return inRange(rows) && inRange(cols) && rows * cols >= 2;
}

const createCanvas = (): CompositeCanvas =>
  document.createElement("canvas") as unknown as CompositeCanvas;

/**
 * 切出一格，导出 PNG：按原图分辨率输出，不缩放；单格长边超过 maxEdge 才等比缩小。
 * @param source 已解码的原图
 * @param tile planTiles 里的一格
 * @param makeCanvas 造画布的函数，测试里注入假的
 * @param maxEdge 长边上限，默认 COMPOSE_MAX_EDGE
 * @throws 拿不到绘图上下文或导出失败时抛 Error，文案可直接显示给用户
 */
export async function cutTile(
  source: CanvasImageSource,
  tile: Tile,
  makeCanvas: () => CompositeCanvas = createCanvas,
  maxEdge: number = COMPOSE_MAX_EDGE,
): Promise<Blob> {
  const longest = Math.max(tile.w, tile.h);
  const shrink = longest > maxEdge ? maxEdge / longest : 1;
  const width = Math.max(1, Math.round(tile.w * shrink));
  const height = Math.max(1, Math.round(tile.h * shrink));

  const canvas = makeCanvas();
  canvas.width = width;
  canvas.height = height;
  const context = canvas.getContext("2d");
  if (!context) throw new Error("切图失败：浏览器不支持画布");
  (context.drawImage as (...args: unknown[]) => void)(
    source,
    tile.x,
    tile.y,
    tile.w,
    tile.h,
    0,
    0,
    width,
    height,
  );
  return new Promise<Blob>((resolve, reject) => {
    canvas.toBlob(
      (blob) => (blob ? resolve(blob) : reject(new Error("切图失败：图片导出失败"))),
      "image/png",
    );
  });
}

/**
 * 依次切出所有格（一次一格，不同时占多张画布）。
 * @param source 已解码的原图
 * @param tiles planTiles 的结果
 * @param makeCanvas 造画布的函数，测试里注入假的
 * @param maxEdge 长边上限，默认 COMPOSE_MAX_EDGE
 * @returns 与 tiles 同序的 PNG
 */
export async function cutTiles(
  source: CanvasImageSource,
  tiles: readonly Tile[],
  makeCanvas: () => CompositeCanvas = createCanvas,
  maxEdge: number = COMPOSE_MAX_EDGE,
): Promise<Blob[]> {
  const blobs: Blob[] = [];
  for (const tile of tiles) blobs.push(await cutTile(source, tile, makeCanvas, maxEdge));
  return blobs;
}
