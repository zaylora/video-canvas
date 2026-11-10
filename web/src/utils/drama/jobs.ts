import type { Drama, DramaJob, DramaStep } from "@/api/drama/type";

import type { WorkspaceStage } from "./stages";
import { unitWord } from "./start-settings";

/** 排队中或进行中 */
export const isActiveJob = (job: Pick<DramaJob, "status">): boolean =>
  job.status === "queued" || job.status === "running";

const STATUS_RANK: Record<DramaJob["status"], number> = {
  queued: 0,
  running: 1,
  succeeded: 2,
  failed: 2,
  canceled: 2,
};

const byCreatedDesc = (a: DramaJob, b: DramaJob) =>
  a.createdAt < b.createdAt ? 1 : a.createdAt > b.createdAt ? -1 : 0;

/**
 * 并入一条任务快照（WebSocket 推送或 HTTP 对账都走这里）：
 * 同 id 更新；终态不被迟到的非终态快照覆盖，状态不倒退（running 不回 queued），进度不回退。
 * 结果按创建时间新的在前。
 */
export function mergeJob(jobs: readonly DramaJob[], incoming: DramaJob): DramaJob[] {
  const existing = jobs.find((job) => job.id === incoming.id);
  let next = incoming;
  if (existing) {
    if (STATUS_RANK[incoming.status] < STATUS_RANK[existing.status]) {
      next = existing;
    } else if (!isActiveJob(existing) && isActiveJob(incoming)) {
      next = existing;
    } else if (isActiveJob(incoming) && incoming.progress < existing.progress) {
      next = { ...incoming, progress: existing.progress };
    }
  }
  return [...jobs.filter((job) => job.id !== incoming.id), next].sort(byCreatedDesc);
}

/**
 * 用服务端的进行中任务对账本地列表：
 * 服务端有的并入（以服务端为准的最新状态）；本地还显示进行中、服务端已经没有的说明在离线期间结束了，
 * 不知道成败，从列表移出并通过 ended 交给调用方重新拉详情；本地已收到终态的卡片（失败待重试）保留。
 */
export function reconcileJobs(
  local: readonly DramaJob[],
  active: readonly DramaJob[],
): { jobs: DramaJob[]; ended: DramaJob[] } {
  const activeIds = new Set(active.map((job) => job.id));
  const ended = local.filter((job) => isActiveJob(job) && !activeIds.has(job.id));
  const endedIds = new Set(ended.map((job) => job.id));
  let jobs = local.filter((job) => !endedIds.has(job.id));
  for (const job of active) jobs = mergeJob(jobs, job);
  return { jobs, ended };
}

/** 保留全部进行中的任务和最近 keep 条已结束的（新的在前） */
export function pruneFinished(jobs: readonly DramaJob[], keep: number): DramaJob[] {
  const sorted = [...jobs].sort(byCreatedDesc);
  let kept = 0;
  return sorted.filter((job) => {
    if (isActiveJob(job)) return true;
    kept += 1;
    return kept <= keep;
  });
}

const STEP_NAME: Record<DramaStep, string> = {
  analyze: "研读剧本",
  plan: "分集规划",
  outline: "故事大纲与人物",
  scripts: "分集剧本",
  breakdown: "资产拆解",
  asset_prompts: "写资产提示词",
  storyboard: "分镜脚本",
  compose: "合成整集",
};

/** 步骤的短名称 */
export const stepName = (step: DramaStep): string => STEP_NAME[step];

/** 进度卡标题：带上范围，如「资产拆解 · 第 2 集」「分集剧本 · 3 集」 */
export function jobTitle(job: Pick<DramaJob, "step" | "episodes">): string {
  const name = STEP_NAME[job.step];
  if (
    job.step === "analyze" ||
    job.step === "plan" ||
    job.step === "outline" ||
    job.step === "asset_prompts"
  ) {
    return name;
  }
  if (job.episodes.length === 0) return name;
  if (
    job.episodes.length === 1 &&
    (job.step === "breakdown" || job.step === "storyboard" || job.step === "compose")
  ) {
    return `${name} · 第 ${job.episodes[0]} 集`;
  }
  return `${name} · ${job.episodes.length} 集`;
}

/** 这条任务是系统自动发起的研读 / 规划（pipeline 线）；没带 lane 的老任务按步骤判断 */
export function isPipelineJob(job: Pick<DramaJob, "lane" | "step">): boolean {
  return (
    job.lane === "pipeline" ||
    job.step === "analyze" ||
    job.step === "outline" ||
    job.step === "plan"
  );
}

