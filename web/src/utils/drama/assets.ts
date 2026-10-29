import type { CanvasNodeData, FlowNode, NodeDrama } from "@/types";

/** 资产区的类型 tab（角色 / 场景 / 道具来自拆解，其余是画布里随手建的素材） */
export type AssetType = "character" | "scene" | "prop" | "text" | "image" | "video" | "audio";

export const ASSET_TYPES: { key: AssetType; label: string }[] = [
  { key: "character", label: "角色" },
  { key: "scene", label: "场景" },
  { key: "prop", label: "道具" },
  { key: "text", label: "文本" },
  { key: "image", label: "图片" },
  { key: "video", label: "视频" },
  { key: "audio", label: "音频" },
];

/** 拆解产物（会批量生成图片的三类） */
export const GENERATED_TYPES: AssetType[] = ["character", "scene", "prop"];

/** 集数筛选：全集 = 所有资产（含未分类）；第 N 集 = 全集资产加出现在 N 的；未分类 = 没有 drama 字段的 */
export type EpisodeFilter = { kind: "all" } | { kind: "episode"; no: number } | { kind: "none" };

/** 卡片状态 */
export type AssetStatus = "idle" | "analyzing" | "running" | "done" | "error";

/** 列表里的一项资产：读自画布节点，不另存 */
export type AssetItem = {
  /** 节点 id */
  id: string;
  type: AssetType;
  name: string;
  /** 造型 / 变体名，可空串 */
  look: string;
  /** 节点标题 */
  label: string;
  /** 出现集数；null = 未分类，[] = 全集 */
  episodes: number[] | null;
  status: AssetStatus;
  data: CanvasNodeData;
};

/** 节点属于哪个类型 tab；导演台不是资产，返回 null */
export function assetTypeOf(data: CanvasNodeData): AssetType | null {
  if (data.kind === "director") return null;
  if (data.drama?.type) return data.drama.type;
  switch (data.kind) {
    case "character":
      return "character";
    case "script":
      return "text";
    case "image":
    case "video":
    case "audio":
      return data.kind;
    default:
      return null;
  }
}

/** 节点的出现集数：没有 drama 字段是未分类（null），空数组是全集，其余升序去重 */
export function assetEpisodesOf(data: CanvasNodeData): number[] | null {
  if (!data.drama) return null;
  return [...new Set(data.drama.episodes ?? [])].sort((a, b) => a - b);
}

/** 这个出现集数是否落在筛选里 */
export function matchesFilter(episodes: number[] | null, filter: EpisodeFilter): boolean {
  if (filter.kind === "all") return true;
  if (filter.kind === "none") return episodes === null;
  if (episodes === null) return false;
  return episodes.length === 0 || episodes.includes(filter.no);
}

/** 状态判断时由外部给的上下文 */
export type StatusContext = {
  /** 「资产提示词」任务还在跑 */
  analyzing?: boolean;
};

/** 卡片状态：失败 / 生成中优先，其次看有没有素材；还在写提示词的空节点是「分析设定中」 */
export function assetStatusOf(data: CanvasNodeData, ctx: StatusContext = {}): AssetStatus {
  if (data.status === "running") return "running";
  if (data.status === "error") return "error";
  // 角色卡节点的主图就是节点自己的 src，和图片节点同一个判断
  const hasContent =
    data.kind === "script" ? !!data.text?.trim() : !!data.src && !data.src.startsWith("blob:");
  if (hasContent) return "done";
  const hasPrompt = typeof data.params?.prompt === "string" && data.params.prompt.trim() !== "";
  if (ctx.analyzing && !hasPrompt && !data.prompt) return "analyzing";
  return "idle";
}

/** 节点列表转资产项；组、导演台略过 */
export function toAssetItems(nodes: readonly FlowNode[], ctx: StatusContext = {}): AssetItem[] {
  const items: AssetItem[] = [];
  for (const node of nodes) {
    if (node.type === "group") continue;
    const data = node.data as CanvasNodeData;
    const type = assetTypeOf(data);
    if (!type) continue;
    items.push({
      id: node.id,
      type,
      name: data.drama?.name?.trim() || data.label,
      look: data.drama?.look?.trim() ?? "",
      label: data.label,
      episodes: assetEpisodesOf(data),
      status: assetStatusOf(data, ctx),
      data,
    });
  }
  return items;
}

