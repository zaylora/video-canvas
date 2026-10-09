import { motion } from "motion/react";
import type { ComponentProps } from "react";

import { SPRING } from "@/lib/motion";
import { cn } from "@/lib/utils";

/**
 * 负载条：浅色底层是“生成中 + 排队”，实色上层是“生成中”，满载时换成琥珀色。
 * 条的长短只用 scaleX（origin-left）+ SPRING 过渡，不对 width 做动画。
 * @param running 生成中占整条的比例（0–1）
 * @param queued 生成中 + 排队占整条的比例（0–1），叠在生成中的后面
 * @param full 满载：整条换 warning 色
 * @param label 给读屏的完整描述，如“亚盛生图：生成中 4，排队 5，上限 4”
 */
function LoadBar({
  className,
  running,
  queued,
  full,
  label,
  ...props
}: Omit<ComponentProps<"div">, "children"> & {
  running: number;
  queued: number;
  full?: boolean;
  label: string;
}) {
  return (
    <div
      data-slot="load-bar"
      data-full={full || undefined}
      role="img"
      aria-label={label}
      className={cn("bg-muted relative h-2 overflow-hidden rounded-full", className)}
      {...props}
    >
      <motion.span
        data-slot="load-bar-queued"
        initial={{ scaleX: 0 }}
        animate={{ scaleX: queued }}
        transition={SPRING}
        className={cn(
          "absolute inset-0 origin-left rounded-full",
          full ? "bg-status-warning/30" : "bg-status-running/30",
        )}
      />
      <motion.span
        data-slot="load-bar-running"
        initial={{ scaleX: 0 }}
        animate={{ scaleX: running }}
        transition={SPRING}
        className={cn(
          "absolute inset-0 origin-left rounded-full",
          full ? "bg-status-warning" : "bg-status-running",
        )}
      />
    </div>
  );
}

export { LoadBar };
