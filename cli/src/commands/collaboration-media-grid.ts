import { execFile } from "node:child_process";
import fs from "node:fs/promises";
import path from "node:path";
import { promisify } from "node:util";

import { APIError } from "../client.ts";
import type { Args } from "../types.ts";
import { intFlag, stringFlag } from "./args.ts";
import { readJSONFile } from "./shared.ts";

const run = promisify(execFile);
const maxTileEdge = 4096;

function evenCuts(count: number) {
  return Array.from({ length: count - 1 }, (_, index) => (index + 1) / count);
}

function validateCuts(cuts: unknown): number[] {
  if (!Array.isArray(cuts) || cuts.length > 4) throw new APIError(-1, "每个方向最多有四条切线", 2);
  let previous = 0;
  for (const cut of cuts) {
    if (
      typeof cut !== "number" ||
      !Number.isFinite(cut) ||
      cut - previous < 0.02 - 1e-10 ||
      cut > 0.98
    ) {
      throw new APIError(-1, "切线须按比例递增，与相邻切线和边缘至少间隔 2%", 2);
    }
    previous = cut;
  }
  return cuts as number[];
}

export function gridOptions(args: Args) {
  if (args.flags.input === undefined) {
    const rows = intFlag(args.flags, "rows", 3, { max: 5 });
    const columns = intFlag(args.flags, "columns", 3, { max: 5 });
    if (rows * columns < 2) throw new APIError(-1, "宫格至少需要两格", 2);
    return { xs: evenCuts(columns), ys: evenCuts(rows) };
  }
  if (args.flags.rows !== undefined || args.flags.columns !== undefined)
    throw new APIError(-1, "自定义切线文件不能和 --rows 或 --columns 同时使用", 2);
  const value = readJSONFile(stringFlag(args.flags, "input", "--input <切线 JSON 文件>"));
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw new APIError(-1, "切线文件需要包含 xs 和 ys 数组", 2);
  const xs = validateCuts(value.xs);
  const ys = validateCuts(value.ys);
  if (xs.length + ys.length === 0) throw new APIError(-1, "宫格至少需要一条切线", 2);
  return { xs, ys };
}

function pixelBounds(cuts: number[], size: number) {
  if (size < cuts.length + 1) throw new APIError(-1, "图片像素不足以按指定行列拆分", 422);
  const bounds = [0];
  for (const [index, cut] of cuts.entries()) {
    const upper = size - (cuts.length - index);
    bounds.push(Math.min(upper, Math.max(bounds[index] + 1, Math.round(cut * size))));
  }
  return [...bounds, size];
}

export async function pngDimensions(input: string) {
  const handle = await fs.open(input, "r");
  try {
    const header = Buffer.alloc(24);
    const { bytesRead } = await handle.read(header, 0, header.length, 0);
    if (bytesRead !== header.length || header.subarray(12, 16).toString() !== "IHDR") {
      throw new APIError(-1, "无法读取图片尺寸", 422);
    }
    return [header.readUInt32BE(16), header.readUInt32BE(20)] as const;
  } finally {
    await handle.close();
  }
}

export async function renderGrid(
  input: string,
  grid: ReturnType<typeof gridOptions>,
  directory: string,
) {
  const [sourceWidth, sourceHeight] = await pngDimensions(input);
  const columns = pixelBounds(grid.xs, sourceWidth);
  const rows = pixelBounds(grid.ys, sourceHeight);
  const tiles = [];
  const filters = [];
  const args = [];
  for (let row = 0; row < rows.length - 1; row++) {
    for (let column = 0; column < columns.length - 1; column++) {
      const index = tiles.length;
      const width = columns[column + 1] - columns[column];
      const height = rows[row + 1] - rows[row];
      const scale = Math.min(1, maxTileEdge / Math.max(width, height));
      const tile = {
        row: row + 1,
        column: column + 1,
        width: Math.max(1, Math.round(width * scale)),
        height: Math.max(1, Math.round(height * scale)),
        name: `tile-${row + 1}-${column + 1}.png`,
      };
      const resize = scale < 1 ? `,scale=${tile.width}:${tile.height}` : "";
      filters.push(
        `[part${index}]crop=${width}:${height}:${columns[column]}:${rows[row]}:exact=1${resize}[tile${index}]`,
      );
      args.push(
        "-map",
        `[tile${index}]`,
        "-frames:v",
        "1",
        "-threads",
        "1",
        path.join(directory, tile.name),
      );
      tiles.push(tile);
    }
  }
  const split = `[0:v:0]split=${tiles.length}${tiles.map((_, index) => `[part${index}]`).join("")}`;
  try {
    await run(
      "ffmpeg",
      [
        "-nostdin",
        "-v",
        "error",
        "-n",
        "-protocol_whitelist",
        "file,pipe",
        "-f",
        "png_pipe",
        "-i",
        input,
        "-filter_complex_threads",
        "1",
        "-filter_complex",
        [split, ...filters].join(";"),
        ...args,
      ],
      { timeout: 120_000, maxBuffer: 64 * 1024, windowsHide: true },
    );
  } catch {
    throw new APIError(-1, "图片宫格加工失败", 422);
  }
  return tiles;
}
