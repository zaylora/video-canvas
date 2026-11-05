import { Check, Loader2 } from "lucide-react";

import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { isActiveJob, isPipelineJob } from "@/utils/drama/jobs";

import { useWorkspace } from "./context";

/**
 * 是否在研读 / 规划：analyzing 是自动规划还没完（研读中或规划中）；
 * showAnalyzing 表示第一集还没有出来，这时剧本页的大纲里显示研读视图
 * （刚从首页提交进来的短剧落在「剧本 → 大纲」，看到的就是它），第一集出现后立刻退出。
 * 暂停时（还没有任何集）视图仍在，标题写「分集规划已暂停」。
 */
export function useAnalyzing() {
  const { drama, jobs } = useWorkspace();
  const analyzing =
    drama.phase === "running" ||
    drama.status === "analyzing" ||
    jobs.some((job) => isPipelineJob(job) && isActiveJob(job));
  const paused = drama.phase === "paused";
  return {
    analyzing,
    paused,
    showAnalyzing: (analyzing || paused) && drama.episodes.length === 0,
  };
}

/**
 * 研读 / 规划中的步骤与骨架屏（放在大纲页标题下面）：「粗读全书」「分集规划」两步。
 * 粗读完成（已有大纲，或已经开始规划）后进入第二步。
 */
export function AnalyzingView() {
  const { drama, jobs } = useWorkspace();
  const { paused } = useAnalyzing();
  const planning =
    drama.planCursor > 0 ||
    !!drama.outlineMd ||
    jobs.some((job) => job.step === "plan" && isActiveJob(job));
  const current = planning ? 1 : 0;
  const steps = [
    ["粗读全书", "抽样读开篇与后文，整理人物与梗概"],
    ["分集规划", "按批读原文，生成分集目录"],
  ] as const;
  return (
    <div className="pt-6">
      <h3 className="text-base font-semibold">{paused ? "分集规划已暂停" : "正在研读剧本…"}</h3>
      <p className="text-muted-foreground mt-1 text-sm">可以离开这个页面，完成后会通知你</p>
      <ol className="mt-6 grid gap-3">
        {steps.map(([title, detail], index) => (
          <li key={title} className="flex items-start gap-3">
            <span
              className={cn(
                "mt-0.5 grid size-5 shrink-0 place-items-center rounded-full border",
                index < current && "bg-status-success border-transparent text-white",
                index === current && "border-status-running text-status-running",
              )}
            >
              {index < current ? (
                <Check className="size-3" />
              ) : index === current && !paused ? (
                <Loader2 className="size-3 animate-spin" />
              ) : null}
            </span>
            <div>
              <div className="text-sm font-medium">{title}</div>
              <div className="text-muted-foreground text-xs">{detail}</div>
            </div>
          </li>
        ))}
      </ol>
      <div className="mt-8 grid gap-2.5">
        {[96, 88, 92, 70, 84].map((width) => (
          <Skeleton
            key={width}
            className="h-4 motion-reduce:animate-none"
            style={{ width: `${width}%` }}
          />
        ))}
      </div>
    </div>
  );
}
