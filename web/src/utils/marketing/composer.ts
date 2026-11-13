import type {
  AngleOption,
  MarketingAngle,
  MarketingLanguage,
  MarketingPlatform,
  PlatformOption,
  RoundSettings,
} from "@/api/marketing/type";
import type { Capabilities, ModelInfo, ParamField } from "@/api/model/type";
import { ratioParam } from "@/utils/canvas/node-frame";
import { matchOption } from "@/utils/tasks/capabilities";

/** 营销生成框里用户编辑的设置（不含上传的素材） */
export interface ComposerDraft {
  /** 投放平台 */
  platform: MarketingPlatform;
  /** 旁白语言 */
  language: MarketingLanguage;
  /** 选中的创意角度；空 = 自动 */
  angles: MarketingAngle[];
  /** 无参考时自动出几份（1–4） */
  autoCount: number;
  /** 有参考时改编几份（0–3）；跟拍固定 1 份 */
  adaptCount: number;
  /** 画幅 */
  aspectRatio: string;
  /** 分辨率 */
  resolution: string;
  /** 时长（秒） */
  duration: number;
  /** 写脚本的文本模型 key；空串 = 后端按能力自动选 */
  textModel: string;
  /** 描述 */
  description: string;
}

/** 视频参数：画幅、分辨率、时长 */
export interface VideoParams {
  /** 画幅 */
  aspectRatio: string;
  /** 分辨率 */
  resolution: string;
  /** 时长（秒） */
  duration: number;
}

/** 无参考时角度最多选几个 */
export const MAX_ANGLES = 4;

/** 有参考时改编最多几份 */
export const MAX_ADAPT = 3;

/** 默认时长上限偏好（秒）：切模型时优先取不超过它的最大时长 */
const PREFERRED_MAX_SECONDS = 15;

/** 平台没给或模型不支持时的画幅 */
const FALLBACK_RATIO = "9:16";

const MB = 1024 * 1024;

/**
 * 一次写脚本会出几份脚本。
 * 有参考：跟拍固定 1 份 + 改编（选了角度按角度数，没选按改编份数，改编 0 份时只有跟拍）；
 * 无参考：选了角度按角度数，否则按自动份数。
 */
export function planCount(input: {
  hasRef: boolean;
  angles: readonly string[];
  autoCount: number;
  adaptCount: number;
}): number {
  const { hasRef, angles, autoCount, adaptCount } = input;
  if (!hasRef) return angles.length > 0 ? angles.length : autoCount;
  if (adaptCount <= 0) return 1;
  return 1 + (angles.length > 0 ? Math.min(angles.length, adaptCount) : adaptCount);
}

/**
 * 多选角度：点已选的取消，没选的在没满 max 时加入；取消到空就是「自动」。
 * @param angles 已选角度
 * @param key 点的角度
 * @param max 最多几个
 */
export function toggleAngle<T extends string>(angles: readonly T[], key: T, max: number): T[] {
  if (angles.includes(key)) return angles.filter((item) => item !== key);
  if (angles.length >= max) return [...angles];
  return [...angles, key];
}

/** 角度按钮上的文字 */
export function angleLabel(draft: ComposerDraft, hasRef: boolean, options: AngleOption[]): string {
  if (hasRef) return `跟拍 + 改编 ×${draft.adaptCount}`;
  if (draft.angles.length === 0) return `自动 ×${draft.autoCount}`;
  if (draft.angles.length === 1) {
    return options.find((item) => item.key === draft.angles[0])?.name ?? draft.angles[0];
  }
  return `${draft.angles.length} 个角度`;
}

const numericOptions = (field: ParamField | undefined): number[] => {
  if (!field) return [];
  if (field.type === "number") {
    const min = field.min ?? 0;
    const max = field.max ?? min;
    const step = Math.max(1, field.step ?? 1);
    const all: number[] = [];
    for (let n = min; n <= max && all.length < 40; n += step) all.push(n);
    if (all.length <= 8) return all;
    const picked = new Set([all[0], all[all.length - 1]]);
    for (let i = 1; i < 6; i += 1) picked.add(all[Math.round((i * (all.length - 1)) / 6)]);
    return [...picked].sort((a, b) => a - b);
  }
  return (field.options ?? []).map(Number).filter((n) => Number.isFinite(n));
};

/** 模型支持的画幅 */
export function ratioOptions(caps: Capabilities | undefined): string[] {
  return (ratioParam(caps)?.options ?? []).map(String);
}

/** 模型支持的分辨率 */
export function resolutionOptions(caps: Capabilities | undefined): string[] {
  return (caps?.params?.resolution?.options ?? []).map(String);
}

/** 模型支持的时长（秒） */
export function durationOptions(caps: Capabilities | undefined): number[] {
  return numericOptions(caps?.params?.duration);
}

/**
 * 切换投放平台：带出该平台的旁白语言和画幅；模型不支持该画幅时退回 9:16，
 * 9:16 也不支持就取模型的第一个画幅。
 * @param platform 选中的平台
 * @param caps 当前视频模型能力
 */
