import type { DramaJob, DramaStep, EpisodeSummary } from "@/api/drama/type";

import { isActiveJob } from "./jobs";

/** 工作台的五道工序，同时也是顶部标签和地址栏 ?area= 的取值 */
export type WorkspaceStage = "overview" | "script" | "assets" | "studio" | "final";

/** 顶部工序标签，顺序即制作顺序（快捷键 1–5 按此排列） */
export const STAGES: readonly { key: WorkspaceStage; label: string }[] = [
  { key: "overview", label: "总览" },
  { key: "script", label: "剧本" },
  { key: "assets", label: "角色与场景" },
  { key: "studio", label: "片场" },
  { key: "final", label: "成片" },
];

/**
 * 解析地址栏 ?area=：没有或写错回到总览。
 * 旧版的 episodes（分集视频）并入了片场，旧链接仍能落到对应位置。
 */
export function parseStage(raw: string | null): WorkspaceStage {
  if (raw === "episodes") return "studio";
  return STAGES.find((stage) => stage.key === raw)?.key ?? "overview";
}

/** 分集栏小进度条一格的状态：off 未开始，run 进行中，part 做了一部分（或已过期），on 完成 */
export type PipState = "off" | "run" | "part" | "on";

/** 分集栏每集下面的四格：剧本 / 拆解 / 分镜 / 成片 */
export function episodePips(episode: EpisodeSummary): [PipState, PipState, PipState, PipState] {
  const script: PipState =
    episode.scriptStatus === "generating" ? "run" : episode.scriptStatus === "ready" ? "on" : "off";
  const breakdown: PipState = episode.locked ? "on" : "off";
  let storyboard: PipState = "off";
  if (episode.storyboardStatus === "generating") storyboard = "run";
  else if (episode.storyboardStatus === "ready") {
    storyboard =
      episode.segmentCount > 0 && episode.doneSegmentCount >= episode.segmentCount ? "on" : "part";
  }
  const final: PipState = episode.final ? (episode.finalStale ? "part" : "on") : "off";
  return [script, breakdown, storyboard, final];
}

/** 总览矩阵里一格：none 表示前置工序没做完，不可点；done / total 只有片段列有 */
export type MatrixCell = {
  state: "done" | "part" | "run" | "todo" | "none";
  done?: number;
  total?: number;
};

/** 总览矩阵里一集的五格 */
export type MatrixRow = {
  script: MatrixCell;
  breakdown: MatrixCell;
  storyboard: MatrixCell;
  segments: MatrixCell;
  final: MatrixCell;
};

/** 一集在剧本 / 拆解 / 分镜 / 片段 / 成片五道工序上的状态：前一道没做完，后面的格子不可点 */
export function episodeMatrix(episode: EpisodeSummary): MatrixRow {
  const scriptReady = episode.scriptStatus === "ready";
  const script: MatrixCell = {
    state: scriptReady ? "done" : episode.scriptStatus === "generating" ? "run" : "todo",
  };
  const breakdown: MatrixCell = episode.locked
    ? { state: "done" }
    : { state: scriptReady ? "todo" : "none" };

  let storyboard: MatrixCell = { state: episode.locked ? "todo" : "none" };
  if (episode.storyboardStatus === "ready") storyboard = { state: "done" };
  else if (episode.storyboardStatus === "generating") storyboard = { state: "run" };

  const hasSegments = episode.storyboardStatus === "ready" && episode.segmentCount > 0;
  const segments: MatrixCell = hasSegments
    ? {
        state: episode.doneSegmentCount >= episode.segmentCount ? "done" : "part",
        done: episode.doneSegmentCount,
        total: episode.segmentCount,
      }
    : { state: "none" };

  let final: MatrixCell = { state: hasSegments ? "todo" : "none" };
  if (episode.final) final = { state: episode.finalStale ? "part" : "done" };

  return { script, breakdown, storyboard, segments, final };
}

/** 总览的整体完成度 */
export type OverallProgress = {
  /** 0–100，五道工序各占 1/5，片段按已出视频的比例折算 */
  percent: number;
  /** 剧本已写好的集数 */
  scripts: number;
  /** 已拆解（锁定）的集数 */
  broken: number;
  /** 已有成片的集数 */
  finals: number;
};