/** 用户发起的任务（user 线），不含自动研读 / 规划 */
export const userJobs = (jobs: readonly DramaJob[]): DramaJob[] =>
  jobs.filter((job) => !isPipelineJob(job));

/**
 * 用户任务进行中时给出「为什么不能再提交 / 改集」，没有返回 null。
 * 自动规划（pipeline 线）与用户任务互不阻塞，不算在内。
 */
export function busyReason(jobs: readonly DramaJob[]): string | null {
  const running = userJobs(jobs).find(isActiveJob);
  if (!running) return null;
  return `有进行中的任务：${jobTitle(running)}`;
}

const TEXT_STEPS: readonly DramaStep[] = ["scripts", "breakdown", "asset_prompts"];

/**
 * 提交某一步会不会撞上进行中的任务（与后端 dramaJobsConflict 同规则，撞上即 67004）：
 * 文本任务（剧本 / 拆解 / 资产提示词）之间互斥；分镜、合成只和同一集的分镜、合成冲突，不同集可以并行。
 * 自动规划不算在内。不冲突返回 null。
 * @param step 要提交的步骤
 * @param episodes 要提交的集号
 */
export function jobConflictReason(
  jobs: readonly DramaJob[],
  step: DramaStep,
  episodes: readonly number[],
): string | null {
  const text = TEXT_STEPS.includes(step);
  const hit = userJobs(jobs).find((job) => {
    if (!isActiveJob(job)) return false;
    if (TEXT_STEPS.includes(job.step) !== text) return false;
    if (text || job.episodes.length === 0) return true;
    return episodes.some((no) => job.episodes.includes(no));
  });
  return hit ? `有进行中的任务：${jobTitle(hit)}` : null;
}

const RUNNING_TEXT: Record<DramaStep, string> = {
  analyze: "正在研读剧本…",
  plan: "正在规划分集…",
  outline: "正在整理故事大纲与人物…",
  scripts: "正在生成分集剧本…",
  breakdown: "正在拆解剧本资产…",
  asset_prompts: "正在写资产提示词…",
  storyboard: "正在生成分镜脚本…",
  compose: "正在合成整集…",
};

/** 进行中的话术，如「正在拆解剧本资产…」 */
export const jobRunningText = (step: DramaStep): string => RUNNING_TEXT[step];

const STEP_STAGE: Record<DramaStep, WorkspaceStage> = {
  analyze: "script",
  plan: "script",
  outline: "script",
  scripts: "script",
  breakdown: "assets",
  asset_prompts: "assets",
  storyboard: "studio",
  compose: "final",
};

/** 任务提交后用户该看哪道工序页 */
export const jobStage = (step: DramaStep): WorkspaceStage => STEP_STAGE[step];

/** 正在给第 no 集生成分镜的任务（排队或进行中）；没有为 undefined */
export function activeStoryboardJob(jobs: readonly DramaJob[], no: number): DramaJob | undefined {
  return jobs.find(
    (job) => job.step === "storyboard" && isActiveJob(job) && job.episodes.includes(no),
  );
}

/** 页面此刻自己在显示哪些任务的进度 */
export type InPlaceView = {
  /** 剧本页正开着整页研读视图 */
  analyzingView: boolean;
  /** 正在看这一集的片场（分镜还没好时那里显示生成进度）；不在片场为 null */
  storyboardEp: number | null;
};

/**
 * 这条任务的进度页面里已经就地显示了，顶部进度条不再重复：
 * 只有研读视图里的研读 / 大纲。分镜任务即使在当前集的片场也进顶部（那里的进度和顶部并存），
 * 这样看自己那集和看别的集一样有取消入口和进度条。只管进行中的，失败照常进顶部（带原因和重试）。
 */
export function coveredInPlace(job: DramaJob, view: InPlaceView): boolean {
  if (!isActiveJob(job)) return false;
  return view.analyzingView && (job.step === "analyze" || job.step === "outline");
}

/** 页内进度条要显示的任务：全部进行中的；没有进行中时，最新一条若失败也显示（带重试） */
export function bannerJobs(jobs: readonly DramaJob[]): DramaJob[] {
  const active = jobs.filter(isActiveJob);
  if (active.length > 0) return active;
  const newest = [...jobs].sort(byCreatedDesc)[0];
  return newest?.status === "failed" ? [newest] : [];
}

/** 规划行的数据来源：只用到的那几个字段，方便测试 */
type PlanningDrama = Pick<
  Drama,
  "phase" | "planCursor" | "rangeEnd" | "pauseReason" | "pauseCode" | "unitKind"
> & { episodes: readonly unknown[] };

/** 规划行上的操作：继续 / 重试 / 改用内置重试 */
export type PlanningAction = "resume" | "retry" | "builtin";

