import type { Capabilities, GenerationOp } from "@/api/model/type";
import { characterRefsOfNode } from "@/utils/characters/node-character";
import { parsePrompt } from "@/utils/canvas/prompt-tokens";
import {
  buildTaskInput,
  hasImageRefs,
  imageLinksOf,
  resolveBindings,
  type Bindings,
  type IncomingLink,
} from "@/utils/tasks/capabilities";
import { buildFreshInput } from "@/utils/tasks/fresh-input";

import type { DramaAssetType, RefAsset } from "./segment-assets";
import { durationField, fitDuration, segmentParams } from "./video-models";

/** 提示词里一个引用在这次请求里的去向（参考素材条据此画） */
export type RefView = {
  id: string;
  label: string;
  type: DramaAssetType | null;
  /** image 带了参考图；text 只用文字（没图 / 被截断 / 首帧模式）；dead 节点已被删 */
  state: "image" | "text" | "dead";
  /** 这个资产在 input.images 里占了几张 */
  imageCount: number;
  /** 超出参考图上限，被截掉了一部分 */
  truncated: boolean;
  /** 角色声音：none 没有 / used 带入参考音频 / ignored 模型不收音频 / dropped 超上限被截掉 */
  voice: "none" | "used" | "ignored" | "dropped";
};

export type SegmentPlan = {
  /** 生成方式；模型能力没到时为 undefined */
  op: GenerationOp | undefined;
  /** 可以直接交给 generate / keyframe 的 input */
  input: Record<string, unknown>;
  /** 输入名 -> 该标红的原因；空表示可以提交 */
  errors: Record<string, string>;
  /** 非阻断提示：声音未生效、第 N 张起未带入、时长被调整…… */
  notices: string[];
  refs: RefView[];
  /** input.images 里的张数（含首帧）与模型上限，参考素材条的「n/上限」 */
  imageCount: number;
  imageMax: number;
  /** 本次带了首帧（i2v，或全能参考里放在参考图最前） */
  keyframe: boolean;
  /** 引用了已被删除的节点，存下来的名字 */
  deadLabels: string[];
};

/** 首帧图的提示词前缀：让图片模型画「开场第一帧」而不是整段剧情 */
export const KEYFRAME_PREFIX = "为下面这个视频片段生成首帧（开场第一帧的静态画面）：";

type Cited = { id: string; label: string; asset: RefAsset | undefined };

/** 提示词里引用的资产，按第一次出现的顺序去重；找不到节点的记下写入时的名字 */
function citedRefs(prompt: string, assets: readonly RefAsset[]): Cited[] {
  const byId = new Map(assets.map((asset) => [asset.id, asset]));
  const seen = new Set<string>();
  const out: Cited[] = [];
  for (const seg of parsePrompt(prompt)) {
    if (seg.type !== "ref" || seen.has(seg.id)) continue;
    seen.add(seg.id);
    out.push({ id: seg.id, label: seg.label, asset: byId.get(seg.id) });
  }
  return out;
}

/** 资产变成一根「上游连线」：和画布节点接进来的连线同一个形状，复用 resolveBindings */
function toLink(asset: RefAsset): IncomingLink {
  const link: IncomingLink = {
    edgeId: asset.id,
    sourceId: asset.id,
    sourceKind: asset.kind,
    sourceLabel: asset.label,
    targetHandle: null,
  };
  if (asset.kind === "character") {
    return {
      ...link,
      // 角色卡的内容就在资产自己的数据里；没有主图（或正在生成）时为 null：只用文字，不算失效
      character: characterRefsOfNode(asset.id, {
        assetId: asset.src ? asset.assetId : undefined,
        status: asset.generating ? "running" : undefined,
        character: asset.character,
      }),
    };
  }
  // 正在生成的节点 assetId 还指着上一版，不当参考图
  return { ...link, assetId: asset.src && !asset.generating ? asset.assetId : undefined };
}

/** 引用已经不可用：节点没了，或角色卡节点缺少角色数据（异常数据；空节点没有形象不算失效） */
const isDead = (asset: RefAsset | undefined): boolean =>
  !asset || (asset.kind === "character" && !asset.character);

type Linked = { links: IncomingLink[]; cited: Cited[] };

function linkRefs(prompt: string, assets: readonly RefAsset[]): Linked {
  const cited = citedRefs(prompt, assets);
  const links = cited.flatMap(({ asset }) => (asset && !isDead(asset) ? [toLink(asset)] : []));
  return { links, cited };
}

const deadLabelsOf = (linked: Linked): string[] =>
  linked.cited.filter(({ asset }) => isDead(asset)).map(({ label }) => label);

/** 参考素材条的每一项：按 bindings 看这个资产实际带了几张图、声音去哪了 */
function refViews(linked: Linked, bindings: Bindings): RefView[] {
  const countOf = (list: IncomingLink[], id: string) =>
    list.filter((link) => link.sourceId === id).length;
  return linked.cited.map(({ id, label, asset }) => {
    if (!asset || isDead(asset)) {
      return {
        id,
        label,
        type: asset?.type ?? null,
        state: "dead",
        imageCount: 0,
        truncated: false,
        voice: "none",
      };
    }
    const imageCount = countOf(bindings.images, id);
    const droppedImage = bindings.dropped.some(
      (link) => link.sourceId === id && link.fromCharacter?.part !== "voice",
    );
    const droppedVoice = bindings.dropped.some(
      (link) => link.sourceId === id && link.fromCharacter?.part === "voice",
    );
    const voice: RefView["voice"] =
      countOf(bindings.audios, id) > 0
        ? "used"
        : countOf(bindings.ignored, id) > 0
          ? "ignored"
          : droppedVoice
            ? "dropped"
            : "none";
    return {
      id,
      label,
      type: asset.type,
      state: imageCount > 0 ? "image" : "text",
      imageCount,
      truncated: droppedImage,
      voice,
    };
  });
}

