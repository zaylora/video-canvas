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

import type { DramaAssetType, RefAsset } from "./segment-assets";
import { durationField, fitDuration, segmentParams } from "./video-models";

/** 提示词里一个引用在这次请求里的去向（参考素材条据此画） */
export type RefView = {
  id: string;
  label: string;
  type: DramaAssetType | null;
  /** image 带了参考图；text 只用文字（没图 / 被截断）；dead 节点已被删 */
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
  /** 可以直接交给 generate 的 input */
  input: Record<string, unknown>;
  /** 输入名 -> 该标红的原因；空表示可以提交 */
  errors: Record<string, string>;
  /** 非阻断提示：声音未生效、第 N 张起未带入、时长被调整…… */
  notices: string[];
  refs: RefView[];
  /** input.images 里的张数与模型上限，参考素材条的「n/上限」 */
  imageCount: number;
  imageMax: number;
  /** 引用了已被删除的节点，存下来的名字 */
  deadLabels: string[];
};

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
      /** 主图未就绪时仍保留声音关系，视频必须先通过对应关系校验。 */
      character: characterRefsOfNode(
        asset.id,
        {
          assetId: asset.src ? asset.assetId : undefined,
          status: asset.generating ? "running" : undefined,
          character: asset.character,
          voiceTaskId: asset.voiceTaskId,
        },
        asset.voiceTaskStatus,
      ),
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
 * 视频的生成方式（契约 §13）：
 * 有参考图 → 全能参考，没有全能参考就退回图生视频；一张参考图都没有 → 文生视频，
 * 只有全能参考的模型照旧用 omni（由校验提示至少要 1 个参考素材）。
 */
export function pickVideoOp(
  caps: Capabilities | undefined,
  state: { hasImages: boolean },
): GenerationOp | undefined {
  const ops = caps?.ops ?? [];
  if (state.hasImages && ops.includes("omni")) return "omni";
  if (!state.hasImages && ops.includes("t2v")) return "t2v";
  if (state.hasImages && ops.includes("i2v")) return "i2v";
  return ops.includes("omni") ? "omni" : ops[0];
}

/**
 * 组装片段「立即生成」的 input：
 * 提示词里 `@[名](节点id)` 解析成资产（角色卡展开主图 + 补充图为参考图、声音为参考音频），
 * 优先保留主图与声音，补充图按剩余容量添加；必要素材或声音未就绪时阻止提交。
 * 时长跟片段、比例跟短剧、分辨率等取用户选的。
 */
export function planSegmentInput(args: {
  caps: Capabilities | undefined;
  prompt: string;
  seconds: number;
  ratio: string;
  overrides: Record<string, unknown>;
  assets: readonly RefAsset[];
}): SegmentPlan {
  const { caps, prompt } = args;
  const linked = linkRefs(prompt, args.assets);
  const op = pickVideoOp(caps, {
    hasImages: hasImageRefs({}, { images: imageLinksOf(linked.links) }),
  });
  const params = {
    prompt,
    ...segmentParams(caps, { seconds: args.seconds, ratio: args.ratio, overrides: args.overrides }),
  };
  const bindings = resolveBindings(caps, op, linked.links, params);
  const built = buildTaskInput(caps, params, bindings, op, linked.links);

  const notices = [...bindings.notices];
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
    refs: refViews(linked, bindings),
    imageCount: Array.isArray(images) ? images.length : 0,
    imageMax: caps?.refs?.image?.max ?? 0,
    deadLabels: deadLabelsOf(linked),
  };
}
