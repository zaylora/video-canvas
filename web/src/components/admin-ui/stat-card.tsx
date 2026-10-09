import { ArrowUpRight } from "lucide-react";
import { motion } from "motion/react";
import type { ComponentProps, ReactNode } from "react";

import { TAP_CARD } from "@/lib/motion";
import { cn } from "@/lib/utils";

const statCardClass =
  "group/stat bg-card text-card-foreground ring-foreground/10 relative flex w-full flex-col gap-1 overflow-hidden rounded-xl p-4 text-left text-sm ring-1";

/**
 * 关键指标卡（总览页）：
 * <StatCard onClick?>
 *   <StatCardLabel><Icon />名称</StatCardLabel>
 *   <StatCardValue>128<StatCardUnit>%</StatCardUnit></StatCardValue>
 *   <StatCardHint tone="warning">说明</StatCardHint>
 *   <StatCardSpark>迷你图</StatCardSpark>
 * </StatCard>
 * 传了 onClick 就是整卡可点的按钮：hover 底色加深并出现右上角箭头，按下压到 TAP_CARD；不传就是静态卡片。
 */
function StatCard({
  className,
  onClick,
  children,
  ...props
}: Omit<ComponentProps<"div">, "onClick" | "children"> & {
  onClick?: () => void;
  children?: ReactNode;
}) {
  if (!onClick) {
    return (
      <div data-slot="stat-card" className={cn(statCardClass, className)} {...props}>
        {children}
      </div>
    );
  }
  return (
    <motion.button
      type="button"
      data-slot="stat-card"
      data-interactive
      whileTap={TAP_CARD}
      onClick={onClick}
      className={cn(
        statCardClass,
        "hover:bg-muted/40 focus-visible:ring-ring cursor-pointer transition-colors focus-visible:ring-2 focus-visible:outline-none",
        className,
      )}
    >
      {children}
      <ArrowUpRight
        aria-hidden
        className="text-muted-foreground absolute top-3 right-3 size-4 opacity-0 transition-opacity group-hover/stat:opacity-100 group-focus-visible/stat:opacity-100"
      />
    </motion.button>
  );
}

/** 指标名：图标 + 文字 */
function StatCardLabel({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="stat-card-label"
      className={cn(
        "text-muted-foreground flex items-center gap-1.5 text-xs [&_svg]:size-3.5",
        className,
      )}
      {...props}
    />
  );
}

/** 指标的大数字，数字本身永远是前景色，状态色只给 StatCardHint */
function StatCardValue({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="stat-card-value"
      className={cn(
        "flex items-baseline gap-1 text-3xl leading-tight font-semibold tabular-nums",
        className,
      )}
      {...props}
    />
  );
}

/** 大数字后面的单位或分母，如 “%”、“/ 12” */
function StatCardUnit({ className, ...props }: ComponentProps<"span">) {
  return (
    <span
      data-slot="stat-card-unit"
      className={cn("text-muted-foreground text-base font-medium", className)}
      {...props}
    />
  );
}

const HINT_TONE = {
  neutral: "text-muted-foreground",
  warning: "text-amber-600 dark:text-amber-400",
  danger: "text-red-600 dark:text-red-400",
} as const;

/** 数字下面的一句说明；有问题时用 warning / danger 语气色 */
function StatCardHint({
  className,
  tone = "neutral",
  ...props
}: ComponentProps<"div"> & { tone?: keyof typeof HINT_TONE }) {
  return (
    <div
      data-slot="stat-card-hint"
      data-tone={tone}
      className={cn("min-h-4.5 text-xs", HINT_TONE[tone], className)}
      {...props}
    />
  );
}

/** 卡片右下角的迷你图位置；窄屏（<640px）隐藏，免得挤住数字 */
function StatCardSpark({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="stat-card-spark"
      aria-hidden
      className={cn("absolute right-4 bottom-3.5 hidden sm:block", className)}
      {...props}
    />
  );
}

export { StatCard, StatCardHint, StatCardLabel, StatCardSpark, StatCardUnit, StatCardValue };
