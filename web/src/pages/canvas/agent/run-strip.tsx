import { useState } from "react";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { Check, ChevronUp, Circle, Loader2, X } from "lucide-react";

import { DURATION, EASE_OUT } from "@/lib/motion";
import { cn } from "@/lib/utils";
import type { RunStrip as RunStripModel } from "@/utils/agent/timeline";

import { ProgressRing } from "./progress-ring";

/**
 * 置顶计划条：悬浮在输入框上方，只管计划（运行状态由消息流末尾的状态行负责）。
 * 进行中显示进度和当前一步；计划没做完而运行已结束时显示「未完成」，✕ 隐藏。
 * 点击向上展开完整步骤（浮在消息流上方，不挤压布局）。
 */
export function RunStrip({
  strip,
  onHide,
}: {
  strip: RunStripModel;
  onHide: (runId: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const reduce = useReducedMotion();
  const { done, total } = strip.progress;

  return (
    <motion.div
      initial={reduce ? { opacity: 0 } : { opacity: 0, y: 6 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, transition: { duration: DURATION.exit } }}
      transition={{ duration: DURATION.base, ease: EASE_OUT }}
      className="relative mx-3 mb-2"
    >
      <AnimatePresence>
        {open && (
          <motion.ul
            initial={reduce ? { opacity: 0 } : { opacity: 0, y: 4, scale: 0.96 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={
              reduce
                ? { opacity: 0, transition: { duration: DURATION.exit } }
                : { opacity: 0, y: 4, scale: 0.96, transition: { duration: DURATION.exit } }
            }
            transition={{ duration: DURATION.base, ease: EASE_OUT }}
            style={{ transformOrigin: "bottom" }}
            className="agent-raised bg-popover absolute right-0 bottom-full left-0 z-10 mb-1.5 flex max-h-56 flex-col gap-1.5 overflow-y-auto rounded-xl p-3 text-xs"
          >
            {strip.steps.map((s, i) => (
              <li key={i} className="flex items-start gap-2">
                {s.status === "done" ? (
                  <Check className="text-status-success mt-0.5 size-3.5 shrink-0" />
                ) : s.status === "doing" ? (
                  <Loader2
                    className={cn(
                      "text-status-running mt-0.5 size-3.5 shrink-0",
                      strip.active && "animate-spin motion-reduce:animate-none",
                    )}
                  />
                ) : (
                  <Circle className="text-muted-foreground mt-0.5 size-3.5 shrink-0" />
                )}
                <span className={cn(s.status === "done" && "text-muted-foreground line-through")}>
                  {s.title}
                </span>
              </li>
            ))}
          </motion.ul>
        )}
      </AnimatePresence>
      <div className="agent-raised flex h-9 items-center gap-2 rounded-xl bg-[color-mix(in_oklab,var(--foreground)_4%,var(--popover))] px-2.5 text-xs">
        <button
          type="button"
          aria-expanded={open}
          aria-label="展开计划"
          onClick={() => setOpen((v) => !v)}
          className="focus-visible:ring-node-ring/60 flex min-w-0 flex-1 items-center gap-2 rounded-md text-left outline-none focus-visible:ring-2"
        >
          <ProgressRing value={done} total={total} className="stroke-status-running" />
          <span className="truncate tabular-nums">
            {strip.unfinished && "未完成 "}
            {done}/{total}
            {strip.current && ` · ${strip.current}`}
          </span>
          <ChevronUp
            className={cn(
              "text-muted-foreground ml-auto size-3.5 shrink-0 transition-transform duration-120",
              open && "rotate-180",
            )}
          />
        </button>
        {strip.unfinished && (
          <button
            type="button"
            aria-label="隐藏这个计划"
            onClick={() => onHide(strip.runId)}
            className="text-muted-foreground hover:text-foreground focus-visible:ring-node-ring/60 rounded-md p-0.5 outline-none focus-visible:ring-2"
          >
            <X className="size-3.5" />
          </button>
        )}
      </div>
    </motion.div>
  );
}