export function pickPlatform(
  platform: PlatformOption,
  caps: Capabilities | undefined,
): Pick<ComposerDraft, "platform" | "language" | "aspectRatio"> {
  const options = ratioOptions(caps);
  const aspectRatio =
    options.length === 0
      ? platform.aspectRatio
      : options.includes(platform.aspectRatio)
        ? platform.aspectRatio
        : options.includes(FALLBACK_RATIO)
          ? FALLBACK_RATIO
          : options[0];
  return { platform: platform.key, language: platform.language, aspectRatio };
}

/**
 * 模型变了（或平台带来新的画幅建议）后整理视频参数：
 * 当前值在新模型里仍可选就保留；否则画幅用建议值，再退回 9:16；
 * 分辨率退回 720p（没有就第一个）；时长取不超过 15 秒的最大值（都超过就取最小）。
 * 模型没有某个参数时原值不动（发送时也不会带上）。
 * @param caps 新模型能力
 * @param current 当前参数
 * @param wantRatio 建议画幅（平台默认画幅）
 */
export function reconcileVideoParams(
  caps: Capabilities | undefined,
  current: VideoParams,
  wantRatio?: string,
): VideoParams {
  const ratios = ratioOptions(caps);
  const resolutions = resolutionOptions(caps);
  const durations = durationOptions(caps);
  const aspectRatio =
    ratios.length === 0 || ratios.includes(current.aspectRatio)
      ? current.aspectRatio
      : wantRatio && ratios.includes(wantRatio)
        ? wantRatio
        : ratios.includes(FALLBACK_RATIO)
          ? FALLBACK_RATIO
          : ratios[0];
  const resolution =
    resolutions.length === 0 || resolutions.includes(current.resolution)
      ? current.resolution
      : (resolutions.find((item) => /720/.test(item)) ?? resolutions[0]);
  const sorted = [...durations].sort((a, b) => a - b);
  const duration =
    sorted.length === 0 || sorted.includes(current.duration)
      ? current.duration
      : ([...sorted].reverse().find((item) => item <= PREFERRED_MAX_SECONDS) ?? sorted[0]);
  return { aspectRatio, resolution, duration };
}

/** 发给后端的视频参数：键名取模型的参数名，模型没有的不发 */
export function buildVideoParams(
  caps: Capabilities | undefined,
  params: VideoParams,
): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  const ratio = ratioParam(caps);
  if (ratio) out[ratio.name] = matchOption(ratio, params.aspectRatio) ?? params.aspectRatio;
  const resolution = caps?.params?.resolution;
  if (resolution) out.resolution = matchOption(resolution, params.resolution) ?? params.resolution;
  const duration = caps?.params?.duration;
  if (duration) out.duration = matchOption(duration, params.duration) ?? params.duration;
  return out;
}

/** 视频模型能不能用来做营销片：要有全能参考（商品图 + 模特图同时参考），且图片参考上限够用 */
export function videoModelSupport(
  info: ModelInfo,
  needImages: number,
): { ok: boolean; reason: string | null } {
  const caps = info.capabilities;
  if (!(caps.ops ?? []).includes("omni")) {
    return { ok: false, reason: "不支持全能参考，不能同时用商品图和模特图" };
  }
  const image = caps.refs?.image;
  if (!image?.on || image.max <= 0) return { ok: false, reason: "不收参考图" };
  if (image.max < needImages) {
    return { ok: false, reason: `图片参考最多 ${image.max} 张，当前需要 ${needImages} 张` };
  }
  return { ok: true, reason: null };
}

/**
 * 参考视频时长上限（秒）：模型能力里没有这个维度（后端目前只有个数和体积），
 * 若后端以后在 refs.video 上补 max_seconds 就按它限制；没有为 null，由后端 68005 兜底。
 */
export function refVideoMaxSeconds(caps: Capabilities | undefined): number | null {
  const value = (caps?.refs?.video as { max_seconds?: number } | undefined)?.max_seconds;
  return typeof value === "number" && value > 0 ? value : null;
}

/**
 * 参考视频能不能用于这个模型：模型不收参考视频、文件体积超限、时长超限都返回原因。
 * @param caps 视频模型能力
 * @param ref 参考视频的时长（秒）与体积（字节），知道哪个给哪个
 * @param label 模型展示名
 * @param maxSeconds 时长上限；不传取能力里的 max_seconds
 */
export function refVideoProblem(
  caps: Capabilities | undefined,
  ref: { durationSec?: number; sizeBytes?: number },
  label: string,
  maxSeconds: number | null = refVideoMaxSeconds(caps),
): string | null {
  const spec = caps?.refs?.video;
  if (!spec?.on || spec.max <= 0) return `${label} 不支持参考视频`;
  if (spec.max_mb > 0 && ref.sizeBytes !== undefined && ref.sizeBytes > spec.max_mb * MB) {
    return `参考视频 ${(ref.sizeBytes / MB).toFixed(0)} MB，超过 ${label} 的上限 ${spec.max_mb} MB`;
  }
  if (maxSeconds && ref.durationSec !== undefined && ref.durationSec > maxSeconds) {
    return `参考视频 ${Math.round(ref.durationSec)} 秒，超过 ${label} 的上限 ${maxSeconds} 秒`;
  }
  return null;
}

