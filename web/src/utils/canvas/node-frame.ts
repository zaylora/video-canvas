import type { Capabilities } from "@/api/model/type";
import { effectiveValue, paramEntries } from "@/utils/tasks/capabilities";

/** 还不知道画幅时节点用的宽高比：16:9 */
export const DEFAULT_NODE_ASPECT = 16 / 9;

/** 节点框的包围盒（像素）：任何画幅都收在里面，横图贴满宽，竖图贴满高 */
export const NODE_FRAME_MAX = { width: 576, height: 324 } as const;

/** 框最窄、最矮的限度（像素），再极端的比例也要放得下占位文字和按钮 */
const NODE_FRAME_MIN = { width: 160, height: 120 } as const;

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
 * 按宽高比算节点框的像素尺寸：限定在 NODE_FRAME_MAX 内，横图贴满宽、竖图贴满高。
 * 极端比例会被收在 NODE_FRAME_MIN 之上，免得框窄到放不下内容。
 * @param aspect 宽 / 高；无效值按默认 16:9
 * @returns 框的宽、高（像素）
 */
export function nodeFrameSize(aspect: number): { width: number; height: number } {
  const ratio = Number.isFinite(aspect) && aspect > 0 ? aspect : DEFAULT_NODE_ASPECT;
  const minRatio = NODE_FRAME_MIN.width / NODE_FRAME_MAX.height;
  const maxRatio = NODE_FRAME_MAX.width / NODE_FRAME_MIN.height;
  const clamped = Math.min(Math.max(ratio, minRatio), maxRatio);
  const boxRatio = NODE_FRAME_MAX.width / NODE_FRAME_MAX.height;
  return clamped >= boxRatio
    ? { width: NODE_FRAME_MAX.width, height: Math.round(NODE_FRAME_MAX.width / clamped) }
    : { width: Math.round(NODE_FRAME_MAX.height * clamped), height: NODE_FRAME_MAX.height };
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
  const entry = paramEntries(caps).find((field) =>
    (RATIO_PARAM_NAMES as readonly string[]).includes(field.name),
  );
  return entry ? parseAspect(effectiveValue(entry, params, entry.name)) : null;
}

/**
 * 节点框此刻该用的宽高比：量到的成片画幅优先，其次是所选比例，都没有就 16:9。
 * @param input.measured 成片实际的宽 / 高；还没量到为 null
 * @param input.selected 参数面板里所选比例对应的宽 / 高；没有为 null
 * @returns 宽 / 高
 */
export function resolveNodeAspect(input: {
  measured: number | null;
  selected: number | null;
}): number {
  return input.measured ?? input.selected ?? DEFAULT_NODE_ASPECT;
}
