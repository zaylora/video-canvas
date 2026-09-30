import { useMemo } from "react";
import { Loader2, RefreshCw, ShieldCheck } from "lucide-react";

import type { TraceStep } from "@/api/admin-ai/type";
import { Button } from "@/components/ui/button";
import { firstFailedStep, formatDuration, maxStepDuration, toTraceView } from "@/utils/admin/trace";

import { CopyButton, Notice } from "../shared";
import { TraceStepRow } from "./trace-step";

/**
 * 追踪时间线：把一次试跑的每次钩子调用与每次 HTTP 请求按执行顺序摊开。
 * 与模型页解耦（只吃 steps），后续任务详情也能复用。
 * - hook 与 http 两种形态，形状加文字区分；默认折叠，失败步骤默认展开、标红并滚动到可见；
 * - 后端已脱敏，界面固定写一行说明，让运营放心复制分享；
 * - 提供“复制全部（JSON）”，不提供下载（追踪里可能含用户提示词）；
 * - 步骤数为 0 是“任务尚未产生追踪”的中性提示，不是错误。
 * @param taskId 试跑任务 ID，显示在标题里
 * @param loading 正在拉取追踪
 * @param onRefresh 刷新追踪；不传则不显示刷新按钮
 */
export function TraceTimeline({
  taskId,
  steps,
  loading,
  onRefresh,
}: {
  taskId: number | string;
  steps: TraceStep[];
  loading?: boolean;
  onRefresh?: () => void;
}) {
  const view = useMemo(() => toTraceView(steps), [steps]);
  const maxMs = maxStepDuration(view);
  const failed = firstFailedStep(view);

  return (
    <section aria-label="追踪" className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        <h3 className="text-sm font-medium">
          追踪 · 试跑 #{taskId}
          {view.steps.length > 0 && (
            <span className="text-muted-foreground font-normal">
              {" "}
              · 共 {view.steps.length} 步 · {formatDuration(view.totalMs)}
              {view.failedCount > 0 && <span className="text-destructive"> · {view.failedCount} 步失败</span>}
            </span>
          )}
        </h3>
        <div className="ml-auto flex items-center gap-1">
          {onRefresh && (
            <Button size="xs" variant="ghost" disabled={loading} onClick={onRefresh}>
              {loading ? <Loader2 className="animate-spin" /> : <RefreshCw />}
              刷新
            </Button>
          )}
          {view.steps.length > 0 && <CopyButton text={JSON.stringify(steps, null, 2)} label="复制全部（JSON）" />}
        </div>
      </div>

      <p className="text-muted-foreground flex items-center gap-1.5 text-xs">
        <ShieldCheck className="size-3.5 shrink-0" aria-hidden />
        Key 与鉴权头已脱敏，可以放心复制分享。
      </p>

      {view.steps.length === 0 ? (
        <Notice tone="info">{loading ? "正在拉取追踪…" : "任务尚未产生追踪，稍后刷新。"}</Notice>
      ) : (
        <ol className="flex flex-col gap-1.5">
          {view.steps.map((step) => (
            <TraceStepRow key={step.id} step={step} maxMs={maxMs} autoScroll={failed?.id === step.id} />
          ))}
        </ol>
      )}
    </section>
  );
}
