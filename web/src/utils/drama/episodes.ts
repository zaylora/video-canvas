import type { DramaJob, DramaPhase, DramaStatus, EpisodeSummary, UnitKind } from "@/api/drama/type";

import { busyReason } from "./jobs";
import { unitWord } from "./start-settings";

/** 一集的剧本能不能手改：锁定集（已拆解资产）只读；生成中也先别改，免得和模型输出互相覆盖 */
export function episodeEditState(episode: Pick<EpisodeSummary, "locked" | "scriptStatus">): {
  editable: boolean;
  reason: "locked" | "generating" | null;
} {
  if (episode.locked) return { editable: false, reason: "locked" };
  if (episode.scriptStatus === "generating") return { editable: false, reason: "generating" };
  return { editable: true, reason: null };
}

/**
 * 剧本页要不要显示「这一集还没有剧本」空状态：有对应原文、还没写出剧本（未生成或失败、正文为空）时显示。
 * 手动新增的集没有对应原文，生成不出剧本，直接给空白稿纸手写。
 * @param episode 集摘要
 * @param scriptMd 已读到的正文
 */
export function showEmptyScript(
  episode: Pick<EpisodeSummary, "scriptStatus" | "unitFrom">,
  scriptMd: string,
): boolean {
  if (episode.scriptStatus !== "empty" && episode.scriptStatus !== "failed") return false;
  if (!episode.unitFrom) return false;
  return scriptMd.trim() === "";
}

/** 目录里一集的状态图标：锁定 > 生成中 > 待生成（还没有剧本或失败）> 正常 */
export function tocState(
  episode: Pick<EpisodeSummary, "locked" | "scriptStatus">,
): "locked" | "generating" | "pending" | "ready" {
  if (episode.locked) return "locked";
  if (episode.scriptStatus === "generating") return "generating";
  if (episode.scriptStatus === "empty" || episode.scriptStatus === "failed") return "pending";
  return "ready";
}

/** 底部「下一步」浮条的状态 */
export type NextStep =
  | { kind: "none" }
  | { kind: "scripts"; episodes: number[]; disabledReason: string | null }
  | { kind: "breakdown"; episodes: number[]; disabledReason: string | null };

/**
 * 下一步该做什么：有集没剧本 -> 生成分集剧本；剧本齐了还有没拆解的 -> 资产拆解；都做完或还在研读 -> 不显示。
 * 有进行中的流水线任务（或还有集在生成剧本）时禁用，并写明原因。
 */
export function nextStep(
  drama: { status: DramaStatus; episodes: readonly EpisodeSummary[] },
  jobs: readonly DramaJob[],
): NextStep {
  if (drama.episodes.length === 0) return { kind: "none" };
  const disabledReason =
    busyReason(jobs) ??
    (drama.episodes.some((ep) => ep.scriptStatus === "generating") ? "有集正在生成剧本" : null);

  const unscripted = drama.episodes
    .filter((ep) => ep.scriptStatus === "empty" || ep.scriptStatus === "failed")
    .map((ep) => ep.no);
  if (unscripted.length > 0) return { kind: "scripts", episodes: unscripted, disabledReason };

  const toBreak = drama.episodes
    .filter((ep) => ep.scriptStatus === "ready" && !ep.locked)
    .map((ep) => ep.no);
  if (toBreak.length > 0) return { kind: "breakdown", episodes: toBreak, disabledReason };
  return { kind: "none" };
}

/** 用户任务一次最多选多少集（接口契约 §6） */
export const MAX_JOB_EPISODES = 200;

/** 这一集是否已规划出来（有标题）；规划只出目录，剧本由用户选集再写 */
export const isPlanned = (episode: Pick<EpisodeSummary, "title">): boolean =>
  episode.title.trim() !== "";

/** 只保留已规划的集：选集弹窗不列还没规划出来的 */
export const plannedEpisodes = <T extends Pick<EpisodeSummary, "title">>(
  episodes: readonly T[],
): T[] => episodes.filter(isPlanned);

/** 资产拆解对话框里一集的选项 */
export type BreakdownOption = {
  no: number;
  title: string;
  disabled: boolean;
  /** 禁用原因，可选时为 null */
  reason: "已拆解" | "还没有剧本" | null;
};

