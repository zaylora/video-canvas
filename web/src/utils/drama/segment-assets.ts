import type { CanvasGraphDto } from "@/api/canvas/type";
import type { NodeCharacter, NodeKind, NodeMediaType } from "@/types";
import { normalizeLegacyCharacter } from "@/utils/characters/node-character";

/** 片段提示词里能 @ 的资产类型（短剧画布上拆解出来的三类） */
export type DramaAssetType = "character" | "scene" | "prop";

export const DRAMA_ASSET_TYPES: { key: DramaAssetType; label: string }[] = [
  { key: "character", label: "角色" },
  { key: "scene", label: "场景" },
  { key: "prop", label: "道具" },
];

/** 短剧画布上的一个资产节点（读自画布，不另存） */
export type RefAsset = {
  /** 画布节点 id，也是提示词里 `@[名](节点id)` 的 id */
  id: string;
  type: DramaAssetType;
  name: string;
  /** 造型 / 变体名，可空串 */
  look: string;
  /** 节点标题，chip 上显示它（如「林川 · 童年」） */
  label: string;
  /** 出现集号；[] = 全集 */
  /** 出现的集号；空数组 = 全集；null = 未分类 */
  episodes: number[] | null;
  kind: NodeKind;
  /** 当前素材地址；还没出图为 null */
  src: string | null;
  mediaType?: NodeMediaType;
  /** 素材记录 id（字符串） */
  assetId?: string;
  /** 角色卡节点自带的角色数据（kind 为 character 时有）；主图就是本资产的 assetId / src */
  character?: NodeCharacter;
  /** 角色声音生成任务 id */
  voiceTaskId?: string;
  /** 声音任务状态，由读取资产的 hook 补齐 */
  voiceTaskStatus?: string;
  /** 节点正在生成：这时 assetId 还指着上一版，不能当参考图 */
  generating: boolean;
};

const DRAMA_TYPES = new Set<string>(["character", "scene", "prop"]);

/** 从画布图谱里取出短剧资产（有 drama.type 的非组节点）；画布没有就是空数组 */
export function assetsFromGraph(nodes: CanvasGraphDto["nodes"] | null | undefined): RefAsset[] {
  const out: RefAsset[] = [];
  for (const node of nodes ?? []) {
    if (node.type === "group") continue;
    // 旧画布里引用全局角色卡的节点先规整成内嵌数据（保留名字，其余为空）
    const data = normalizeLegacyCharacter(node).data;
    const type = data.drama?.type;
    if (!type || !DRAMA_TYPES.has(type) || data.kind === "director") continue;
    const name = data.drama?.name?.trim() || data.label;
    const look = data.drama?.look?.trim() ?? "";
    const src = data.src && !data.src.startsWith("blob:") ? data.src : null;
    out.push({
      id: node.id,
      type,
      name,
      look,
      label: data.label?.trim() || (look ? `${name} · ${look}` : name),
      episodes:
        data.drama?.episodes === null
          ? null
          : [...new Set(data.drama?.episodes ?? [])].sort((a, b) => a - b),
      kind: data.kind,
      src,
      mediaType: data.mediaType,
      assetId: data.assetId,
      character: data.character,
      ...(data.voiceTaskId ? { voiceTaskId: data.voiceTaskId } : {}),
      generating: data.status === "running",
    });
  }
  return out;
}

/** 这个资产出现在第 no 集（全集资产算出现在每一集，未分类的不算出现在任何一集） */
export const inEpisode = (asset: Pick<RefAsset, "episodes">, no: number): boolean =>
  asset.episodes !== null && (asset.episodes.length === 0 || asset.episodes.includes(no));

/** 按是否属于本集拆成两组，各自保持原顺序（@ 候选本集优先、资产库「本集 / 全集」切换都用它） */
export function splitByEpisode(
  assets: readonly RefAsset[],
  no: number,
): { here: RefAsset[]; other: RefAsset[] } {
  return {
    here: assets.filter((asset) => inEpisode(asset, no)),
    other: assets.filter((asset) => !inEpisode(asset, no)),
  };
}

/** 按角色 / 场景 / 道具分组，空组略过 */
export function groupByType(
  assets: readonly RefAsset[],
): { type: DramaAssetType; label: string; items: RefAsset[] }[] {
  return DRAMA_ASSET_TYPES.map(({ key, label }) => ({
    type: key,
    label,
    items: assets.filter((asset) => asset.type === key),
  })).filter((group) => group.items.length > 0);
}

/** 本集的角色 / 场景 / 道具数（分集卡片上显示） */
export function countForEpisode(
  assets: readonly RefAsset[],
  no: number,
): Record<DramaAssetType, number> {
  const counts: Record<DramaAssetType, number> = { character: 0, scene: 0, prop: 0 };
  for (const asset of assets) if (inEpisode(asset, no)) counts[asset.type] += 1;
  return counts;
}

/**
 * 删节点前的提示：要删的这些资产里，被多少个片段的提示词引用了、分别是哪几个资产被引用。
 * 一个片段引用了好几个也只算一个；没有被引用返回 null（只提示，不阻止）。
 */
export function referenceWarning(
  doomed: readonly { id: string; label: string }[],
  prompts: readonly string[],
): { segments: number; names: string[] } | null {
  const names = doomed.filter((node) => countReferences(node.id, prompts) > 0).map((n) => n.label);
  if (names.length === 0) return null;
  const segments = prompts.filter((prompt) =>
    doomed.some((node) => prompt.includes(`](${node.id})`) && prompt.includes("@[")),
  ).length;
  return { segments, names };
}

/** 有几个片段的提示词引用了这个节点（删节点前提示用）；prompts 是全部片段的提示词 */
export function countReferences(nodeId: string, prompts: readonly string[]): number {
  const needle = `](${nodeId})`;
  return prompts.filter((prompt) => prompt.includes("@[") && prompt.includes(needle)).length;
}
