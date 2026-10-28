import { create } from "zustand";

import type { DramaJob } from "@/api/drama/type";
import { isActiveJob, mergeJob, pruneFinished, reconcileJobs } from "@/utils/drama/jobs";

/** 每部短剧最多留几条已结束的任务卡 */
const KEEP_FINISHED = 5;

type DramaJobsState = {
  /** 每部短剧的任务卡（进行中 + 最近结束的），新的在前 */
  byDrama: Record<string, DramaJob[]>;
  /** 每部短剧「有任务收尾」的次数；工作台据此重新拉详情（集、大纲、剧本都可能变了） */
  settled: Record<string, number>;
  /** 正在打开的工作台（引用计数）：WebSocket 重连 / 回到前台时只对账这些 */
  watching: Record<string, number>;
  /** 并入一条任务快照（WebSocket 推送 / 提交任务的返回） */
  applyJob: (dramaId: string, job: DramaJob) => void;
  /** 用服务端的进行中任务对账 */
  applyActive: (dramaId: string, active: DramaJob[]) => void;
  /** 关掉一张任务卡（只用于已结束的） */
  dismiss: (dramaId: string, jobId: string) => void;
  /** 工作台挂载时登记，卸载时取消 */
  watch: (dramaId: string) => () => void;
};

const bump = (settled: Record<string, number>, dramaId: string) => ({
  ...settled,
  [dramaId]: (settled[dramaId] ?? 0) + 1,
});

export const useDramaJobsStore = create<DramaJobsState>((set, get) => ({
  byDrama: {},
  settled: {},
  watching: {},

  applyJob: (dramaId, job) => {
    const current = get().byDrama[dramaId] ?? [];
    const before = current.find((item) => item.id === job.id);
    const merged = pruneFinished(mergeJob(current, job), KEEP_FINISHED);
    const after = merged.find((item) => item.id === job.id);
    // 进行中 -> 结束（成功 / 失败 / 取消），或者第一次见到就已经结束：数据可能变了
    const settledNow = !!after && !isActiveJob(after) && (!before || isActiveJob(before));
    set((state) => ({
      byDrama: { ...state.byDrama, [dramaId]: merged },
      settled: settledNow ? bump(state.settled, dramaId) : state.settled,
    }));
  },

  applyActive: (dramaId, active) => {
    const { jobs, ended } = reconcileJobs(get().byDrama[dramaId] ?? [], active);
    set((state) => ({
      byDrama: { ...state.byDrama, [dramaId]: pruneFinished(jobs, KEEP_FINISHED) },
      settled: ended.length > 0 ? bump(state.settled, dramaId) : state.settled,
    }));
  },

  dismiss: (dramaId, jobId) =>
    set((state) => ({
      byDrama: {
        ...state.byDrama,
        [dramaId]: (state.byDrama[dramaId] ?? []).filter(
          (job) => job.id !== jobId || isActiveJob(job),
        ),
      },
    })),

  watch: (dramaId) => {
    set((state) => ({
      watching: { ...state.watching, [dramaId]: (state.watching[dramaId] ?? 0) + 1 },
    }));
    return () =>
      set((state) => {
        const count = (state.watching[dramaId] ?? 1) - 1;
        const watching = { ...state.watching };
        if (count <= 0) delete watching[dramaId];
        else watching[dramaId] = count;
        return { watching };
      });
  },
}));