/** JobBar 里「分集规划」那一行的展示内容 */
export type PlanningRowView =
  | {
      /** 研读 / 规划进行中 */
      kind: "running";
      /** 主文案，如「正在规划第 301–312 章」 */
      title: string;
      /** 灰字：当前在做什么、已出多少集目录 */
      detail: string;
      /** 0–100 */
      progress: number;
    }
  | {
      /** 暂停或失败 */
      kind: "paused";
      /** muted 用户主动暂停；error 积分 / 失败等需要处理 */
      tone: "muted" | "error";
      /** 主文案 */
      title: string;
      /** 原因 */
      reason: string;
      /** 可用操作，按显示顺序 */
      actions: PlanningAction[];
    };

/**
 * 规划还要多久（分钟）：按「已规划的单元数 ÷ 已用时间」估出速度，再乘剩余单元数。
 * 还没规划出任何单元（速度未知）或已规划完时返回 null；用时含暂停期间，所以只是粗估
 * @param drama 创建时间、已规划到第几个单元、范围终点
 * @param now 当前时间（毫秒）
 */
export function planningEtaMinutes(
  drama: { createdAt: string; planCursor: number; rangeEnd: number },
  now: number,
): number | null {
  const remaining = drama.rangeEnd - drama.planCursor;
  if (drama.planCursor <= 0 || remaining <= 0) return null;
  const elapsed = now - new Date(drama.createdAt).getTime();
  if (!(elapsed > 0)) return null;
  return Math.max(1, Math.round(((elapsed / drama.planCursor) * remaining) / 60_000));
}

const batchRange = (from: number, to: number, word: string) =>
  from === to ? `第 ${from} ${word}` : `第 ${from}–${to} ${word}`;

/**
 * 自动规划那一行（JobBar 顶部）：done 没有；running 显示研读 / 正在规划第 a–b 章和进度；
 * paused 按暂停原因给文案和操作（用户暂停 / 积分不足 → 继续，失败 / 上下文不足 → 重试，
 * 自定义提示词导致失败 → 重试 + 改用内置重试）。
 * @param drama 短剧的规划相关字段
 * @param jobs 任务卡
 */
export function planningRow(
  drama: PlanningDrama,
  jobs: readonly DramaJob[],
): PlanningRowView | null {
  if (drama.phase === "done") return null;
  const word = unitWord(drama.unitKind);
  if (drama.phase === "running") {
    const active = jobs.filter((job) => isPipelineJob(job) && isActiveJob(job));
    const analyze = active.find((job) => job.step === "analyze" || job.step === "outline");
    const plan = active.find((job) => job.step === "plan");
    if (analyze && !plan) {
      return {
        kind: "running",
        title: "正在粗读全书",
        detail: "研读 · 抽样读开篇与后文，整理人物与梗概",
        progress: analyze.progress,
      };
    }
    const progress =
      drama.rangeEnd > 0 ? Math.min(99, Math.round((drama.planCursor / drama.rangeEnd) * 100)) : 0;
    const title =
      plan && plan.batchFrom && plan.batchTo
        ? `正在规划${batchRange(plan.batchFrom, plan.batchTo, word)}`
        : "正在规划";
    return {
      kind: "running",
      title,
      detail: `分集规划 · 已出 ${drama.episodes.length.toLocaleString("en-US")} 集目录`,
      progress,
    };
  }
  const code = drama.pauseCode || "user";
  const reasonOr = (fallback: string) => drama.pauseReason || fallback;
  switch (code) {
    case "credits":
      return {
        kind: "paused",
        tone: "error",
        title: "分集规划暂停",
        reason: "积分不足，充值后继续",
        actions: ["resume"],
      };
    case "failed":
      return {
        kind: "paused",
        tone: "error",
        title: "分集规划暂停",
        reason: `${reasonOr("规划失败")} · 失败不扣积分`,
        actions: ["retry"],
      };
    case "context":
      return {
        kind: "paused",
        tone: "error",
        title: "分集规划暂停",
        reason: `${reasonOr("模型上下文不足")} · 失败不扣积分`,
        actions: ["retry"],
      };
    case "custom_prompt":
      return {
        kind: "paused",
        tone: "error",
        title: "分集规划暂停",
        reason: `${reasonOr("自定义提示词导致输出不合格")} · 失败不扣积分`,
        actions: ["retry", "builtin"],
      };
    default:
      return {
        kind: "paused",
        tone: "muted",
        title: "分集规划已暂停",
        reason: `已规划到第 ${drama.planCursor} ${word}，已规划的集照常可以写剧本`,
        actions: ["resume"],
      };
  }
}
