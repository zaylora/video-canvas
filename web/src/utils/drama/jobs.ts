import type { DramaJob, DramaStep } from "@/api/drama/type";

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
  outline: "故事大纲与人物",
  scripts: "分集剧本",
  breakdown: "资产拆解",
  asset_prompts: "写资产提示词",
};

/** 步骤的短名称 */
export const stepName = (step: DramaStep): string => STEP_NAME[step];

/** 进度卡标题：带上范围，如「资产拆解 · 第 2 集」「分集剧本 · 3 集」 */
export function jobTitle(job: Pick<DramaJob, "step" | "episodes">): string {
  const name = STEP_NAME[job.step];
  if (job.step === "analyze" || job.step === "outline" || job.step === "asset_prompts") return name;
  if (job.episodes.length === 0) return name;
  if (job.episodes.length === 1 && job.step === "breakdown") {
    return `${name} · 第 ${job.episodes[0]} 集`;
  }
  return `${name} · ${job.episodes.length} 集`;
}

/** 同一部短剧同时只能跑一个流水线任务：有进行中的就给出「为什么不能提交」，没有返回 null */
export function busyReason(jobs: readonly DramaJob[]): string | null {
  const running = jobs.find(isActiveJob);
  if (!running) return null;
  return `有进行中的任务：${jobTitle(running)}`;
}
