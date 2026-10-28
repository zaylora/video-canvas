import { useState } from "react";
import { AnimatePresence, motion } from "motion/react";
import { Bot, ListChecks, Loader2, RotateCw, X } from "lucide-react";
import { toast } from "sonner";

import { cancelJob, submitJob } from "@/api/drama";
import { dramaErrorMessage, jobFailureMessage } from "@/api/drama/errors";
import type { DramaJob } from "@/api/drama/type";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { DURATION, EASE_OUT, SPRING } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useDramaJobsStore } from "@/store/drama";
import { skippedText } from "@/utils/drama/compose";
import { isActiveJob, jobTitle } from "@/utils/drama/jobs";
import { formatCanvasTime } from "@/utils/home/home";

import { AssistantPane } from "../assistant/assistant-pane";
import { useWorkspace } from "./context";

/** 一张任务卡：进行中显示进度条，失败显示原因与重试，成功显示时间与实际扣费 */
function JobCard({ job }: { job: DramaJob }) {
  const { drama, refresh } = useWorkspace();
  const [busy, setBusy] = useState(false);
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

  const cancel = async () => {
    setBusy(true);
    try {
      useDramaJobsStore.getState().applyJob(drama.id, await cancelJob(drama.id, job.id));
    } catch (e) {
      toast.error(dramaErrorMessage(e, "取消失败"));
    } finally {
      setBusy(false);
    }
  };

  return (
    <motion.div
      layout="position"
      initial={{ opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, transition: { duration: DURATION.exit } }}
      transition={{ duration: DURATION.base, ease: EASE_OUT }}
      className="bg-card grid gap-1.5 rounded-xl border p-3"
    >
      <div className="flex items-center gap-2">
        <span className="min-w-0 flex-1 truncate text-sm font-medium">{jobTitle(job)}</span>
        {job.status === "running" || job.status === "queued" ? (
          <span className="text-status-running inline-flex shrink-0 items-center gap-1 text-xs">
            <Loader2 className="size-3 animate-spin" />
            {job.status === "queued" ? "排队中" : "进行中"}
          </span>
        ) : job.status === "failed" ? (
          <span className="text-destructive shrink-0 text-xs">失败</span>
        ) : job.status === "canceled" ? (
          <span className="text-muted-foreground shrink-0 text-xs">已取消</span>
        ) : (
          <span className="text-status-success shrink-0 text-xs">完成</span>
        )}
        {active && (
          <Button
            variant="ghost"
            size="icon-xs"
            aria-label="取消任务"
            disabled={busy}
            onClick={() => void cancel()}
          >
            <X />
          </Button>
        )}
      </div>

      {active && (
        <div className="grid gap-1">
          <div className="bg-muted h-1.5 overflow-hidden rounded-full">
            <motion.i
              className="bg-status-running block h-full origin-left rounded-full"
              initial={false}
              animate={{ scaleX: Math.max(0.03, job.progress / 100) }}
              transition={SPRING}
            />
          </div>
          <div className="text-muted-foreground text-[11px] tabular-nums">{job.progress}%</div>
        </div>
      )}

      {job.status === "failed" && (
        <>
          <p className="text-destructive text-xs">{jobFailureMessage(job)}</p>
          <div className="flex items-center gap-2">
            <Button variant="outline" size="sm" disabled={busy} onClick={() => void retry()}>
              <RotateCw />
              重试
            </Button>
            <span className="text-muted-foreground text-[11px]">失败不扣积分</span>
          </div>
        </>
      )}

      {job.status === "succeeded" && (
        <div className="text-muted-foreground text-[11px] tabular-nums">
          {formatCanvasTime(job.finishedAt ?? job.createdAt)}
          {job.credits > 0 && <span className="text-credit ml-2">-{job.credits} 积分</span>}
        </div>
      )}
      {job.step === "compose" &&
        job.status === "succeeded" &&
        (job.result?.skippedSeqs.length ?? 0) > 0 && (
          <p className="text-status-warning text-xs">
            跳过了没有采用视频的片段 {skippedText(job.result?.skippedSeqs ?? [])}
          </p>
        )}
    </motion.div>
  );
}

/** 右栏：「进度」任务卡（WebSocket 推送 + 挂载 / 重连时对账）和「助手」（短剧助手，复用画布 Agent） */
export function SidePanel({ className }: { className?: string }) {
  const { jobs } = useWorkspace();
  return (
    <aside
      aria-label="右栏"
      className={cn("border-border bg-background flex w-80 shrink-0 flex-col border-l", className)}
    >
      <Tabs defaultValue="progress" className="min-h-0 flex-1 gap-0">
        <div className="flex items-center gap-2 p-3">
          <TabsList>
            <TabsTrigger value="progress">
              <ListChecks />
              进度
            </TabsTrigger>
            <TabsTrigger value="assistant">
              <Bot />
              助手
            </TabsTrigger>
          </TabsList>
        </div>
        <TabsContent value="progress" className="min-h-0 overflow-y-auto px-3 pb-3">
          <div className="grid content-start gap-2">
            <AnimatePresence initial={false}>
              {jobs.map((job) => (
                <JobCard key={job.id} job={job} />
              ))}
            </AnimatePresence>
            {jobs.length === 0 && (
              <div className="text-muted-foreground py-10 text-center text-xs">
                还没有任务。研读、生成剧本、资产拆解的进度会出现在这里。
              </div>
            )}
            <p className="text-muted-foreground px-0.5 pt-1 text-[11px]">
              刷新或断线后自动对账恢复进度。
            </p>
          </div>
        </TabsContent>
        <TabsContent value="assistant" className="min-h-0">
          <AssistantPane />
        </TabsContent>
      </Tabs>
    </aside>
  );
}
