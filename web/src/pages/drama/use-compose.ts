import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";

import { submitJob } from "@/api/drama";
import { dramaErrorMessage } from "@/api/drama/errors";
import { useDramaJobsStore } from "@/store/drama";
import { composeNotice } from "@/utils/drama/compose";

/**
 * 提交「合成整集」（契约 §14）：服务端 ffmpeg 拼接，不扣积分，进度走 drama.job 推送 + 任务卡。
 * 67010（没有可合成的片段）/ 67004（已有进行中的任务）等错误给出明确文案。
 * 返回 compose(no)：提交成功为 true；pending 是正在提交的集号。
 */
export function useCompose(dramaId: string) {
  const [pending, setPending] = useState<number | null>(null);
  const compose = useCallback(
    async (no: number) => {
      setPending(no);
      try {
        const job = await submitJob(dramaId, { step: "compose", episodes: [no] });
        useDramaJobsStore.getState().applyJob(dramaId, job);
        return true;
      } catch (e) {
        toast.error(dramaErrorMessage(e, "合成提交失败，请稍后重试"));
        return false;
      } finally {
        setPending(null);
      }
    },
    [dramaId],
  );
  return { compose, pending };
}

/**
 * 合成结束时的提示：成功且跳过了没有采用视频的片段，就提示跳过了哪几个。
 * 只提示这次页面打开之后才结束的任务（进入时已经成功的旧任务不重复提示）。
 */
export function useComposeNotices(dramaId: string) {
  useEffect(() => {
    const seen = new Set(
      (useDramaJobsStore.getState().byDrama[dramaId] ?? [])
        .filter((job) => job.status === "succeeded")
        .map((job) => job.id),
    );
    return useDramaJobsStore.subscribe((state) => {
      for (const job of state.byDrama[dramaId] ?? []) {
        if (job.status !== "succeeded" || seen.has(job.id)) continue;
        seen.add(job.id);
        const message = composeNotice(job);
        if (message) toast.warning(message);
      }
    });
  }, [dramaId]);
}
