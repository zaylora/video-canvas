import { useId, type ReactNode } from "react";
import { motion } from "motion/react";

import { SoonTip } from "@/components/home/soon";
import { SPRING, TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";

/** 分段控件的一项 */
export type SegmentedOption<T extends string> = {
  /** 选项值 */
  value: T;
  /** 显示内容：文字或图标 */
  label: ReactNode;
  /** 读屏用的名字，label 是图标时必填 */
  ariaLabel?: string;
  /** 还没有功能：禁用并提示「即将上线」 */
  soon?: boolean;
};

/**
 * 分段控件的三种外观：
 * flat 没有底，只有选中项的灰块（所有画布页的疏密切换）；
 * track 有一条灰色轨道，选中项是浮起的白块（资产页的范围切换）；
 * pill 是大号胶囊，选中块反色并带一抹琥珀光（创作页的「生成 / 画布」）。
 */
export type SegmentedVariant = "flat" | "track" | "pill";

/** 各外观的外框、按钮、选中态和滑块样式 */
const VARIANT = {
  flat: {
    root: "gap-0.5",
    button: "rounded-lg px-3",
    selected: "text-foreground font-medium",
    thumb: "bg-muted rounded-lg",
  },
  track: {
    root: "bg-muted gap-0 rounded-[10px] p-[3px]",
    button: "h-7! rounded-lg px-3.5 text-[13px]",
    selected: "text-foreground font-medium",
    thumb: "bg-background rounded-lg shadow-sm",
  },
  pill: {
    root: "bg-muted w-[min(320px,100%)] gap-0 rounded-full p-1",
    button: "h-9! flex-1 rounded-full text-sm",
    selected: "text-background font-medium",
    thumb:
      "bg-foreground rounded-full bg-[radial-gradient(ellipse_60%_120%_at_18%_50%,color-mix(in_oklch,var(--beam-bright)_55%,transparent),transparent_70%)]",
  },
} as const;

/**
 * 分段控件：选中项底下的滑块用 layoutId 跟着点击位置滑过去。
 * 首页的「全部 / 个人 / 协作」、所有画布页的疏密切换、创作页的模式切换和资产页的范围切换共用。
 */
export function Segmented<T extends string>({
  options,
  value,
  onChange,
  size = "sm",
  variant = "flat",
  label,
  className,
}: {
  options: SegmentedOption<T>[];
  value: T;
  onChange: (value: T) => void;
  /** sm 是 h-8，md 是 h-9；track 和 pill 的高度由外观决定 */
  size?: "sm" | "md";
  variant?: SegmentedVariant;
  /** 整个控件的读屏名字 */
  label?: string;
  className?: string;
}) {
  const layoutId = useId();
  const style = VARIANT[variant];

  return (
    <div
      role="tablist"
      aria-label={label}
      data-slot="segmented"
      data-variant={variant}
      className={cn("inline-flex items-center", style.root, className)}
    >
      {options.map((option) => {
        const selected = option.value === value;
        const button = (
          <motion.button
            key={option.value}
            type="button"
            role="tab"
            aria-selected={selected}
            aria-label={option.ariaLabel}
            disabled={option.soon}
            whileTap={option.soon ? undefined : TAP}
            onClick={() => onChange(option.value)}
            className={cn(
              "text-muted-foreground relative inline-flex items-center justify-center whitespace-nowrap outline-none select-none",
              "focus-visible:ring-ring/50 transition-colors duration-150 focus-visible:ring-3",
              "hover:text-foreground disabled:pointer-events-none disabled:opacity-50 [&_svg]:size-4",
              size === "md" ? "h-9 text-[15px]" : "h-8",
              style.button,
              selected && style.selected,
              /** 选中的滑块是反色底时，hover 不能把字色又压回前景色 */
              selected && variant === "pill" && "hover:text-background",
            )}
          >
            {selected && (
              <motion.span
                layoutId={layoutId}
                transition={SPRING}
                className={cn("absolute inset-0", style.thumb)}
              />
            )}
            <span className="relative inline-flex items-center gap-1.5">{option.label}</span>
          </motion.button>
        );
        return option.soon ? <SoonTip key={option.value}>{button}</SoonTip> : button;
      })}
    </div>
  );
}
