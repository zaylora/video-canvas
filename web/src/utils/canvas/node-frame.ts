import type { Capabilities } from "@/api/model/type";
import type { NodeMediaType } from "@/types";
import { effectiveValue, paramEntries, type ParamEntry } from "@/utils/tasks/capabilities";

/** 还不知道画幅时节点用的宽高比：16:9 */
export const DEFAULT_NODE_ASPECT = 16 / 9;

/** 音频节点的宽高比：1:1。音频没有画面也没有比例参数，不跟成片和参数走 */
export const AUDIO_NODE_ASPECT = 1;

/** 节点框的短边（像素）：任何画幅的短边都一样，16:9 的高、1:1 的边长都是它 */
export const NODE_FRAME_SHORT_SIDE = 324;

/** 不同模型给比例参数起的名字 */
const RATIO_PARAM_NAMES = ["aspect_ratio", "ratio"] as const;

/**
 * 把比例参数值（如「16:9」）读成宽高比。
 * @param value 比例参数值
 * @returns 宽 / 高；Auto、空值或写法不认识时为 null，由调用方兜底
 */
export function parseAspect(value: unknown): number | null {
  const match = /^(\d+(?:\.\d+)?):(\d+(?:\.\d+)?)$/.exec(String(value ?? ""));
  if (!match) return null;
  const width = Number(match[1]);
  const height = Number(match[2]);
  return width > 0 && height > 0 ? width / height : null;
}

/**
 * 按宽高比算节点框的像素尺寸：短边固定 NODE_FRAME_SHORT_SIDE，横图定高、竖图定宽。
 * 长边不封顶，极端比例会很长。
 * @param aspect 宽 / 高；无效值按默认 16:9
 * @returns 框的宽、高（像素）
 */
export function nodeFrameSize(aspect: number): { width: number; height: number } {
  const ratio = Number.isFinite(aspect) && aspect > 0 ? aspect : DEFAULT_NODE_ASPECT;
  return ratio >= 1
    ? { width: Math.round(NODE_FRAME_SHORT_SIDE * ratio), height: NODE_FRAME_SHORT_SIDE }
    : { width: NODE_FRAME_SHORT_SIDE, height: Math.round(NODE_FRAME_SHORT_SIDE / ratio) };
}

/**
 * 读用户在参数面板里选的比例；没选过就用参数默认值。
 * @param caps 所选模型的能力描述
 * @param params 节点里存的参数值
 * @returns 宽 / 高；模型没有比例参数、选了 Auto 或没有能力描述时为 null
 */
export function paramAspect(
  caps: Capabilities | undefined,
  params: Record<string, unknown>,
): number | null {
  const entry = ratioParam(caps);
  return entry ? parseAspect(effectiveValue(entry, params, entry.name)) : null;
}

/**
 * 模型的比例参数（aspect_ratio 或 ratio）。
 * @returns 参数名、是否开放给用户、可选值等；模型没有比例参数时为 undefined
 */
export function ratioParam(caps: Capabilities | undefined): ParamEntry | undefined {
  return paramEntries(caps).find((field) =>
    (RATIO_PARAM_NAMES as readonly string[]).includes(field.name),
  );
}

/**
 * 节点框此刻该用的宽高比：音频固定 1:1；其余量到的成片画幅优先，其次是节点提前记下的画幅，
 * 再其次是所选比例，都没有就 16:9。
 * @param input.measured 成片实际的宽 / 高；还没量到为 null
 * @param input.declared 节点数据里提前记下的宽 / 高（data.mediaAspect）；没记过不给或为 null
 * @param input.selected 参数面板里所选比例对应的宽 / 高；没有为 null
 * @param input.mediaType 节点的素材种类；不给按图片 / 视频处理
 * @returns 宽 / 高
 */
export function resolveNodeAspect(input: {
  measured: number | null;
  declared?: number | null;
  selected: number | null;
  mediaType?: NodeMediaType;
}): number {
  if (input.mediaType === "audio") return AUDIO_NODE_ASPECT;
  return input.measured ?? input.declared ?? input.selected ?? DEFAULT_NODE_ASPECT;
}
