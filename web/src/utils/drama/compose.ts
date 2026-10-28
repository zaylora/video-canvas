import type { DramaJob, EpisodeSummary, Segment } from "@/api/drama/type";

import { isActiveJob } from "./jobs";
import { hasVideo } from "./segments";

/** 「合成整集」按钮能不能点；不能点时 reason 就是按钮的说明 */
export type ComposeCheck = { enabled: boolean; reason: string | null };

/**
 * 合成整集的可用性（契约 §14）：至少一个片段有采用视频才可点（否则后端 67010）；
 * 同一部短剧同时只有一个进行中的任务（后端 67004），有就禁用并写明是哪个。
 */
export function composeCheck(
  segments: readonly Segment[],
  busyReason: string | null,
): ComposeCheck {
  if (!segments.some(hasVideo)) {
    return { enabled: false, reason: "至少要有一个片段采用了视频才能合成" };
  }
  if (busyReason) return { enabled: false, reason: busyReason };
  return { enabled: true, reason: null };
}

/** 分集卡片上的判定：没加载片段，只看集摘要里「有采用视频的片段数」，规则同 composeCheck */
export function composeCheckEpisode(
  episode: Pick<EpisodeSummary, "doneSegmentCount">,
  busyReason: string | null,
): ComposeCheck {
  if (episode.doneSegmentCount <= 0) {
    return { enabled: false, reason: "至少要有一个片段采用了视频才能合成" };
  }
  if (busyReason) return { enabled: false, reason: busyReason };
  return { enabled: true, reason: null };
}

/** 没有采用视频的片段（合成时会被跳过）的 seq，按顺序；seq 从 0 起 */
export const missingVideoSeqs = (segments: readonly Segment[]): number[] =>
  [...segments]
    .sort((a, b) => a.seq - b.seq)
    .filter((segment) => !hasVideo(segment))
    .map((segment) => segment.seq);

/** 把 seq 列表写成界面上的序号：`[1, 3]` → 「02、04」 */
export const skippedText = (seqs: readonly number[]): string =>
  seqs.map((seq) => String(seq + 1).padStart(2, "0")).join("、");

/** 一集成片的状态：none 没有；fresh 最新；stale 成片之后片段改过（提示可重新合成） */
export type FinalState = "none" | "fresh" | "stale";

export function finalState(episode: Pick<EpisodeSummary, "final" | "finalStale">): FinalState {
  if (!episode.final) return "none";
  return episode.finalStale ? "stale" : "fresh";
}

/** 这一集进行中（排队 / 运行）的合成任务，没有为 null */
export const activeComposeJob = (jobs: readonly DramaJob[], no: number): DramaJob | null =>
  jobs.find((job) => job.step === "compose" && isActiveJob(job) && job.episodes.includes(no)) ??
  null;

/**
 * 合成成功后要不要提示：跳过了没有采用视频的片段才提示，写明是哪几个；
 * 不是合成任务、没成功、没有跳过都返回 null。
 */
export function composeNotice(job: DramaJob): string | null {
  if (job.step !== "compose" || job.status !== "succeeded") return null;
  const skipped = job.result?.skippedSeqs ?? [];
  if (skipped.length === 0) return null;
  const no = job.episodes[0];
  return `${no ? `第 ${no} 集` : "整集"}已合成，跳过了没有采用视频的片段 ${skippedText(skipped)}`;
}
