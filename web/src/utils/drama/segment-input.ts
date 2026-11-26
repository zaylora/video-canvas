import type { Capabilities, GenerationOp } from "@/api/model/type";
import { characterRefsOfNode } from "@/utils/characters/node-character";
import { parsePrompt } from "@/utils/canvas/prompt-tokens";
import {
  buildTaskInput,
  hasImageRefs,
  imageLinksOf,
  resolveBindings,
  toAssetNumber,
  type Bindings,
  type IncomingLink,
} from "@/utils/tasks/capabilities";

import { boardLine, promptLang, type BoardRef } from "./board";
import type { DramaAssetType, RefAsset } from "./segment-assets";
import { durationField, fitDuration, segmentParams } from "./video-models";

/** 提示词里一个引用在这次请求里的去向（参考素材条据此画） */
export type RefView = {
  id: string;
  label: string;
  type: DramaAssetType | null;
  /** image 带了参考图；audio 是参考音频；text 只用文字（没图 / 被截断）；dead 节点已被删 */
  state: "image" | "audio" | "text" | "dead";
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
  /** input.audios 里的条数与模型上限；角色卡音色就体现在这里 */
  audioCount: number;
  audioMax: number;
  /** 引用了已被删除的节点，存下来的名字 */
  deadLabels: string[];
  /** 故事板这次带没带、为什么 */
  board: BoardPlan;
  /** 带入故事板时加在提示词最前面的说明（语言跟正文一致，片场原样预览）；没带入为空串 */
  boardLine: string;
};

/**
 * 故事板在这次请求里的去向：image 带入（第一张参考图）/ stale 带入但已过期 / off 用户本次关掉 /
 * none 没有可用的故事板 / unsupported 模型不是全能参考，不带
 */
export type BoardPlan = {
  state: "image" | "stale" | "off" | "none" | "unsupported";
  /** 没带入的原因，带入时为空串 */
  reason: string;
};

/** 故事板的虚拟连线 id：不和任何画布节点重名 */
const BOARD_LINK_ID = "__board__";

/** 这根连线能带来的参考音频：音频节点自己，或角色卡绑定的声音（旁白常常只有声音没有主图） */
function audioIdOf(link: IncomingLink): string | undefined {
  return link.sourceKind === "audio" ? link.assetId : (link.character?.voiceAssetId ?? undefined);
}

/**
 * 决定故事板带不带：只有全能参考（omni）才带，图生视频会把第一张图当首帧、文生视频不收图。
 * 带的时候生成方式固定为 omni，故事板作为普通图片连线排在所有引用前面，于是固定是图片1。
 */
function planBoard(
  caps: Capabilities | undefined,
  board: BoardRef | null | undefined,
  off: boolean,
): BoardPlan {
  if (!board) return { state: "none", reason: "" };
  if (off) return { state: "off", reason: "本次已关掉故事板" };
  const ops = caps?.ops ?? [];
  const images = caps?.refs?.image?.on ? caps.refs.image.max : 0;
  if (!ops.includes("omni") || images <= 0) {
    const reason =
      ops.includes("i2v") && images > 0
        ? "当前模型把第一张图当首帧，故事板未带入"
        : "当前模型不收参考图，故事板未带入";
    return { state: "unsupported", reason };
  }
  return { state: board.stale ? "stale" : "image", reason: "" };
}

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

/**
 * 提示词里 @ 的资产转成连线（同画布连线的规则）：失效的引用不带；故事板拿它带参考图
 * @param prompt 提示词（含 `@[名](节点id)`）
 * @param assets 画布上的资产
 */