/**
 * 视频的生成方式（契约 §13）：有首帧且模型支持 i2v → i2v；
 * 有参考图 → 全能参考，没有全能参考就退回图生视频；一张参考图都没有 → 文生视频，
 * 只有全能参考的模型照旧用 omni（由校验提示至少要 1 个参考素材）。
 */
export function pickVideoOp(
  caps: Capabilities | undefined,
  state: { keyframe: boolean; hasImages: boolean },
): GenerationOp | undefined {
  const ops = caps?.ops ?? [];
  if (state.keyframe && ops.includes("i2v")) return "i2v";
  if (state.hasImages && ops.includes("omni")) return "omni";
  if (!state.hasImages && ops.includes("t2v")) return "t2v";
  if (state.hasImages && ops.includes("i2v")) return "i2v";
  return ops.includes("omni") ? "omni" : ops[0];
}

/**
 * 组装片段「立即生成」的 input：
 * 提示词里 `@[名](节点id)` 解析成资产（角色卡展开主图 + 补充图为参考图、声音为参考音频），
 * 按所选模型的参考图 / 音频上限截断并给出提示；模型不收音频时只用图片并提示声音未生效；
 * 有首帧且模型支持 i2v 时走图生视频（参考图只放首帧，@ 的资产只用文字）。
 * 时长跟片段、比例跟短剧、分辨率等取用户选的。
 */
export function planSegmentInput(args: {
  caps: Capabilities | undefined;
  prompt: string;
  seconds: number;
  ratio: string;
  overrides: Record<string, unknown>;
  assets: readonly RefAsset[];
  /** 首帧图的素材 id；没有为 null */
  keyframeAssetId: string | null;
}): SegmentPlan {
  const { caps, prompt, keyframeAssetId } = args;
  const linked = linkRefs(prompt, args.assets);
  const hasKeyframe = keyframeAssetId !== null;
  const keyLink: IncomingLink | null = hasKeyframe
    ? {
        edgeId: "keyframe",
        sourceId: "keyframe",
        sourceKind: "image",
        sourceLabel: "首帧",
        targetHandle: null,
        assetId: keyframeAssetId,
      }
    : null;
  const withKey = keyLink ? [keyLink, ...linked.links] : linked.links;
  const op = pickVideoOp(caps, {
    keyframe: hasKeyframe,
    hasImages: hasImageRefs({}, { images: imageLinksOf(withKey) }),
  });
  const keyframeOnly = op === "i2v" && keyLink !== null;
  // 首帧图生视频：参考图只放首帧，别的资产只用文字
  const bindings = resolveBindings(caps, op, keyframeOnly ? [keyLink] : withKey);
  const params = {
    prompt,
    ...segmentParams(caps, { seconds: args.seconds, ratio: args.ratio, overrides: args.overrides }),
  };
  const built = buildTaskInput(caps, params, bindings, op, linked.links);

  const notices = [...bindings.notices];
  if (keyframeOnly) notices.unshift("有首帧：只带首帧图生成，@ 的资产只用文字");
  const duration = durationField(caps);
  if (duration) {
    const fit = fitDuration(duration, args.seconds);
    if (fit.adjusted) notices.push(`片段 ${args.seconds}s 超出模型范围，按 ${fit.value}s 生成`);
  }
  const images = built.input.images;
  return {
    op,
    input: built.input,
    errors: built.errors,
    notices,
    // 首帧模式下 @ 的资产都只用文字：用空绑定算
    refs: refViews(linked, keyframeOnly ? resolveBindings(caps, op, []) : bindings),
    imageCount: Array.isArray(images) ? images.length : 0,
    imageMax: caps?.refs?.image?.max ?? 0,
    keyframe: hasKeyframe && Array.isArray(images) && images.length > 0,
    deadLabels: deadLabelsOf(linked),
  };
}

/**
 * 组装「生成首帧」的 input：图片模型，引用的角色 / 场景 / 道具带成参考图（图生图），
 * 没有参考图就文生图；比例跟短剧。提示词前面加一句说明，让模型画开场第一帧。
 * 走和画布一致的 buildFreshInput，保证角色卡展开、截断规则相同。
 */
export function planKeyframeInput(args: {
  caps: Capabilities | undefined;
  prompt: string;
  ratio: string;
  assets: readonly RefAsset[];
}) {
  const linked = linkRefs(args.prompt, args.assets);
  const params = {
    prompt: `${KEYFRAME_PREFIX}${args.prompt}`,
    ...segmentParams(args.caps, { seconds: 0, ratio: args.ratio, overrides: {} }),
  };
  const fresh = buildFreshInput(args.caps, params, linked.links);
  return { ...fresh, deadLabels: deadLabelsOf(linked) };
}
