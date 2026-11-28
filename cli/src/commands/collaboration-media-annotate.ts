import { execFile } from "node:child_process";
import fs from "node:fs/promises";
import path from "node:path";
import { promisify } from "node:util";

import { APIError } from "../client.ts";
import type { Args } from "../types.ts";
import { stringFlag } from "./args.ts";
import { pngDimensions } from "./collaboration-media-grid.ts";
import { readJSONFile } from "./shared.ts";

const run = promisify(execFile);
const maxRasterWork = 32 * 1024 * 1024;

type Point = [number, number];
type Stroke = { color: Buffer; width: number; points: Point[] };
type Raster = { pixels: Buffer; width: number; height: number; work: number };

function validPoint(value: unknown): value is Point {
  return (
    Array.isArray(value) &&
    value.length === 2 &&
    value.every((coord) => typeof coord === "number" && coord >= 0 && coord <= 1)
  );
}

export async function annotationOptions(args: Args): Promise<Stroke[]> {
  const input = stringFlag(args.flags, "input", "--input <笔画 JSON 文件>");
  const stat = await fs.stat(input).catch(() => undefined);
  if (!stat?.isFile() || stat.size > 256 * 1024)
    throw new APIError(-1, "请提供不超过 256 KB 的笔画文件", 2);
  const { strokes = undefined } = readJSONFile(input) ?? {};
  if (!Array.isArray(strokes) || strokes.length < 1 || strokes.length > 64)
    throw new APIError(-1, "笔画文件需要 1–64 条 strokes", 2);
  let pointCount = 0;
  return strokes.map((stroke: unknown) => {
    const { color, width, points } = (stroke ?? {}) as Record<string, unknown>;
    if (
      typeof color !== "string" ||
      !/^#[0-9a-f]{6}$/i.test(color) ||
      typeof width !== "number" ||
      !Number.isInteger(width) ||
      width < 1 ||
      width > 128 ||
      !Array.isArray(points) ||
      points.length === 0 ||
      !points.every(validPoint) ||
      (pointCount += points.length) > 2048
    ) {
      throw new APIError(
        -1,
        "笔画需要 #RRGGBB 颜色、1–128 像素宽度和 [0,1] 坐标，总计最多 2048 点",
        2,
      );
    }
    return { color: Buffer.from(color.slice(1), "hex"), width, points };
  });
}

function paintSegment(raster: Raster, stroke: Stroke, first: Point, last: Point) {
  const startX = Math.round(first[0] * (raster.width - 1));
  const startY = Math.round(first[1] * (raster.height - 1));
  const endX = Math.round(last[0] * (raster.width - 1));
  const endY = Math.round(last[1] * (raster.height - 1));
  const radius = stroke.width / 2;
  const left = Math.max(0, Math.ceil(Math.min(startX, endX) - radius));
  const top = Math.max(0, Math.ceil(Math.min(startY, endY) - radius));
  const right = Math.min(raster.width - 1, Math.floor(Math.max(startX, endX) + radius));
  const bottom = Math.min(raster.height - 1, Math.floor(Math.max(startY, endY) + radius));
  raster.work += (right - left + 1) * (bottom - top + 1);
  if (raster.work > maxRasterWork) throw new APIError(-1, "笔画覆盖范围过大，请减少笔画", 422);
  const deltaX = endX - startX;
  const deltaY = endY - startY;
  const lengthSquared = deltaX * deltaX + deltaY * deltaY;
  for (let row = top; row <= bottom; row++) {
    for (let column = left; column <= right; column++) {
      const offsetX = column - startX;
      const offsetY = row - startY;
      const projection = lengthSquared ? (offsetX * deltaX + offsetY * deltaY) / lengthSquared : 0;
      const progress = Math.max(0, Math.min(1, projection));
      const distanceX = offsetX - progress * deltaX;
      const distanceY = offsetY - progress * deltaY;
      if (distanceX * distanceX + distanceY * distanceY > radius * radius) continue;
      const pixel = (row * raster.width + column) * 4;
      stroke.color.copy(raster.pixels, pixel);
      raster.pixels[pixel + 3] = 255;
    }
  }
}

export async function renderAnnotation(input: string, strokes: Stroke[], directory: string) {
  const [width, height] = await pngDimensions(input);
  const size = width * height * 4;
  if (size < 4 || size > 64 * 1024 * 1024) throw new APIError(-1, "图片像素超出加工限制", 422);
  const flags = ["-hide_banner", "-nostdin", "-v", "error", "-protocol_whitelist", "file,pipe"];
  const settings = { timeout: 120_000, windowsHide: true };
  const raw = path.join(directory, "pixels.rgba");
  const output = path.join(directory, "annotation.png");
  try {
    const { stdout: pixels } = await run(
      "ffmpeg",
      [
        ...flags,
        "-i",
        input,
        "-frames:v",
        "1",
        "-threads",
        "1",
        "-f",
        "rawvideo",
        "-pix_fmt",
        "rgba",
        "pipe:1",
      ],
      { ...settings, encoding: "buffer", maxBuffer: size + 4096 },
    );
    if (pixels.length !== size) throw new APIError(-1, "解码后的像素尺寸不匹配", 422);
    const raster = { pixels, width, height, work: 0 };
    for (const stroke of strokes) {
      for (const [index, point] of stroke.points.entries()) {
        paintSegment(raster, stroke, stroke.points[index - 1] ?? point, point);
      }
    }
    await fs.writeFile(raw, pixels, { mode: 0o600, flag: "wx" });
    await run(
      "ffmpeg",
      [
        ...flags,
        "-f",
        "rawvideo",
        "-pixel_format",
        "rgba",
        "-video_size",
        `${width}x${height}`,
        "-i",
        raw,
        "-frames:v",
        "1",
        "-threads",
        "1",
        "-n",
        output,
      ],
      settings,
    );
  } catch (error) {
    if (error instanceof APIError) throw error;
    throw new APIError(-1, "图片笔画渲染失败", 422);
  }
  return output;
}
