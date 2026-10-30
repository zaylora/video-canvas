import type { AssetDto } from "@/api/asset/type";
import type {
  CanvasNode,
  CanvasNodeData,
  CharacterAssetRef,
  CharacterVoice,
  NodeCharacter,
} from "@/types";
import { takeUploadFile } from "@/utils/canvas/media";
import { newNodeLabel } from "@/utils/canvas/node-label";
import { appendOutput } from "@/utils/canvas/outputs";
import type { CharacterRefs } from "@/utils/tasks/capabilities";

/** 形象（造型）名的默认值 */
export const DEFAULT_LOOK = "基础形象";

/** 补充图最多几张 */
export const MAX_EXTRAS = 4;

/** 角色名、形象名的长度上限（字） */
export const MAX_NAME = 64;

/** 角色设定、生成声音的提示词的长度上限（字） */
export const MAX_TEXT = 500;

/**
 * 空角色卡节点的数据：新建节点时用。形象名默认「基础形象」，没有补充图、没有音色。
 * @param name 角色名，调用方按画布里已有节点编号去重（如「未命名角色 3」）
 */
export function emptyCharacter(name: string): NodeCharacter {
  return { name, look: DEFAULT_LOOK, description: "", extras: [], voice: null };
}

/** 新建角色卡节点的默认名（不带编号） */
export const DEFAULT_ROLE_NAME = "未命名角色";

/**
 * 加号菜单新建角色卡节点时的默认数据：空角色（没有图、没有音色），名字和节点标题同为
 * 「未命名角色」，画布里已有同名节点时从 2 起编号。
 * @param existingLabels 画布里已有的节点标题
 */
export function newCharacterData(existingLabels: Iterable<string>): {
  label: string;
  character: NodeCharacter;
} {
  const label = newNodeLabel(DEFAULT_ROLE_NAME, existingLabels);
  return { label, character: emptyCharacter(label) };
}

/**
 * 节点自带的数据换成绑定用的素材引用（取代旧版「按 characterId 读缓存」）。
 * 没有主图或正在生成时不带旧图；若已绑定声音，保留声音关系，让视频提交明确提示主图未就绪。
 * 没有声音的空节点仍返回 null，补充图要有主图才带。
 * 没有声音时 voiceAssetId 为 null。
 * @param id 节点 id，连线展开出的虚拟连线靠它定位
 * @param data 节点数据
 * @param voiceTaskStatus 声音任务的最新状态；缺少快照时等待对账，成功后等待声音绑定回填
 */
export function characterRefsOfNode(
  id: string,
  data: Pick<CanvasNodeData, "assetId" | "status" | "character" | "voiceTaskId">,
  voiceTaskStatus?: string,
): CharacterRefs | null {
  const character = data.character;
  if (!character) return null;
  const voicePending = Boolean(
    data.voiceTaskId && !["failed", "expired", "canceled"].includes(voiceTaskStatus ?? ""),
  );
  const ready = Boolean(data.assetId && data.status !== "running");
  if (!ready && !character.voice && !voicePending) return null;
  return {
    id,
    name: character.name,
    avatarAssetId: ready ? data.assetId! : "",
    extraAssetIds: ready ? character.extras.map((extra) => extra.assetId) : [],
    voiceAssetId: character.voice?.mode === "audio" ? character.voice.assetId : null,
    ...(voicePending ? { voicePending: true } : {}),
  };
}

/**
 * 节点与 @ 候选里的显示名：「角色名 · 形象名」，形象名为空只显示角色名；
 * 没有角色数据（异常数据）退回节点标题。
 */
export function characterDisplayName(data: Pick<CanvasNodeData, "label" | "character">): string {
  const character = data.character;
  if (!character) return data.label;
  return character.look ? `${character.name} · ${character.look}` : character.name;
}

/** 规整只需要看到的节点数据字段：画布里的节点和后端接口里的节点都满足 */
type LegacyCharacterData = {
  kind?: string;
  label?: string;
  character?: NodeCharacter;
  characterId?: string;
  labelCache?: { name: string; coverAssetId?: string };
  drama?: { name?: string; look?: string };
};

/** 旧版的文本音色：声音现在只有音频，打开画布时规整掉 */
type LegacyTextVoice = { mode: "text"; text: string };

/**
 * 旧版文本音色换成「没有声音 + 生成声音的提示词」：描述不丢，留给用户日后生成声音时用；
 * 已经写过提示词的不覆盖。没有文本音色的原样返回同一个对象。
 */
