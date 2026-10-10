import type { Capabilities } from "@/api/model/type";
import { refKindsOf } from "@/utils/tasks/capabilities";

/**
 * 图片扩图的纯逻辑（设计稿 docs/design/画布UI设计 6.17）：框的几何、比例识别、输出尺寸、提示词。
 * 坐标单位是「原图像素」，原图左上角是原点：框是 `x0 ≤ 0、y0 ≤ 0、x1 ≥ 原图宽、y1 ≥ 原图高`，
 * 即外框始终完整包住原图，只能往外扩。屏幕上怎么摆由调用方乘显示缩放。
 */

/** 外框：相对原图左上角的四条边 */
export type OutpaintFrame = { x0: number; y0: number; x1: number; y1: number };

/** 原图的像素尺寸 */
export type ImageSize = { width: number; height: number };

/** 框的最长边最多是原图最长边的几倍：到了就不能再扩 */
export const OUTPAINT_MAX_MULT = 3;

/** 「原比例」下可选的倍数 */
export const OUTPAINT_MULTS = [1, 1.25, 1.5, 2, 3] as const;

/** 默认倍数 */
export const OUTPAINT_DEFAULT_MULT = 1.5;

/** 比例条上的预设比例（宽 / 高） */
export const OUTPAINT_RATIOS = [
  { key: "21:9", ratio: 21 / 9 },
  { key: "4:3", ratio: 4 / 3 },
  { key: "1:1", ratio: 1 },
  { key: "3:4", ratio: 3 / 4 },
  { key: "9:16", ratio: 9 / 16 },
] as const;

/** 比例条上的按钮：原比例，或某个预设 */
export type RatioKey = "orig" | (typeof OUTPAINT_RATIOS)[number]["key"];

/** 拼给模型的大图最长边上限（像素）：超了整体等比缩小，免得超出模型能接收的大小 */
export const OUTPAINT_MAX_EDGE = 4096;

/** 判断「比例相同」的相对误差 */
const RATIO_EPSILON = 0.004;

/** 八个手柄：四条边、四个角（角手柄等比缩放） */
export const OUTPAINT_HANDLES = ["l", "r", "t", "b", "tl", "tr", "bl", "br"] as const;
export type OutpaintHandle = (typeof OUTPAINT_HANDLES)[number];

export const frameWidth = (f: OutpaintFrame) => f.x1 - f.x0;
export const frameHeight = (f: OutpaintFrame) => f.y1 - f.y0;

/** 框的最长边上限（原图像素） */
export const frameCap = (size: ImageSize) => OUTPAINT_MAX_MULT * Math.max(size.width, size.height);

/** 宽高为 w × h、原图居中的框 */
function centered(size: ImageSize, w: number, h: number): OutpaintFrame {
  const dx = (w - size.width) / 2;
  const dy = (h - size.height) / 2;
  return { x0: 0 - dx, y0: 0 - dy, x1: size.width + dx, y1: size.height + dy };
}

/** 原比例：长宽各放大 mult 倍，原图居中 */
export const frameByMult = (size: ImageSize, mult: number): OutpaintFrame =>
  centered(size, size.width * mult, size.height * mult);

/**
 * 某个比例（宽 / 高）下刚好包住原图的最小框，原图居中。
 * 比原图更宽的比例是高度不变、宽度补足；更窄的比例相反。
 */
export function frameByRatio(size: ImageSize, ratio: number): OutpaintFrame {
  return size.width / size.height >= ratio
    ? centered(size, size.width, size.width / ratio)
    : centered(size, size.height * ratio, size.height);
}

/** 这个框有没有超过 3 倍上限（预设比例会不会越界用它判断） */
export const fitsCap = (size: ImageSize, f: OutpaintFrame) =>
  Math.max(frameWidth(f), frameHeight(f)) <= frameCap(size) + 0.01;

/**
 * 当前框对应比例条上的哪个按钮：和原图比例相同是「原比例」（优先于预设），
 * 其次对照预设；都不是（拖手柄拖出了自定义比例）返回 null。
 */
export function detectRatio(size: ImageSize, f: OutpaintFrame): RatioKey | null {
  const ratio = frameWidth(f) / frameHeight(f);
  if (Math.abs(ratio / (size.width / size.height) - 1) < RATIO_EPSILON) return "orig";
  const hit = OUTPAINT_RATIOS.find((item) => Math.abs(ratio / item.ratio - 1) < RATIO_EPSILON);
  return hit ? hit.key : null;
}

/** 框相对原图的倍数：按最长边算 */
export const longMult = (size: ImageSize, f: OutpaintFrame) =>
  Math.max(frameWidth(f), frameHeight(f)) / Math.max(size.width, size.height);

/** 倍数显示成 1.5x、1.37x */
export const formatMult = (mult: number) =>
  `${(Math.round(mult * 100) / 100).toString().replace(/\.0+$/, "")}x`;

const clamp = (value: number, min: number, max: number) => Math.min(Math.max(value, min), max);

/**
 * 平移框：大小不变，只能在「仍然包住原图」的范围里动，所以不会把原图露在框外；
 * 框和原图一样大时没有余地，原样返回。
 */
export function moveFrame(
  size: ImageSize,
  frame: OutpaintFrame,
  dx: number,
  dy: number,
): OutpaintFrame {
  const w = frameWidth(frame);
  const h = frameHeight(frame);
  const x0 = clamp(frame.x0 + dx, size.width - w, 0);
  const y0 = clamp(frame.y0 + dy, size.height - h, 0);
  return { x0, y0, x1: x0 + w, y1: y0 + h };
}