/** 已锁定的、没有剧本的（含剧本还在生成）都禁用 */
export function breakdownOptions(episodes: readonly EpisodeSummary[]): BreakdownOption[] {
  return plannedEpisodes(episodes).map((ep) => {
    const reason = ep.locked ? "已拆解" : ep.scriptStatus !== "ready" ? "还没有剧本" : null;
    return { no: ep.no, title: ep.title, disabled: reason !== null, reason };
  });
}

/** 对话框打开时的默认选择：第一个可用的集 */
export function defaultBreakdownSelection(episodes: readonly EpisodeSummary[]): number[] {
  const first = breakdownOptions(episodes).find((option) => !option.disabled);
  return first ? [first.no] : [];
}

/** 提交按钮：至少一集；比例以项目为准，建剧时已定，这里不再选 */
export function breakdownSubmit(selected: readonly number[]): { ok: boolean; label: string } {
  if (selected.length === 0) return { ok: false, label: "至少选一集" };
  if (selected.length > MAX_JOB_EPISODES) {
    return { ok: false, label: `一次最多 ${MAX_JOB_EPISODES} 集` };
  }
  return { ok: true, label: "开始拆解" };
}

/** 生成分集剧本对话框里一集的选项 */
export type ScriptOption = {
  no: number;
  title: string;
  disabled: boolean;
  /** 不能选的原因，可选时为 null */
  reason: "已拆解" | "生成中" | null;
  /** 已有剧本：可以重新生成，但会覆盖 */
  hasScript: boolean;
};

/** 已拆解（剧本锁定）的、正在生成的不能选；已有剧本的可以选来重新生成 */
export function scriptOptions(episodes: readonly EpisodeSummary[]): ScriptOption[] {
  return plannedEpisodes(episodes).map((ep) => {
    const reason = ep.locked ? "已拆解" : ep.scriptStatus === "generating" ? "生成中" : null;
    return {
      no: ep.no,
      title: ep.title,
      disabled: reason !== null,
      reason,
      hasScript: ep.scriptStatus === "ready",
    };
  });
}

/** 对话框打开时的默认选择：还没有剧本（没有或失败）的集，和原来一键生成的范围一致 */
export function defaultScriptSelection(episodes: readonly EpisodeSummary[]): number[] {
  return scriptOptions(episodes)
    .filter((option) => !option.disabled && !option.hasScript)
    .map((option) => option.no)
    .slice(0, MAX_JOB_EPISODES);
}

/**
 * 提交按钮：至少一集；按钮写明生成几集；overwrite 是选中里已有剧本、会被覆盖的集数。
 * @param selected 选中的集号
 * @param episodes 全部集
 */
export function scriptSubmit(
  selected: readonly number[],
  episodes: readonly EpisodeSummary[],
): { ok: boolean; label: string; overwrite: number } {
  const overwrite = scriptOptions(episodes).filter(
    (option) => option.hasScript && selected.includes(option.no),
  ).length;
  if (selected.length === 0) return { ok: false, label: "至少选一集", overwrite };
  if (selected.length > MAX_JOB_EPISODES) {
    return { ok: false, label: `一次最多 ${MAX_JOB_EPISODES} 集`, overwrite };
  }
  return { ok: true, label: `生成 ${selected.length} 集剧本`, overwrite };
}

/**
 * 删掉第 deleted 集后，当前看的那一集该落到哪个集号（后面的集已经往前补位）：
 * 删的就是当前集——落到补上来的下一集，删的是最后一集就落到前一集；
 * 删的在当前集前面——集号减 1，看的还是同一集；在后面——不变。一集都不剩时回到 1。
 * @param current 删之前看的集号
 * @param deleted 删掉的集号
 * @param remaining 删完还剩几集
 */
export function episodeAfterDelete(current: number, deleted: number, remaining: number): number {
  if (remaining === 0) return 1;
  if (current === deleted) return Math.min(deleted, remaining);
  return current > deleted ? current - 1 : current;
}

/**
 * 删一集前的确认框文案：写明删的是哪一集；有片段的说明片段会一起删（生成过的视频素材保留）；
 * 不是最后一集的说明后面的集号会往前补。
 * @param episode 要删的集
 * @param total 删之前一共几集
 */
