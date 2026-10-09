import { motion } from "motion/react";
import type { ComponentProps } from "react";

import { TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";

/** 行内操作的语气：文字和图标都用状态色（同 Tag 的语气色：浅色主题 700、深色主题 400），hover 底色取同色 10% */
const toneClasses = {
  neutral: "hover:bg-accent",
  info: "text-sky-700 hover:bg-sky-500/10 hover:text-sky-700 dark:text-sky-400 dark:hover:text-sky-400",
  success:
    "text-emerald-700 hover:bg-emerald-500/10 hover:text-emerald-700 dark:text-emerald-400 dark:hover:text-emerald-400",
  warning:
    "text-amber-700 hover:bg-amber-500/10 hover:text-amber-700 dark:text-amber-400 dark:hover:text-amber-400",
  danger:
    "text-red-700 hover:bg-red-500/10 hover:text-red-700 dark:text-red-400 dark:hover:text-red-400",
} as const;

type RowActionTone = keyof typeof toneClasses;

/**
 * 带文字的轻量按钮，用在表格工具栏里（如「清除筛选」）；表格行内的操作用 RowIconAction。
 * 点击不会冒泡到整行的点击。
 * 禁用时收不到指针事件，用 title 说明原因；按下有回弹，开了「减少动态效果」时 motion 自动去掉缩放。
 */
function RowAction({
  className,
  tone = "neutral",
  onClick,
  ...props
}: Omit<ComponentProps<"button">, "onDrag" | "onDragStart" | "onDragEnd" | "onAnimationStart"> & {
  tone?: RowActionTone;
}) {
  return (
    <motion.button
      type="button"
      data-slot="row-action"
      whileTap={TAP}
      className={cn(
        "text-muted-foreground hover:text-foreground focus-visible:ring-ring/50 inline-flex h-7 shrink-0 items-center gap-1 rounded-md px-2 text-xs font-medium whitespace-nowrap transition-colors outline-none focus-visible:ring-[3px]",
        "disabled:pointer-events-none disabled:opacity-40 [&_svg]:size-3.5 [&_svg]:shrink-0",
        toneClasses[tone],
        className,
      )}
      onClick={(event) => {
        event.stopPropagation();
        onClick?.(event);
      }}
      {...props}
    />
  );
}

export { RowAction };