/**
 * 拖手柄后的新框。边手柄只动一条边（自由比例），夹在「原图的边」和「3 倍上限」之间；
 * 角手柄锁定当前框的比例、对角固定，缩放系数夹在「刚好包住原图」和「3 倍上限」之间，
 * 横竖两个方向的位移取平均，拖起来更顺。
 * @param start 开始拖动那一刻的框（位移都相对它算，不要用上一帧的结果）
 * @param dx 横向位移（原图像素）
 * @param dy 纵向位移（原图像素）
 * @returns 新框；limited 为 true 表示想超过 3 倍上限、被拦住了
 */
export function resizeFrame(
  size: ImageSize,
  handle: OutpaintHandle,
  start: OutpaintFrame,
  dx: number,
  dy: number,
): { frame: OutpaintFrame; limited: boolean } {
  const cap = frameCap(size);
  let { x0, y0, x1, y1 } = start;
  let limited = false;

  if (handle.length === 1) {
    if (handle === "l") {
      const want = start.x0 + dx;
      x0 = clamp(want, start.x1 - cap, 0);
      limited = want < start.x1 - cap - 0.5;
    } else if (handle === "r") {
      const want = start.x1 + dx;
      x1 = clamp(want, size.width, start.x0 + cap);
      limited = want > start.x0 + cap + 0.5;
    } else if (handle === "t") {
      const want = start.y0 + dy;
      y0 = clamp(want, start.y1 - cap, 0);
      limited = want < start.y1 - cap - 0.5;
    } else {
      const want = start.y1 + dy;
      y1 = clamp(want, size.height, start.y0 + cap);
      limited = want > start.y0 + cap + 0.5;
    }
    return { frame: { x0, y0, x1, y1 }, limited };
  }

  const sw = frameWidth(start);
  const sh = frameHeight(start);
  const aspect = sw / sh;
  const hx = handle.includes("l") ? -1 : 1;
  const hy = handle.includes("t") ? -1 : 1;
  const anchorX = hx < 0 ? start.x1 : start.x0;
  const anchorY = hy < 0 ? start.y1 : start.y0;
  const want = (sw + hx * dx + (sh + hy * dy) * aspect) / 2;
  const lo = Math.max(
    hx > 0 ? size.width - anchorX : anchorX,
    (hy > 0 ? size.height - anchorY : anchorY) * aspect,
  );
  const hi = Math.max(lo, Math.min(cap, cap * aspect));
  const w = clamp(want, lo, hi);
  limited = want > hi + 0.5;
  const h = w / aspect;
  return {
    frame: {
      x0: hx > 0 ? anchorX : anchorX - w,
      x1: hx > 0 ? anchorX + w : anchorX,
      y0: hy > 0 ? anchorY : anchorY - h,
      y1: hy > 0 ? anchorY + h : anchorY,
    },
    limited,
  };
}

/**
 * 拼给模型的大图的像素尺寸：按原图像素 1:1，最长边超过上限就整体等比缩小。
 * scale 是相对原图像素的缩放，拼图时原图也按它缩。
 */
export function outputSize(f: OutpaintFrame): { width: number; height: number; scale: number } {
  const w = frameWidth(f);
  const h = frameHeight(f);
  const scale = Math.min(1, OUTPAINT_MAX_EDGE / Math.max(w, h));
  return { width: Math.round(w * scale), height: Math.round(h * scale), scale };
}

/** 比例的文字写法：预设比例用预设名，原比例和自定义比例写成 1.78:1 这样 */
export function ratioText(size: ImageSize, f: OutpaintFrame): string {
  const key = detectRatio(size, f);
  if (key && key !== "orig") return key;
  return `${(frameWidth(f) / frameHeight(f)).toFixed(2)}:1`;
}

/**
 * 扩图交给模型的提示词：固定指令在前，用户写的接在后面。
 * 指令让模型把透明区域补全、保持原图不变，并写明目标比例（拼好的大图本身就是目标比例，这里只是再强调一次）。
 */
export function buildOutpaintPrompt(userPrompt: string, ratio: string): string {
  const instruction = `保持原图内容不变，把透明区域按原图的画面风格、光线和透视自然补全成完整的图片，输出画面比例为 ${ratio}。`;
  const extra = userPrompt.trim();
  return extra ? `${instruction}\n${extra}` : instruction;
}

/**
 * 图片在节点预览框里的位置：等比放进盒子（object-contain），居中。
 * @returns 图片左上角相对盒子的偏移、显示尺寸，以及「原图一个像素显示成多少屏幕像素」
 */
export function containRect(image: ImageSize, box: { width: number; height: number }) {
  const scale = Math.min(box.width / image.width, box.height / image.height);
  const width = image.width * scale;
  const height = image.height * scale;
  return { x: (box.width - width) / 2, y: (box.height - height) / 2, width, height, scale };
}

/**
 * 当前模型能不能扩图：要支持图生图、并且收参考图。
 * @returns 不能时返回给用户看的原因，能返回 null
 */
export function outpaintBlocker(caps: Capabilities | undefined): string | null {
  if (!caps) return null;
  const ops = caps.ops ?? [];
  if (!ops.includes("i2i")) return "当前图片模型不支持图生图，换一个模型再扩图";
  if (refKindsOf(caps, "i2i").length === 0 || (caps.refs?.image?.max ?? 0) < 1) {
    return "当前图片模型不接收参考图，换一个模型再扩图";
  }
  return null;
}
