import { useEffect, useRef, useState } from "react";
import { ChevronRight, Circle, CircleX, Diamond } from "lucide-react";

import { cn } from "@/lib/utils";
import { durationPercent, formatDuration, type TraceStepView } from "@/utils/admin/trace";

import { FoldableCode } from "@/components/admin-ui/foldable-code";
import { Tag } from "@/components/admin-ui/tag";

/** 步骤类型的“形状 + 文字”标签：hook 是菱形，http 是圆点，失败是叉；不只靠颜色区分 */
function KindBadge({ step }: { step: TraceStepView }) {
  const label = step.kind === "hook" ? "hook" : step.kind === "http" ? "http" : "步骤";
  const Icon = step.failed ? CircleX : step.kind === "hook" ? Diamond : Circle;
  return (
    <span
      className={cn(
        "inline-flex shrink-0 items-center gap-1 rounded-md border px-1.5 text-[11px] leading-5 font-medium",
        step.failed
          ? "border-destructive/50 bg-destructive/10 text-destructive"
          : step.kind === "hook"
            ? "border-transparent bg-primary/10 text-primary"
            : "border-transparent bg-emerald-500/10 text-emerald-700 dark:text-emerald-400",
      )}
    >
      <Icon
        className={cn("size-3", !step.failed && step.kind === "http" && "fill-current")}
        aria-hidden
      />
      {step.failed ? `${label} · 失败` : label}
    </span>
  );
}

/**
 * 追踪里的一个步骤：默认折叠，只显示一行摘要（序号、类型、名称、状态、耗时条）；
 * 失败的步骤默认展开、标红，并且是列表里被自动滚动到可见的那一步（autoScroll）。
 * @param maxMs 本次追踪里最长的步骤耗时，耗时条按它归一，只用来一眼找慢步骤
 * @param autoScroll 是否挂载时滚动到可见（只给第一个失败步骤）
 */
export function TraceStepRow({
  step,
  maxMs,
  autoScroll,
}: {
  step: TraceStepView;
  maxMs: number;
  autoScroll?: boolean;
}) {
  const [open, setOpen] = useState(step.failed);
  const ref = useRef<HTMLLIElement>(null);
  const bodyId = `trace-step-${step.id}`;

  useEffect(() => {
    if (autoScroll) ref.current?.scrollIntoView({ block: "nearest" });
  }, [autoScroll]);

  return (
    <li
      ref={ref}
      data-step-kind={step.kind}
      data-failed={step.failed}
      className={cn(
        "overflow-hidden rounded-lg border",
        step.failed && "border-destructive bg-destructive/5",
      )}
    >
      <button
        type="button"
        className="hover:bg-muted/60 flex w-full items-center gap-2 px-2.5 py-1.5 text-left text-xs"
        aria-expanded={open}
        aria-controls={bodyId}
        onClick={() => setOpen((value) => !value)}
      >
        <ChevronRight
          className={cn(
            "text-muted-foreground size-3 shrink-0 transition-transform",
            open && "rotate-90",
          )}
        />
        <span className="text-muted-foreground w-4 shrink-0 font-mono">{step.index}</span>
        <KindBadge step={step} />
        <span className="min-w-0 flex-1 truncate font-mono" title={step.url ?? step.title}>
          {step.title}
        </span>
        {step.status !== undefined && (
          <span
            className={cn(
              "font-mono",
              step.status >= 400 ? "text-destructive" : "text-muted-foreground",
            )}
          >
            {step.status}
          </span>
        )}
        {step.truncated && <Tag tone="warning">已截断</Tag>}
        <span className="text-muted-foreground w-14 shrink-0 text-right font-mono">
          {formatDuration(step.durationMs)}
        </span>
        <span className="bg-muted h-1.5 w-14 shrink-0 overflow-hidden rounded-full" aria-hidden>
          <span
            className={cn("block h-full", step.failed ? "bg-destructive" : "bg-muted-foreground")}
            style={{ width: `${durationPercent(step.durationMs, maxMs)}%` }}
          />
        </span>
      </button>

      {open && (
        <div id={bodyId} className="flex flex-col gap-2 border-t px-2.5 py-2 text-xs">
          {step.error && (
            <p className="text-destructive font-medium break-words" role="alert">
              {step.error}
            </p>
          )}
          {step.blocks.length === 0 && !step.error && step.logs.length === 0 && (
            <p className="text-muted-foreground">这一步没有记录内容。</p>
          )}
          {step.blocks.map((block) => (
            <div key={block.label} className="flex flex-col gap-1">
              <div className="text-muted-foreground flex items-center gap-1.5 font-medium">
                {block.label}
                {block.label === "响应体" && step.truncated && <Tag tone="warning">已截断</Tag>}
              </div>
              <FoldableCode text={block.text} maxChars={600} />
            </div>
          ))}
          {step.logs.length > 0 && (
            <div className="flex flex-col gap-1">
              <div className="text-muted-foreground font-medium">
                utils.log（{step.logs.length}）
              </div>
              <ul className="bg-muted rounded-md p-2 font-mono text-[11px] leading-5">
                {step.logs.map((line, index) => (
                  <li key={index} className="break-all whitespace-pre-wrap">
                    {line}
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>
      )}
    </li>
  );
}
