import { useState } from "react";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { ChevronUp, X } from "lucide-react";

import { DURATION, EASE_OUT } from "@/lib/motion";
import { cn } from "@/lib/utils";
import type { RunStrip as RunStripModel } from "@/utils/agent/timeline";

/**
 * 计划条：输入框上方的一行，没有外框。左边是当前这一步，中间每一步一小格（做完绿、在做蓝），右边是 done/total。
 * 只管计划（运行状态由消息流末尾的状态行负责）；计划没做完而运行已结束时显示「未完成」，并给 ✕ 隐藏。
 * 点击在原地向上展开完整步骤，把消息流往上顶，不盖住任何内容。
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
  const finished = total > 0 && done === total;

  return (
    <motion.div
      initial={reduce ? { opacity: 0 } : { opacity: 0, y: 6 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, transition: { duration: DURATION.exit } }}
      transition={{ duration: DURATION.base, ease: EASE_OUT }}
      className="mx-4 mb-1.5 shrink-0"
    >
      <AnimatePresence initial={false}>
        {open && (
          <motion.ul
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: "auto", opacity: 1 }}
            exit={{ height: 0, opacity: 0, transition: { duration: DURATION.exit } }}
            transition={{ duration: DURATION.base, ease: EASE_OUT }}
            className="max-h-48 overflow-y-auto text-xs"
          >
            {strip.steps.map((s, i) => (
              <li
                key={i}
                className={cn(
                  "flex h-6 items-center gap-2.5",
                  s.status === "todo" && "text-muted-foreground",
                  s.status === "done" && "text-muted-foreground/70",
                )}
              >
                <span
                  className={cn(
                    "mx-1 size-1.5 shrink-0 rounded-full border-[1.5px]",
                    s.status === "done" && "bg-status-success border-status-success",
                    s.status === "doing" &&
                      "bg-status-running border-status-running motion-safe:animate-pulse",
                    s.status === "todo" && "border-current",
                  )}
                />
                <span className="truncate">{s.title}</span>
              </li>
            ))}
          </motion.ul>
        )}
      </AnimatePresence>
      <div className="flex h-6.5 items-center gap-2.5 text-xs">
        <button
          type="button"
          aria-expanded={open}
          aria-label="展开计划"
          onClick={() => setOpen((v) => !v)}
          className="focus-visible:ring-node-ring/60 flex min-w-0 flex-1 items-center gap-2.5 rounded-md text-left outline-none focus-visible:ring-2"
        >
          <span className={cn("max-w-[45%] truncate", finished && "text-status-success")}>
            {strip.unfinished && "未完成 · "}
            {finished ? "完成" : strip.current}
          </span>
          <span className="flex min-w-10 flex-1 gap-[3px]" aria-hidden>
            {strip.steps.map((s, i) => (
              <i
                key={i}
                className={cn(
                  "h-[3px] flex-1 rounded-full transition-colors duration-300",
                  s.status === "done" && "bg-status-success",
                  s.status === "doing" &&
                    cn("bg-status-running", strip.active && "motion-safe:animate-pulse"),
                  s.status === "todo" && "bg-foreground/12",
                )}
              />
            ))}
          </span>
          <span className="text-muted-foreground tabular-nums">
            {done}/{total}
          </span>
          <ChevronUp
            className={cn(
              "text-muted-foreground size-3.5 shrink-0 transition-transform duration-120",
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
