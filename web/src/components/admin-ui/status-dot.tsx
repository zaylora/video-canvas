import type { ComponentProps } from "react";
import { cva, type VariantProps } from "class-variance-authority";

import { cn } from "@/lib/utils";

const statusDotVariants = cva("size-1.5 shrink-0 rounded-full", {
  variants: {
    tone: {
      success: "bg-emerald-500",
      warning: "bg-amber-500",
      danger: "bg-red-500",
      info: "bg-sky-500",
      neutral: "bg-zinc-400",
    },
  },
  defaultVariants: { tone: "neutral" },
});

const statusLabelVariants = cva("inline-flex shrink-0 items-center gap-1.5 text-xs", {
  variants: {
    tone: {
      success: "text-emerald-600 dark:text-emerald-400",
      warning: "text-amber-600 dark:text-amber-400",
      danger: "text-red-600 dark:text-red-400",
      info: "text-sky-600 dark:text-sky-400",
      neutral: "text-muted-foreground",
    },
  },
  defaultVariants: { tone: "neutral" },
});

/** 状态圆点 */
function StatusDot({
  className,
  tone,
  ...props
}: ComponentProps<"span"> & VariantProps<typeof statusDotVariants>) {
  return (
    <span
      data-slot="status-dot"
      aria-hidden="true"
      className={cn(statusDotVariants({ tone }), className)}
      {...props}
    />
  );
}

/** 圆点 + 文字的状态，如“● 启用”（文字本身表达状态，不只靠颜色） */
function StatusLabel({
  className,
  tone,
  children,
  ...props
}: ComponentProps<"span"> & VariantProps<typeof statusLabelVariants>) {
  return (
    <span
      data-slot="status-label"
      className={cn(statusLabelVariants({ tone }), className)}
      {...props}
    >
      <StatusDot tone={tone} />
      {children}
    </span>
  );
}

export { StatusDot, StatusLabel, statusDotVariants };