/** 某类型在某筛选下的资产 */
export function filterAssets(
  items: readonly AssetItem[],
  type: AssetType,
  filter: EpisodeFilter,
): AssetItem[] {
  return items.filter((item) => item.type === type && matchesFilter(item.episodes, filter));
}

/** 各类型 tab 上的计数：与当前集数筛选一致 */
export function countByType(
  items: readonly AssetItem[],
  filter: EpisodeFilter,
): Record<AssetType, number> {
  const counts = Object.fromEntries(ASSET_TYPES.map((t) => [t.key, 0])) as Record<
    AssetType,
    number
  >;
  for (const item of items) {
    if (matchesFilter(item.episodes, filter)) counts[item.type] += 1;
  }
  return counts;
}

/** 可批量生成的：角色 / 场景 / 道具里已分类、待生成或失败的 */
export function pendingGenerate(items: readonly AssetItem[], type: AssetType): AssetItem[] {
  if (!GENERATED_TYPES.includes(type)) return [];
  return items.filter(
    (item) =>
      item.type === type &&
      item.episodes !== null &&
      (item.status === "idle" || item.status === "error"),
  );
}

/** 勾选 / 取消勾选一集，保持升序去重 */
export function toggleEpisode(episodes: readonly number[], no: number): number[] {
  const set = new Set(episodes);
  if (set.has(no)) set.delete(no);
  else set.add(no);
  return [...set].sort((a, b) => a - b);
}

/** 改节点的出现集数：保留 drama 里已有的其他字段；未分类的节点同时归类 */
export function setEpisodes(
  data: CanvasNodeData,
  episodes: number[],
): Pick<CanvasNodeData, "drama"> {
  const next: NodeDrama = { ...(data.drama ?? {}), episodes };
  return { drama: next };
}

/** 新建节点的默认短剧元数据：全集；角色节点同时标成角色；已有的不覆盖（返回同一个对象） */
export function defaultDramaForNew(data: CanvasNodeData): NodeDrama {
  if (data.drama) return data.drama;
  return data.kind === "character" ? { type: "character", episodes: [] } : { episodes: [] };
}

/** 出现集数的展示：未分类 / 全集 / 把连续的集号合并成「第 1–3、5 集」 */
export function episodesLabel(episodes: readonly number[] | null): string {
  if (episodes === null) return "未分类";
  if (episodes.length === 0) return "全集";
  const sorted = [...new Set(episodes)].sort((a, b) => a - b);
  const parts: string[] = [];
  let start = sorted[0];
  let prev = start;
  const flush = () => parts.push(start === prev ? String(start) : `${start}–${prev}`);
  for (const no of sorted.slice(1)) {
    if (no === prev + 1) {
      prev = no;
      continue;
    }
    flush();
    start = prev = no;
  }
  flush();
  return `第 ${parts.join("、")} 集`;
}

/** 类型 tab 里「新建」对应的画布节点种类：角色是空的角色卡节点，场景 / 道具 / 图片都是图片节点 */
export function newNodeKindOf(
  type: AssetType,
): "character" | "image" | "video" | "script" | "audio" {
  switch (type) {
    case "character":
      return "character";
    case "scene":
    case "prop":
    case "image":
      return "image";
    case "video":
      return "video";
    case "text":
      return "script";
    case "audio":
      return "audio";
  }
}

/**
 * 在某类型 tab、某集数筛选下新建节点时，写进节点的短剧元数据：
 * 未分类筛选下建的就是未分类（null）；其余默认全集，场景 / 道具带上类型，第 N 集筛选下归到那一集。
 */
export function dramaForNewAsset(type: AssetType, filter: EpisodeFilter): NodeDrama | null {
  if (filter.kind === "none") return null;
  const episodes = filter.kind === "episode" ? [filter.no] : [];
  return type === "scene" || type === "prop" || type === "character"
    ? { type, episodes }
    : { episodes };
}
