import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { getDrama, updateDrama } from "@/api/drama";
import { DRAMA_ERR, dramaErrorMessage, isDramaError, isRevisionConflict } from "@/api/drama/errors";
import type { Drama, DramaJob, UpdateDramaInput } from "@/api/drama/type";
import { useDramaJobsStore } from "@/store/drama";
import { reconcileDrama } from "@/utils/drama/job-events";
import type { AutosaveStatus } from "@/utils/drama/autosave";
import { toast } from "sonner";

/** 顶栏「保存状态」：把各处自动保存的状态汇总成一个 */
export type SaveSummary = "saved" | "saving" | "error" | "conflict";

/** 汇总：冲突 > 失败 > 有待保存 / 保存中 > 已保存 */
export function summarizeSave(states: Record<string, AutosaveStatus>): SaveSummary {
  const all = Object.values(states);
  if (all.includes("conflict")) return "conflict";
  if (all.includes("error")) return "error";
  if (all.includes("saving") || all.includes("dirty")) return "saving";
  return "saved";
}

const NO_JOBS: DramaJob[] = [];

/** 工作台对子组件开放的状态与动作 */
export type DramaWorkspace = {
  drama: Drama;
  jobs: DramaJob[];
  save: SaveSummary;
  /** 重新读取详情（任务收尾、冲突后加载最新） */
  refresh: () => Promise<void>;
  /**
   * 改短剧（标题、画风、比例、Agent 模型、大纲、人物）：串行排队，用最新 revision；返回保存后的 revision。
   * revision 冲突时提示并加载最新，再抛出错误让调用方知道没存上。
   */
  patch: (fields: Omit<UpdateDramaInput, "revision">) => Promise<number>;
  /** 把某集的新 revision / 状态并进本地详情（改了剧本后，目录与「下一步」要跟着） */
  applyEpisode: (no: number, patch: Partial<Drama["episodes"][number]>) => void;
  /** 各处自动保存汇报自己的状态 */
  reportSave: (key: string, status: AutosaveStatus) => void;
};

/**
 * 短剧工作台的数据层：详情、任务卡（来自 store，WebSocket 推送 + 对账）、保存状态。
 * - 挂载时拉详情、登记「正在看这部短剧」（WebSocket 重连 / 回到前台时据此对账）、对账一次进行中的任务；
 * - 任务收尾（settled 计数变化）后重新拉详情：集、大纲、剧本、比例都可能变了。
 */
export function useDramaWorkspace(id: string) {
  const [drama, setDrama] = useState<Drama | null>(null);
  const [error, setError] = useState<"notfound" | "failed" | null>(null);
  const [saveStates, setSaveStates] = useState<Record<string, AutosaveStatus>>({});
  const revisionRef = useRef(0);
  const queueRef = useRef<Promise<unknown>>(Promise.resolve());
  /** 并发刷新只认最后一次发起的，避免旧响应盖掉新数据 */
  const fetchSeq = useRef(0);
  const jobs = useDramaJobsStore((state) => state.byDrama[id]) ?? NO_JOBS;
  const settled = useDramaJobsStore((state) => state.settled[id] ?? 0);

  const refresh = useCallback(async () => {
    const seq = ++fetchSeq.current;
    try {
      const next = await getDrama(id, true);
      if (seq !== fetchSeq.current) return;
      revisionRef.current = next.revision;
      setDrama(next);
      setError(null);
    } catch (e) {
      if (seq !== fetchSeq.current) return;
      // 已经有数据时刷新失败不打断；第一次就失败才显示错误页
      setError((prev) => (isDramaError(e, DRAMA_ERR.notFound) ? "notfound" : (prev ?? "failed")));
    }
  }, [id]);

  // 首次加载：详情 + 任务对账
  useEffect(() => {
    let active = true;
    void (async () => {
      await refresh();
      if (!active) return;
      await reconcileDrama(id);
    })();
    const unwatch = useDramaJobsStore.getState().watch(id);
    return () => {
      active = false;
      unwatch();
    };
  }, [id, refresh]);

  // 详情里带的进行中任务也并进任务卡（刷新页面后立刻有卡片，不用等对账）
  useEffect(() => {
    if (!drama) return;
    const known = new Set(useDramaJobsStore.getState().byDrama[id]?.map((job) => job.id));
    for (const job of drama.activeJobs) {
      if (!known.has(job.id)) useDramaJobsStore.getState().applyJob(id, job);
    }
  }, [drama, id]);

  // 任务收尾 -> 重新拉详情
  const firstSettled = useRef(settled);
  useEffect(() => {
    if (settled === firstSettled.current) return;
    void refresh();
  }, [settled, refresh]);

  const patch = useCallback<DramaWorkspace["patch"]>(
    (fields) => {
      const run = async () => {
        try {
          const next = await updateDrama(id, { ...fields, revision: revisionRef.current });
          revisionRef.current = next.revision;
          setDrama((prev) => (prev ? { ...next, activeJobs: prev.activeJobs } : next));
          return next.revision;
        } catch (e) {
          if (isRevisionConflict(e)) {
            toast.warning("这部短剧在别处被修改过，已为你加载最新内容");
            void refresh();
          } else {
            toast.error(dramaErrorMessage(e, "保存失败，请稍后重试"));
          }
          throw e;
        }
      };
      const result = queueRef.current.then(run, run);
      queueRef.current = result.catch(() => undefined);
      return result;
    },
    [id, refresh],
  );

  const applyEpisode = useCallback<DramaWorkspace["applyEpisode"]>((no, change) => {
    setDrama((prev) =>
      prev
        ? {
            ...prev,
            episodes: prev.episodes.map((ep) => (ep.no === no ? { ...ep, ...change } : ep)),
          }
        : prev,
    );
  }, []);

  const reportSave = useCallback((key: string, status: AutosaveStatus) => {
    setSaveStates((prev) => (prev[key] === status ? prev : { ...prev, [key]: status }));
  }, []);

  const save = useMemo(() => summarizeSave(saveStates), [saveStates]);
  return { drama, error, jobs, save, refresh, patch, applyEpisode, reportSave };
}
