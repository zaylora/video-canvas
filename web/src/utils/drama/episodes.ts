import type { DramaJob, DramaRatio, DramaStatus, EpisodeSummary } from "@/api/drama/type";

import { busyReason } from "./jobs";

/** 一集的剧本能不能手改：锁定集（已拆解资产）只读；生成中也先别改，免得和模型输出互相覆盖 */
export function episodeEditState(episode: Pick<EpisodeSummary, "locked" | "scriptStatus">): {
  editable: boolean;
  reason: "locked" | "generating" | null;
} {
  if (episode.locked) return { editable: false, reason: "locked" };
  if (episode.scriptStatus === "generating") return { editable: false, reason: "generating" };
  return { editable: true, reason: null };
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
  return episodes.map((ep) => {
    const reason = ep.locked ? "已拆解" : ep.scriptStatus !== "ready" ? "还没有剧本" : null;
    return { no: ep.no, title: ep.title, disabled: reason !== null, reason };
  });
}

/** 对话框打开时的默认选择：第一个可用的集 */
export function defaultBreakdownSelection(episodes: readonly EpisodeSummary[]): number[] {
  const first = breakdownOptions(episodes).find((option) => !option.disabled);
  return first ? [first.no] : [];
}

/** 已有锁定集（资产已按当前比例生成）时比例不能再改 */
export function ratioFixed(episodes: readonly Pick<EpisodeSummary, "locked">[]): boolean {
  return episodes.some((ep) => ep.locked);
}

/** 提交按钮：比例必选、至少一集 */
export function breakdownSubmit(
  ratio: DramaRatio,
  selected: readonly number[],
): { ok: boolean; label: string } {
  if (!ratio) return { ok: false, label: "先选比例" };
  if (selected.length === 0) return { ok: false, label: "至少选一集" };
  return { ok: true, label: "开始拆解" };
}
