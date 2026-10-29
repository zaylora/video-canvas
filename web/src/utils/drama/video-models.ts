import type { Capabilities, ModelInfo, ParamField } from "@/api/model/type";
import { ratioParam } from "@/utils/canvas/node-frame";
import { matchOption } from "@/utils/tasks/capabilities";

/** 短剧分镜单段时长的合法范围与缺省（契约 §10 的 max_seconds） */
export const STORYBOARD_SECONDS = { min: 4, max: 60, fallback: 15 } as const;

/**
 * 视频模型能不能用于短剧片段：片段靠 @ 资产当参考图，所以必须收参考图，
 * 并且至少有一种收图的生成方式（图生视频或全能参考）。不能用的给出原因，界面置灰。
 */
export function videoModelSupport(info: ModelInfo): { ok: boolean; reason: string | null } {
  const caps = info.capabilities;
  const image = caps.refs?.image;
  if (!image?.on || image.max <= 0) {
    return { ok: false, reason: "不支持参考图，片段里的 @ 资产会失效" };
  }
  const ops = caps.ops ?? [];
  if (!ops.includes("i2v") && !ops.includes("omni")) {
    return { ok: false, reason: "只支持文生视频，没有图生 / 参考图方式" };
  }
  return { ok: true, reason: null };
}

/** 时长参数；模型没有为 undefined */
export const durationField = (caps: Capabilities | undefined): ParamField | undefined =>
  caps?.params?.duration;

const numericOptions = (field: ParamField): number[] =>
  (field.options ?? []).map(Number).filter((n) => Number.isFinite(n));

/** 模型单段时长上限（秒）；没有时长参数为 null */
export function modelMaxSeconds(caps: Capabilities | undefined): number | null {
  const field = durationField(caps);
  if (!field) return null;
  if (field.type === "number") return field.max ?? null;
  const options = numericOptions(field);
  return options.length > 0 ? Math.max(...options) : null;
}

/** 生成分镜时给后端的 max_seconds：取模型时长上限，夹在 4–60，没有就用缺省 15 */
export function storyboardMaxSeconds(info: ModelInfo | undefined): number {
  const max = modelMaxSeconds(info?.capabilities);
  if (max === null) return STORYBOARD_SECONDS.fallback;
  return Math.min(STORYBOARD_SECONDS.max, Math.max(STORYBOARD_SECONDS.min, Math.floor(max)));
}

/**
 * 把片段时长适配到模型的时长参数：数字参数夹在 min–max；枚举取不小于它的最近选项，都比它小就取最大。
 * adjusted 表示和片段原时长不一样，提交时提示用户。
 */
export function fitDuration(
  field: ParamField,
  seconds: number,
): { value: number; adjusted: boolean } {
  let value = seconds;
  if (field.type === "number") {
    if (field.min !== undefined) value = Math.max(field.min, value);
    if (field.max !== undefined) value = Math.min(field.max, value);
  } else {
    const options = numericOptions(field).sort((a, b) => a - b);
    if (options.length > 0) value = options.find((option) => option >= seconds) ?? options.at(-1)!;
  }
  return { value, adjusted: value !== seconds };
}

/** 模型的一行摘要：「参考图 ≤9 · 参考音频 · ≤15s」 */
export function modelSummary(info: ModelInfo): string {
  const caps = info.capabilities;
  const parts: string[] = [];
  if (caps.refs?.image?.on) parts.push(`参考图 ≤${caps.refs.image.max}`);
  if (caps.refs?.audio?.on) parts.push("参考音频");
  const max = modelMaxSeconds(caps);
  if (max !== null) parts.push(`≤${max}s`);
  return parts.join(" · ");
}

/**
 * 提交时的参数：时长跟片段（按模型适配）、比例跟短剧（选项里有才覆盖，否则按模型默认）、
 * 其余用户在顶栏选的（如分辨率）。模型没有的参数不乱加。
 */
export function segmentParams(
  caps: Capabilities | undefined,
  options: { seconds: number; ratio: string; overrides: Record<string, unknown> },
): Record<string, unknown> {
  const params: Record<string, unknown> = {};
  if (!caps) return params;
  const duration = durationField(caps);
  if (duration) params.duration = fitDuration(duration, options.seconds).value;
  const ratio = ratioParam(caps);
  if (ratio && options.ratio) {
    const hit = matchOption(ratio, options.ratio);
    if (hit !== undefined) params[ratio.name] = hit;
  }
  for (const [name, value] of Object.entries(options.overrides)) {
    const field = caps.params?.[name];
    if (!field || name === "duration" || name === ratio?.name) continue;
    if (field.type === "enum") {
      const hit = matchOption(field, value);
      if (hit !== undefined) params[name] = hit;
    } else if (value !== undefined) {
      params[name] = value;
    }
  }
  return params;
}

/**
 * 默认选哪个视频模型：存过选择且仍可用就沿用，否则取第一个可用的；一个都没有为 undefined。
 */
export function pickVideoModel(
  models: readonly ModelInfo[],
  savedKey: string,
): ModelInfo | undefined {
  const usable = models.filter((info) => videoModelSupport(info).ok);
  return usable.find((info) => info.key === savedKey) ?? usable[0];
}