function dropTextVoice(character: NodeCharacter): NodeCharacter {
  const voice = character.voice as CharacterVoice | LegacyTextVoice | null;
  if (voice?.mode !== "text") return character;
  const text = [...voice.text.trim()].slice(0, MAX_TEXT).join("");
  const keep = !character.voicePrompt?.trim() && text;
  return { ...character, voice: null, ...(keep ? { voicePrompt: text } : {}) };
}

/**
 * 把一个旧版角色卡节点规整成新数据（打开画布、Agent 补丁新建节点都经过它）。
 * 旧节点只存一个全局角色卡 ID（characterId）和缓存的名字（labelCache）；全局角色卡已经下线，
 * 且不迁移（用户决定），所以变成空角色节点：保留位置、连线和名字，其余为空。
 * 短剧里的旧节点名字和造型取 data.drama，已生成的主图（assetId / src）本来就在节点上，保持不动；
 * labelCache.coverAssetId 不会变成主图。已经是新数据的节点原样返回（同一个对象），残留的旧字段去掉。
 * 旧版文本音色同样规整：voice 变成 null，描述搬进 voicePrompt（见 dropTextVoice）。
 * @param node 一个节点
 */
export function normalizeLegacyCharacter<T extends { data: LegacyCharacterData }>(node: T): T {
  const data = node.data;
  if (data.kind !== "character") return node;
  if (data.characterId === undefined && data.labelCache === undefined) {
    if (!data.character) return node;
    const character = dropTextVoice(data.character);
    return character === data.character ? node : ({ ...node, data: { ...data, character } } as T);
  }
  const { characterId: _id, labelCache, ...rest } = data;
  if (data.character) {
    return { ...node, data: { ...rest, character: dropTextVoice(data.character) } } as T;
  }
  const name = data.drama?.name?.trim() || labelCache?.name?.trim() || data.label?.trim();
  const character = emptyCharacter(name || DEFAULT_ROLE_NAME);
  const look = data.drama?.look?.trim();
  if (look) character.look = look;
  return { ...node, data: { ...rest, character } } as T;
}

/** 整批规整，见 normalizeLegacyCharacter */
export function normalizeLegacyCharacters(nodes: CanvasNode[]): CanvasNode[] {
  return nodes.map(normalizeLegacyCharacter);
}

/** 加补充图的结果：成功带回新数据，失败带回原因（界面就地提示） */
export type AddExtraResult = { ok: true; character: NodeCharacter } | { ok: false; reason: string };

/**
 * 给角色加一张补充图：最多 4 张，不能是主图，不能重复。素材必须是图片由调用方（选择框按种类过滤）保证。
 * @param character 当前角色数据
 * @param mainAssetId 主图素材 id，没有主图传空串
 * @param extra 要加的图片
 */
export function addExtra(
  character: NodeCharacter,
  mainAssetId: string,
  extra: CharacterAssetRef,
): AddExtraResult {
  if (character.extras.length >= MAX_EXTRAS) {
    return { ok: false, reason: `补充图最多 ${MAX_EXTRAS} 张` };
  }
  if (extra.assetId === mainAssetId) return { ok: false, reason: "已是主图" };
  if (character.extras.some((item) => item.assetId === extra.assetId)) {
    return { ok: false, reason: "已添加过这张图" };
  }
  return { ok: true, character: { ...character, extras: [...character.extras, extra] } };
}

/**
 * 把选好（素材库）或传好（上传）的一张图写成节点主图：落在节点自己的 assetId / src / mediaType，
 * 同时记进节点的历史版本（原来的主图留在历史里，可以切回去），清掉失败状态和失败原因；
 * 如果同一张图之前是补充图，从补充图里去掉（补充图不含主图）。
 * @param data 节点数据
 * @param asset 图片素材
 */
export function applyMainImage(
  data: CanvasNodeData,
  asset: Pick<AssetDto, "id" | "url">,
): CanvasNodeData {
  const { error: _error, ...rest } = data;
  const character = data.character
    ? {
        ...data.character,
        extras: data.character.extras.filter((extra) => extra.assetId !== asset.id),
      }
    : data.character;
  return {
    ...rest,
    ...appendOutput(data, {
      id: asset.id,
      src: asset.url,
      mediaType: "image",
      assetId: asset.id,
      createdAt: Date.now(),
    }),
    status: "idle",
    ...(character ? { character } : {}),
  };
}