/** 全剧完成度：把每集五道工序的完成情况折成一个百分比 */
export function overallProgress(episodes: readonly EpisodeSummary[]): OverallProgress {
  if (episodes.length === 0) return { percent: 0, scripts: 0, broken: 0, finals: 0 };
  let units = 0;
  let scripts = 0;
  let broken = 0;
  let finals = 0;
  for (const episode of episodes) {
    if (episode.scriptStatus === "ready") {
      scripts += 1;
      units += 1;
    }
    if (episode.locked) {
      broken += 1;
      units += 1;
    }
    if (episode.storyboardStatus === "ready") {
      units += 1;
      if (episode.segmentCount > 0) {
        units += Math.min(1, episode.doneSegmentCount / episode.segmentCount);
      }
    }
    if (episode.final) {
      finals += 1;
      units += 1;
    }
  }
  return { percent: Math.round((units / (episodes.length * 5)) * 100), scripts, broken, finals };
}

/** 「继续制作」的落点：哪道工序的哪一集 */
export type ResumeTarget = { stage: WorkspaceStage; ep: number };

/**
 * 总览主按钮「继续制作」去哪：按集号从前往后找第一个没做完的集，指向它缺的那道工序。
 * 剧本没写好或还没拆解 → 剧本；分镜没有或片段没出齐 → 片场；片段齐了但没有成片（或已过期）→ 成片。
 * 全部做完（或没有分集）返回 null。
 */
export function resumeTarget(episodes: readonly EpisodeSummary[]): ResumeTarget | null {
  for (const episode of episodes) {
    if (episode.scriptStatus !== "ready" || !episode.locked) {
      return { stage: "script", ep: episode.no };
    }
    const segmentsDone =
      episode.storyboardStatus === "ready" &&
      episode.segmentCount > 0 &&
      episode.doneSegmentCount >= episode.segmentCount;
    if (!segmentsDone) return { stage: "studio", ep: episode.no };
    if (!episode.final || episode.finalStale) return { stage: "final", ep: episode.no };
  }
  return null;
}

/**
 * 命令面板里选中一集后去哪：分镜已经有了（或正在生成）直接进片场，
 * 否则去剧本页——那里能写剧本、拆解资产，是这一集的起点。
 */
export function jumpStageFor(episode: EpisodeSummary): WorkspaceStage {
  return episode.storyboardStatus === "ready" || episode.storyboardStatus === "generating"
    ? "studio"
    : "script";
}

/** 工序标签右上角的提醒点：run 有任务在跑（闪烁），todo 有待办（静态） */
export type StageHints = Partial<Record<WorkspaceStage, "run" | "todo">>;

/**
 * 哪些工序标签要亮提醒点：
 * 剧本——集在生成或研读 / 大纲 / 分集剧本任务在跑为 run，有集还没剧本为 todo；
 * 角色与场景——拆解 / 写资产提示词在跑为 run，有剧本写好却没拆解的集为 todo；
 * 片场——分镜在生成或分镜任务在跑为 run；成片——合成任务在跑为 run。
 * 同一道工序 run 优先于 todo。
 */
export function stageHints(
  episodes: readonly EpisodeSummary[],
  jobs: readonly DramaJob[],
): StageHints {
  const running = (...steps: DramaStep[]) =>
    jobs.some((job) => isActiveJob(job) && steps.includes(job.step));
  const hints: StageHints = {};

  if (
    episodes.some((ep) => ep.scriptStatus === "generating") ||
    running("analyze", "outline", "scripts")
  ) {
    hints.script = "run";
  } else if (episodes.some((ep) => ep.scriptStatus === "empty" || ep.scriptStatus === "failed")) {
    hints.script = "todo";
  }

  if (running("breakdown", "asset_prompts")) hints.assets = "run";
  else if (episodes.some((ep) => ep.scriptStatus === "ready" && !ep.locked)) hints.assets = "todo";

  if (episodes.some((ep) => ep.storyboardStatus === "generating") || running("storyboard")) {
    hints.studio = "run";
  }
  if (running("compose")) hints.final = "run";
  return hints;
}