export function deleteEpisodeConfirm(
  episode: Pick<EpisodeSummary, "no" | "title" | "segmentCount">,
  total: number,
): { title: string; description: string } {
  const name = episode.title.trim()
    ? `第 ${episode.no} 集「${episode.title.trim()}」`
    : `第 ${episode.no} 集`;
  const parts = ["这一集的剧本会一起删除，不能撤销。"];
  if ((episode.segmentCount ?? 0) > 0) {
    parts.push(`它的 ${episode.segmentCount} 个片段也会删除，已生成的视频仍保留在素材里。`);
  }
  if (episode.no < total) {
    const from = episode.no + 1;
    parts.push(
      from === total
        ? `第 ${total} 集会变成第 ${total - 1} 集。`
        : `第 ${from}–${total} 集会依次变成第 ${from - 1}–${total - 1} 集。`,
    );
  }
  return { title: `删除${name}？`, description: parts.join("") };
}

/** 总览进度表每页几集 */
export const EPISODES_PER_PAGE = 10;

/**
 * 总览进度表的一页：页码越界收回到有效范围，没有集时也算一页。
 * @returns 这一页的集、实际页码、总页数，以及「第 from–to 集」用的序号（按在列表里的位置，从 1 起）
 */
export function episodePage<T>(episodes: readonly T[], page: number) {
  const pages = Math.max(1, Math.ceil(episodes.length / EPISODES_PER_PAGE));
  const current = Math.min(Math.max(1, page), pages);
  const start = (current - 1) * EPISODES_PER_PAGE;
  const items = episodes.slice(start, start + EPISODES_PER_PAGE);
  return { items, page: current, pages, from: start + 1, to: start + items.length };
}

/** 某一集在进度表的第几页；找不到回第 1 页 */
export function pageOfEpisode(episodes: readonly EpisodeSummary[], no: number): number {
  const index = episodes.findIndex((episode) => episode.no === no);
  return index < 0 ? 1 : Math.floor(index / EPISODES_PER_PAGE) + 1;
}

/**
 * 「按范围选：第 a–b 集」：返回范围内所有能选的集号。a > b 自动对调，越界自然截断
 * @param options 选集弹窗里的选项（写剧本或拆解）
 * @param from 起始集号
 * @param to 结束集号
 */
export function selectByRange(
  options: readonly { no: number; disabled: boolean }[],
  from: number,
  to: number,
): number[] {
  const lo = Math.min(from, to);
  const hi = Math.max(from, to);
  return options
    .filter((option) => !option.disabled && option.no >= lo && option.no <= hi)
    .map((option) => option.no);
}

/**
 * 写剧本的衔接缺口：选中的集里，上一集既没有剧本、也不在本次选择中的集号。
 * 这些集会用上一集的梗概和结尾悬念衔接（与后端同口径，估价接口会带回来，这里先本地给出）
 * @param selected 选中的集号
 * @param episodes 全部集
 */
export function gapBefore(
  selected: readonly number[],
  episodes: readonly Pick<EpisodeSummary, "no" | "scriptStatus" | "locked">[],
): number[] {
  const chosen = new Set(selected);
  const hasScript = (no: number) => {
    const hit = episodes.find((episode) => episode.no === no);
    return !!hit && (hit.locked || hit.scriptStatus === "ready");
  };
  return [...selected]
    .sort((a, b) => a - b)
    .filter((no) => no > 1 && !chosen.has(no - 1) && !hasScript(no - 1));
}

/**
 * 增删集被拦的原因：规划中（后端 67019）、或有用户任务在跑；可以操作返回 null
 * @param phase 规划阶段
 * @param jobs 任务卡
 */
export function editBlockReason(phase: DramaPhase, jobs: readonly DramaJob[]): string | null {
  if (phase === "running") return "正在规划，完成或暂停后再操作";
  return busyReason(jobs);
}

/**
 * 一集对应的原文位置：「第 3 章」或「第 3–5 章」；没有关联单元（null）返回 null
 * @param from 起始单元号
 * @param to 结束单元号
 * @param kind 单元种类（章 / 集 / 场 / 段）
 */
export function sourceRangeLabel(
  from: number | null,
  to: number | null,
  kind: UnitKind,
): string | null {
  if (!from) return null;
  const word = unitWord(kind);
  return to && to !== from ? `第 ${from}–${to} ${word}` : `第 ${from} ${word}`;
}