/**
 * 绑定一段音频作为声音（上传、素材库、AI 生成都走这里），会取代之前的声音。
 * @param character 当前角色数据
 * @param asset 音频素材
 * @param origin 来源，只用于展示和排查
 */
export function applyVoiceAudio(
  character: NodeCharacter,
  asset: Pick<AssetDto, "id" | "url">,
  origin: CharacterVoice["origin"],
): NodeCharacter {
  return { ...character, voice: { mode: "audio", assetId: asset.id, src: asset.url, origin } };
}

/** 角色卡上传的校验结果：通过，或一句摆给用户看的原因 */
export type CharacterFileCheck = { ok: true } | { ok: false; reason: string };

/**
 * 角色卡上传前的校验：主图 / 补充图只收图片，音色只收音频；
 * 大小上限和别的种类的提示沿用画布上传（takeUploadFile）。
 * @param file 本地文件
 * @param want 这个入口要的种类
 */
export function takeCharacterFile(file: File, want: "image" | "audio"): CharacterFileCheck {
  const taken = takeUploadFile(file);
  if ("error" in taken) return { ok: false, reason: taken.error };
  if (taken.mediaType !== want) {
    return { ok: false, reason: want === "image" ? "角色形象只能上传图片" : "音色只能上传音频" };
  }
  return { ok: true };
}

/** 选好 / 传好素材后写回节点：要改的字段，和被拒的素材的原因（界面就地提示） */
export type PickedResult = { patch: Partial<CanvasNodeData>; warnings: string[] };

/**
 * 素材库选择或上传完成后，把素材按用途写回节点（节点和详情页共用）：
 * 主图取第一张；补充图一次可以来多张，按顺序加，已是主图 / 重复 / 超过 4 张的跳过并记下原因；
 * 声音绑定成音频。什么都没改时 patch 为空对象，调用方可以不写节点。
 * @param data 节点数据
 * @param assets 选中（或传好）的素材，按选择顺序
 * @param purpose 用途
 * @param origin 音色的来源：素材库选的是 library，本地传的是 upload，AI 生成的是 generated
 */
export function applyPickedAssets(
  data: CanvasNodeData,
  assets: ReadonlyArray<Pick<AssetDto, "id" | "url">>,
  purpose: "main" | "extra" | "voice",
  origin: CharacterVoice["origin"] = "library",
): PickedResult {
  if (assets.length === 0) return { patch: {}, warnings: [] };
  const character = data.character ?? emptyCharacter(data.label);
  if (purpose === "main") {
    return { patch: pickChanged(data, applyMainImage(data, assets[0])), warnings: [] };
  }
  if (purpose === "voice") {
    return { patch: { character: applyVoiceAudio(character, assets[0], origin) }, warnings: [] };
  }
  let next = character;
  const warnings: string[] = [];
  for (const asset of assets) {
    const out = addExtra(next, data.assetId ?? "", { assetId: asset.id, src: asset.url });
    if (out.ok) next = out.character;
    else warnings.push(out.reason);
  }
  return { patch: next === character ? {} : { character: next }, warnings };
}

/** 主图写回只带真正变了的字段，避免把整份节点数据当 patch 塞回去 */
function pickChanged(before: CanvasNodeData, after: CanvasNodeData): Partial<CanvasNodeData> {
  const patch: Partial<CanvasNodeData> = {};
  for (const key of Object.keys(after) as (keyof CanvasNodeData)[]) {
    if (after[key] !== before[key]) (patch as Record<string, unknown>)[key] = after[key];
  }
  if (before.error !== undefined && after.error === undefined) patch.error = undefined;
  return patch;
}

/**
 * 改角色名：只改当前节点（不会同步到同名的其他节点）。节点标题如果还和旧角色名一样（说明没手动改过），
 * 就跟着改成新名字；用户自己起过标题的不动。名字超过上限按字符截断。
 * @param data 节点数据
 * @param name 新的角色名（调用方保证非空）
 */
export function renameCharacter(
  data: CanvasNodeData,
  name: string,
): Pick<CanvasNodeData, "label" | "character"> {
  const next = [...name].slice(0, MAX_NAME).join("");
  const character = data.character ?? emptyCharacter(data.label);
  const followsName = data.label === character.name;
  return { label: followsName ? next : data.label, character: { ...character, name: next } };
}
