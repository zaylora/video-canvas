import { Check, Loader2 } from "lucide-react";

import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { isActiveJob } from "@/utils/drama/jobs";

import { useWorkspace } from "./context";

/**
 * 是否在研读：analyzing 是研读中；showAnalyzing 表示大纲和分集都还没有，这时剧本页的大纲里显示研读视图
 * （刚从首页提交进来的短剧落在「剧本 → 大纲」，看到的就是它）。
 */
export function useAnalyzing() {
  const { drama, jobs } = useWorkspace();
  const analyzing =
    drama.status === "analyzing" ||
    jobs.some((job) => (job.step === "analyze" || job.step === "outline") && isActiveJob(job));
  return { analyzing, showAnalyzing: analyzing && !drama.outlineMd && drama.episodes.length === 0 };
}

/** 研读中的步骤与骨架屏（放在大纲页标题下面） */
export function AnalyzingView() {
  const { jobs } = useWorkspace();
  const outlineRunning = jobs.some((job) => job.step === "outline" && isActiveJob(job));
  const current = outlineRunning ? 1 : 0;
  const steps = [
    ["研读剧本", "判断是剧本还是小说，整理章节与人物"],
    ["故事大纲与人物传记", "提炼主线、人物关系和每集梗概"],
    ["按集拆分", "生成分集目录"],
  ] as const;
  return (
    <div className="pt-6">
      <h3 className="text-base font-semibold">正在研读剧本…</h3>
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
              ) : index === current ? (
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