export const assetLinks = (prompt: string, assets: readonly RefAsset[]): IncomingLink[] =>
  linkRefs(prompt, assets).links;

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
    // 没有素材地址的音频仍会挂在 bindings 上，但 buildTaskInput 会跳过它，这里按能提交的算
    const audioCount = bindings.audios.filter(
      (link) => link.sourceId === id && toAssetNumber(link.assetId) !== null,
    ).length;
    if (asset.kind === "audio")
      return {
        id,
        label,
        type: asset.type,
        // 没有素材地址时只当文字，不假装带了参考音频
        state: audioCount > 0 ? "audio" : "text",
        imageCount: 0,
        truncated: false,
        voice: "none",
      };
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
  /** 片段采用的故事板；没有为 null */
  board?: BoardRef | null;
  /** 用户本次关掉了故事板 */
  boardOff?: boolean;
}): SegmentPlan {
  const { caps, prompt } = args;
  const linked = linkRefs(prompt, args.assets);
  const board = planBoard(caps, args.board, !!args.boardOff);
  const withBoard = (board.state === "image" || board.state === "stale") && args.board;
  const links: IncomingLink[] = withBoard
    ? [
        {
          edgeId: BOARD_LINK_ID,
          sourceId: BOARD_LINK_ID,
          sourceKind: "image",
          sourceLabel: "故事板",
          targetHandle: null,
          assetId: withBoard.assetId,
        },
        ...linked.links,
      ]
    : linked.links;
  const hasImages = hasImageRefs({}, { images: imageLinksOf(linked.links) });
  const readyAudio = linked.links.filter((link) => toAssetNumber(audioIdOf(link)) !== null);
  // 只 @ 了音频（没有参考图）时也要走全能参考，文生视频没有参考素材口
  const audioOnly = !hasImages && readyAudio.length > 0;
  const op =
    withBoard || (audioOnly && caps?.ops?.includes("omni"))
      ? "omni"
      : pickVideoOp(caps, { hasImages });
  // 说明行和正文同一种语言：提交的是英文转写稿（H3）就用英文
  const line = withBoard ? boardLine(withBoard.shotCount, args.ratio, promptLang(prompt)) : "";
  const params = {
    prompt: line ? `${line}\n${prompt}` : prompt,
    ...segmentParams(caps, { seconds: args.seconds, ratio: args.ratio, overrides: args.overrides }),
  };
  const bindings = resolveBindings(caps, op, links, params);
  const built = buildTaskInput(caps, params, bindings, op, links);
  if (withBoard && built.errors.images) {
    built.errors.images = `${built.errors.images}（含故事板 1 张，可以在参考素材里关掉故事板）`;
  }
  // 模型不收参考音频时音频会被静默丢掉，旁白就变成没有音色的普通文字
  const sentAudios = Array.isArray(built.input.audios) ? built.input.audios : [];
  const narrator = readyAudio.filter((link) => sentAudios.includes(toAssetNumber(audioIdOf(link))));
  if (caps && narrator.length < readyAudio.length)
    built.errors.audios ??=
      "当前模型或生成方式不接收参考音频，旁白音色带不进去；请换支持参考音频的模型，或去掉音频引用";
  // 声音生成关掉时参考音频没有意义，只拦真正带进这次请求的音频
  const sound = caps?.params?.generate_audio;
  const soundValue = sound?.open ? built.input.generate_audio : sound?.default;
  if (narrator.length > 0 && sound && (soundValue === false || soundValue === "false"))
    built.errors.generate_audio ??= "使用参考音频时请开启声音生成";

  const notices = [...bindings.notices];
  if (board.state === "stale") notices.push("故事板已过期，原稿在出图之后改过");
  const duration = durationField(caps);
  if (duration) {
    const fit = fitDuration(duration, args.seconds);
    if (fit.adjusted) notices.push(`片段 ${args.seconds}s 超出模型范围，按 ${fit.value}s 生成`);
  }
  const images = built.input.images;
  const audios = built.input.audios;
  return {
    op,
    input: built.input,
    errors: built.errors,
    notices,
    refs: refViews(linked, bindings),
    imageCount: Array.isArray(images) ? images.length : 0,
    imageMax: caps?.refs?.image?.max ?? 0,
    audioCount: Array.isArray(audios) ? audios.length : 0,
    audioMax: caps?.refs?.audio?.max ?? 0,
    deadLabels: deadLabelsOf(linked),
    board,
    boardLine: line,
  };
}
