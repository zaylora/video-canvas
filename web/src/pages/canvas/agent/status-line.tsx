import { useEffect, useState } from "react";
import { motion, useReducedMotion } from "motion/react";
import { Hand } from "lucide-react";

import { DURATION, EASE_OUT } from "@/lib/motion";
import type { StatusLine as StatusLineModel } from "@/utils/agent/timeline";

import { ShimmerText } from "./shimmer-text";

/** 从 since 起过了多少秒，每秒刷新一次；不计时（since 为 null）时为 null */
function useElapsed(since: number | null) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (since === null) return;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [since]);
  return since === null ? null : Math.max(1, Math.round((now - since) / 1000));
}

/**
 * 消息流末尾的状态行：运行中唯一的实时指示（流光和计时只在这里）。
 * 运行中：品牌小圆点 + 流光文字「思考中… / 正在{工具}…」+ 秒数 + 「Esc 停止」；
 * 等你决定：手形图标 + 静态的「等你确认 ↓」，决定框就在下方。
 */
export function StatusLine({ line }: { line: StatusLineModel }) {
  const reduce = useReducedMotion();
  const seconds = useElapsed(line.waiting ? null : line.since);
  return (
    <motion.div
      role="status"
      initial={reduce ? { opacity: 0 } : { opacity: 0, y: 4 }}
      animate={{ opacity: 1, y: 0 }}
      exit={
        reduce
          ? { opacity: 0, transition: { duration: DURATION.exit } }
          : { opacity: 0, y: 4, transition: { duration: DURATION.exit } }
      }
      transition={{ duration: DURATION.base, ease: EASE_OUT }}
      className="text-muted-foreground flex min-h-5 items-center gap-1.5 text-[12.5px]"
    >
      {line.waiting ? (
        <>
          <Hand className="text-status-warning size-3.5 shrink-0" />
          <span>{line.text} ↓</span>
        </>
      ) : (
        <>
          <span
            aria-hidden
            className="agent-orb size-2 shrink-0 rounded-full shadow-[0_0_8px_color-mix(in_oklab,var(--preset)_60%,transparent)]"
          />
          <ShimmerText>{line.text}</ShimmerText>
          {seconds !== null && <span className="tabular-nums">{seconds}s</span>}
          <span className="ml-auto flex items-center gap-1 text-[11px] opacity-80">
            <kbd className="ring-chrome-border bg-foreground/5 inline-grid h-[18px] min-w-[18px] place-items-center rounded-[5px] px-1 font-sans text-[11px] ring-1">
              Esc
            </kbd>
            停止
          </span>
        </>
      )}
    </motion.div>
  );
}
