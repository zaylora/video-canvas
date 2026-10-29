import { useState } from "react";
import { AnimatePresence, motion } from "motion/react";
import { Loader2, RotateCw, X } from "lucide-react";
import { toast } from "sonner";

import { cancelJob, submitJob } from "@/api/drama";
import { dramaErrorMessage, jobFailureMessage } from "@/api/drama/errors";
import type { DramaJob } from "@/api/drama/type";
import { Button } from "@/components/ui/button";
import { DURATION, EASE_OUT, SPRING } from "@/lib/motion";
import { useDramaJobsStore } from "@/store/drama";
import { cn } from "@/lib/utils";
import {
  bannerJobs,
  coveredInPlace,
  isActiveJob,
  jobRunningText,
  jobTitle,
  type InPlaceView,
} from "@/utils/drama/jobs";

import { useWorkspace } from "./context";

/** 取消一条进行中的任务；busy 期间按钮该禁用 */
export function useCancelJob(job: DramaJob | undefined) {
  const { drama } = useWorkspace();
  const [busy, setBusy] = useState(false);
  const cancel = async () => {
    if (!job) return;
    setBusy(true);
    try {
      useDramaJobsStore.getState().applyJob(drama.id, await cancelJob(drama.id, job.id));
    } catch (e) {
      toast.error(dramaErrorMessage(e, "取消失败"));
    } finally {
      setBusy(false);
    }
  };
  return { busy, cancel };
}

/** 任务进度条（细条，弹簧推进）：顶部进度条和页面里就地显示进度的地方共用 */
export function JobProgressBar({ progress, className }: { progress: number; className?: string }) {
  return (
    <div className={cn("bg-muted h-1 overflow-hidden rounded-full", className)}>
      <motion.i
        className="bg-status-running block h-full origin-left rounded-full"
        initial={false}
        animate={{ scaleX: Math.max(0.03, progress / 100) }}
        transition={SPRING}
      />
    </div>
  );
}

/** 一条任务：进行中显示话术、进度条和取消；失败显示原因、重试和关闭 */
function JobRow({ job }: { job: DramaJob }) {
  const { drama, refresh } = useWorkspace();
  const [busy, setBusy] = useState(false);
  const canceller = useCancelJob(job);
  const active = isActiveJob(job);

  const retry = async () => {
    setBusy(true);
    try {
      const next = await submitJob(drama.id, {
        step: job.step,
        episodes: job.episodes,
        ratio: job.step === "breakdown" ? drama.ratio : undefined,
      });
      useDramaJobsStore.getState().dismiss(drama.id, job.id);
      useDramaJobsStore.getState().applyJob(drama.id, next);
      void refresh();
    } catch (e) {
      toast.error(dramaErrorMessage(e, "重试失败，请稍后再试"));
    } finally {
      setBusy(false);
    }
  };

  return (
    <motion.div
      layout="position"
      initial={{ opacity: 0, y: -6 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, transition: { duration: DURATION.exit } }}
      transition={{ duration: DURATION.base, ease: EASE_OUT }}
      role={active ? "status" : "alert"}
      className="grid gap-1.5 px-4 py-2.5"
    >
      <div className="flex items-center gap-2 text-sm">
        {active ? (
          <>
            <Loader2 className="text-status-running size-4 shrink-0 animate-spin motion-reduce:animate-none" />
            <span className="min-w-0 flex-1 truncate">
              <b className="font-medium">{jobRunningText(job.step)}</b>
              <span className="text-muted-foreground ml-2 text-xs">
                {job.status === "queued" ? "排队中" : jobTitle(job)} ·
                可以离开这个页面，完成后会通知你
              </span>
            </span>
            <span className="text-muted-foreground text-xs tabular-nums">{job.progress}%</span>
            <Button
              variant="ghost"
              size="xs"
              disabled={canceller.busy}
              onClick={() => void canceller.cancel()}
            >
              取消
            </Button>
          </>
        ) : (
          <>
            <span className="text-destructive min-w-0 flex-1 truncate">
              <b className="font-medium">{jobTitle(job)}失败</b>
              <span className="ml-2 text-xs">{jobFailureMessage(job)} · 失败不扣积分</span>
            </span>
            <Button variant="outline" size="xs" disabled={busy} onClick={() => void retry()}>
              <RotateCw />
              重试
            </Button>
            <Button
              variant="ghost"
              size="icon-xs"
              aria-label="关闭"
              onClick={() => useDramaJobsStore.getState().dismiss(drama.id, job.id)}
            >
              <X />
            </Button>
          </>
        )}
      </div>
      {active && <JobProgressBar progress={job.progress} />}
    </motion.div>
  );
}

/**
 * 工作台顶栏下方的进度条：点了「开始 / 分析 / 拆解」之后，不管用户在哪一页都能立刻看到正在做什么。
 * 进行中的任务显示话术和进度，最新一条失败时显示原因并可重试。
 * 页面里已经就地显示进度的任务（研读视图里的研读、片场里当前集的分镜）不重复显示。
 */
export function JobBar({ inPlace }: { inPlace: InPlaceView }) {
  const { jobs } = useWorkspace();
  const shown = bannerJobs(jobs).filter((job) => !coveredInPlace(job, inPlace));
  return (
    <div aria-label="任务进度" className="border-border shrink-0 border-b empty:hidden">
      <AnimatePresence initial={false}>
        {shown.map((job) => (
          <JobRow key={job.id} job={job} />
        ))}
      </AnimatePresence>
    </div>
  );
}