/** 能看图的文本模型（写脚本的第一环节要看商品图） */
export function visionModels(models: readonly ModelInfo[]): ModelInfo[] {
  return models.filter((item) => item.capabilities.vision === true);
}

/** 把生成框设置整理成发给后端的轮次设置 */
export function toRoundSettings(input: {
  draft: ComposerDraft;
  hasRef: boolean;
  videoModel: ModelInfo;
}): RoundSettings {
  const { draft, hasRef, videoModel } = input;
  let angles: MarketingAngle[] = draft.angles;
  let autoCount = draft.autoCount;
  if (hasRef) {
    if (draft.adaptCount <= 0) {
      angles = [];
      autoCount = 0;
    } else if (angles.length > 0) {
      angles = angles.slice(0, draft.adaptCount);
    } else {
      autoCount = draft.adaptCount;
    }
  }
  return {
    platform: draft.platform,
    language: draft.language,
    angles,
    autoCount,
    videoModel: videoModel.key,
    textModel: draft.textModel,
    params: buildVideoParams(videoModel.capabilities, {
      aspectRatio: draft.aspectRatio,
      resolution: draft.resolution,
      duration: draft.duration,
    }),
    description: draft.description.trim(),
  };
}

/** 判断能不能发送所需的现状 */
export interface MarketingSendArgs {
  /** 是详情页底部的 dock：商品图固定，不要求再传 */
  dock: boolean;
  /** 商品图张数（dock 时是项目已有的） */
  productCount: number;
  /** 是否带模特图 */
  hasModelImage: boolean;
  /** 有素材还在上传 */
  uploading: boolean;
  /** 有素材上传失败 */
  uploadFailed: boolean;
  /** 参考视频；没有为 null */
  ref: { durationSec?: number; sizeBytes?: number } | null;
  /** 当前视频模型；没有可用模型为 undefined */
  videoModel: ModelInfo | undefined;
  /** 能看图的文本模型清单 */
  visionModels: readonly ModelInfo[];
  /** 写脚本预估积分；还没算出来为 null */
  credits: number | null;
  /** 可用积分；还没拿到为 null */
  available: number | null;
  /** 正在发送 */
  sending: boolean;
  /** dock 所在的项目正在写脚本：等写完再来一轮 */
  scripting?: boolean;
}

/** 发送前的判断结果 */
export interface MarketingSendState {
  /** 能不能发送 */
  canSend: boolean;
  /** 不能发送时要告诉用户的原因；null 表示不需要说 */
  hint: string | null;
  /** 提示语气：warn 可以补救，bad 要处理 */
  tone: "warn" | "bad" | null;
}

const blocked = (hint: string | null, tone: MarketingSendState["tone"]): MarketingSendState => ({
  canSend: false,
  hint,
  tone: hint ? tone : null,
});

/**
 * 发送前的全部判断。原因按用户最该先处理的顺序：
 * 发送中 / 没有视频模型（静默）→ 项目正在写脚本 → 文本模型 → 素材上传中 → 上传失败 → 没有商品图（静默）
 * → 参考视频 → 图片参考张数 → 余额。
 * 余额只在预估和余额都拿到时才拦，拿不到交给后端 40001 兜底。
 */
export function evaluateMarketingSend(
  args: MarketingSendArgs,
  draft: Pick<ComposerDraft, "textModel">,
): MarketingSendState {
  if (args.sending) return blocked(null, null);
  const model = args.videoModel;
  if (!model) return blocked(null, null);

  if (args.scripting) return blocked("这个项目正在写脚本，等它写完再来一轮", "warn");
  if (args.visionModels.length === 0) {
    return blocked("没有可用的看图文本模型，请联系管理员", "bad");
  }
  if (draft.textModel && !args.visionModels.some((item) => item.key === draft.textModel)) {
    return blocked("当前文本模型不能看图，换一个能看图的模型", "bad");
  }
  if (args.uploading) return blocked("素材还在上传", "warn");
  if (args.uploadFailed) return blocked("有素材上传失败，移除后重新上传", "bad");
  if (!args.dock && args.productCount === 0) return blocked(null, null);

  if (args.ref) {
    const problem = refVideoProblem(model.capabilities, args.ref, model.label);
    if (problem) return blocked(problem, "bad");
  }
  const images = args.productCount + (args.hasModelImage ? 1 : 0);
  const imageMax = model.capabilities.refs?.image?.max ?? 0;
  if (images > imageMax) return blocked(`${model.label} 最多收 ${imageMax} 张参考图`, "bad");

  if (args.credits !== null && args.available !== null && args.credits > args.available) {
    return blocked(`余额不足，还差 ${args.credits - args.available} 积分`, "bad");
  }
  return { canSend: true, hint: null, tone: null };
}
