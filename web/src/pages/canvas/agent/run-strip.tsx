import { useState } from "react";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { Check, ChevronUp, Circle, Hand, Loader2, X } from "lucide-react";

import { DURATION, EASE_OUT } from "@/lib/motion";
import { cn } from "@/lib/utils";
import type { RunStrip as RunStripModel } from "@/utils/agent/timeline";

/** 进度环：已完成占比，动画只改 stroke-dashoffset */
function Ring({ done, total }: { done: number; total: number }) {
  const r = 7;
  const c = 2 * Math.PI * r;
  return (
    <svg viewBox="0 0 18 18" className="size-[18px] shrink-0 -rotate-90" aria-hidden>
      <circle cx="9" cy="9" r={r} fill="none" strokeWidth="2" className="stroke-foreground/15" />
      <circle
        cx="9"
        cy="9"
        r={r}
        fill="none"
        strokeWidth="2"
        strokeLinecap="round"
        className="stroke-status-running transition-[stroke-dashoffset] duration-[240ms] ease-(--motion-ease)"
        strokeDasharray={c}
        strokeDashoffset={c * (1 - (total ? done / total : 0))}
      />
    </svg>
  );
}

/**
 * 置顶运行条：悬浮在输入框上方。有计划显示进度和当前一步，没有计划显示运行状态；
 * 计划没做完而运行已结束时显示「未完成」，✕ 隐藏。点击向上展开完整步骤（浮在消息流上方，不挤压布局）。
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
  const waiting = strip.prefix !== "";
  const label = strip.unfinished
    ? `未完成：${strip.current}`
    : [
        strip.prefix && `${strip.prefix} · `,
        strip.progress ? `${strip.progress.done}/${strip.progress.total} · ` : "",
        strip.current ? `正在：${strip.current}` : waiting ? "" : "思考中…",
      ].join("");

  return (
    <motion.div
      initial={reduce ? false : { opacity: 0, y: 6 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: DURATION.base, ease: EASE_OUT }}
      className="relative mx-3 mb-2"
    >
      <AnimatePresence>
        {open && strip.steps.length > 0 && (
          <motion.ul
            initial={reduce ? { opacity: 0 } : { opacity: 0, y: 4, scale: 0.96 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={
              reduce
                ? { opacity: 0 }
                : { opacity: 0, y: 4, scale: 0.96, transition: { duration: DURATION.exit } }
            }
            transition={{ duration: DURATION.base, ease: EASE_OUT }}
            style={{ transformOrigin: "bottom" }}
            className="bg-popover ring-chrome-border absolute right-0 bottom-full left-0 mb-1.5 flex max-h-56 flex-col gap-1.5 overflow-y-auto rounded-xl p-3 text-xs shadow-lg ring-1"
          >
            {strip.steps.map((s, i) => (
              <li key={i} className="flex items-start gap-2">
                {s.status === "done" ? (
                  <Check className="text-status-success mt-0.5 size-3.5 shrink-0" />
                ) : s.status === "doing" ? (
                  <Loader2
                    className={cn(
                      "text-status-running mt-0.5 size-3.5 shrink-0",
                      strip.active && "animate-spin",
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
      <div className="bg-foreground/5 ring-chrome-border flex h-9 items-center gap-2 rounded-xl px-2.5 text-xs ring-1">
        <button
          type="button"
          aria-expanded={open}
          aria-label="展开计划"
          disabled={strip.steps.length === 0}
          onClick={() => setOpen((v) => !v)}
          className="focus-visible:ring-node-ring/60 flex min-w-0 flex-1 items-center gap-2 rounded-md text-left outline-none focus-visible:ring-2"
        >
          {strip.progress ? (
            <Ring done={strip.progress.done} total={strip.progress.total} />
          ) : waiting ? (
            <Hand className="text-status-running size-4 shrink-0" />
          ) : (
            <Loader2
              className={cn("text-muted-foreground size-4 shrink-0", !reduce && "animate-spin")}
            />
          )}
          <span className="truncate">{label}</span>
          {strip.steps.length > 0 && (
            <ChevronUp
              className={cn(
                "text-muted-foreground ml-auto size-3.5 shrink-0 transition-transform duration-120",
                open && "rotate-180",
              )}
            />
          )}
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
