import { useState } from "react";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { Check, ChevronUp, X } from "lucide-react";

import { DURATION, EASE_OUT } from "@/lib/motion";
import { cn } from "@/lib/utils";
import type { RunStrip as RunStripModel } from "@/utils/agent/timeline";

import { ProgressRing } from "./progress-ring";

/**
 * 计划条（设计稿 6.12）：并进输入框顶部的一行，用发丝线和编辑区隔开。
 * 左边进度环（没做完琥珀、做完绿），然后是「未完成」徽标和当前这一步，右边 done/total、展开、隐藏。
 * 只管计划（运行状态由消息流里的活动行负责）；点开时步骤列表在输入框内部向上长出。
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
      initial={reduce ? { opacity: 0 } : { opacity: 0, y: 4 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, transition: { duration: DURATION.exit } }}
      transition={{ duration: DURATION.base, ease: EASE_OUT }}
      className="border-foreground/8 border-b"
    >
      <AnimatePresence initial={false}>
        {open && (
          <motion.ul
            initial={reduce ? { opacity: 0 } : { opacity: 0, y: 4 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, transition: { duration: DURATION.exit } }}
            transition={{ duration: DURATION.base, ease: EASE_OUT }}
            className="flex max-h-48 flex-col overflow-y-auto px-3 pt-2 text-xs"
          >
            {strip.steps.map((s, i) => (
              <li
                key={i}
                className={cn(
                  "flex h-6.5 items-center gap-2.5",
                  s.status === "todo" && "text-muted-foreground",
                  s.status === "done" &&
                    "text-muted-foreground line-through decoration-foreground/20",
                  s.status === "doing" && "font-medium",
                )}
              >
                <span className="grid size-3.5 shrink-0 place-items-center">
                  {s.status === "done" ? (
                    <Check className="text-status-success size-3.5" />
                  ) : (
                    <span
                      className={cn(
                        "size-[7px] rounded-full border-[1.5px]",
                        s.status === "doing"
                          ? "bg-status-warning border-status-warning"
                          : "border-current opacity-70",
                      )}
                    />
                  )}
                </span>
                <span className="truncate">{s.title}</span>
              </li>
            ))}
          </motion.ul>
        )}
      </AnimatePresence>
      <div className="flex h-9.5 items-center gap-2 pr-1.5 pl-3 text-xs">
        <button
          type="button"
          aria-expanded={open}
          aria-label={open ? "收起计划" : "展开计划"}
          onClick={() => setOpen((v) => !v)}
          className="focus-visible:ring-node-ring/60 flex min-w-0 flex-1 items-center gap-2 rounded-md text-left whitespace-nowrap outline-none focus-visible:ring-2"
        >
          <ProgressRing
            value={done}
            total={total}
            className={finished ? "stroke-status-success" : "stroke-status-warning"}
          />
          {finished ? (
            <span className="bg-status-success/16 text-status-success shrink-0 rounded-[5px] px-1.5 py-px text-[11px] font-medium">
              完成
            </span>
          ) : (
            strip.unfinished && (
              <span className="bg-status-warning/16 text-status-warning shrink-0 rounded-[5px] px-1.5 py-px text-[11px] font-medium">
                未完成
              </span>
            )
          )}
          <span className="min-w-0 flex-1 truncate">
            {finished ? "全部步骤已完成" : strip.current}
          </span>
          <span className="text-muted-foreground shrink-0 tabular-nums">
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
            title="隐藏这个计划"
            onClick={() => onHide(strip.runId)}
            className="text-muted-foreground hover:bg-chrome-hover hover:text-foreground focus-visible:ring-node-ring/60 grid size-6.5 shrink-0 place-items-center rounded-md outline-none transition-colors duration-120 focus-visible:ring-2"
          >
            <X className="size-3.5" />
          </button>
        )}
      </div>
    </motion.div>
  );
}
