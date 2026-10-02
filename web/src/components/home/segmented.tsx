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
 * 分段控件：选中项底下的灰块用 layoutId 跟着点击位置滑过去。
 * 首页的「全部 / 个人 / 协作」和所有画布页的疏密切换共用。
 */
export function Segmented<T extends string>({
  options,
  value,
  onChange,
  size = "sm",
  className,
}: {
  options: SegmentedOption<T>[];
  value: T;
  onChange: (value: T) => void;
  /** sm 是 h-8，md 是 h-9 */
  size?: "sm" | "md";
  className?: string;
}) {
  const layoutId = useId();

  return (
    <div
      role="tablist"
      data-slot="segmented"
      className={cn("inline-flex items-center gap-0.5", className)}
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
              "text-muted-foreground relative inline-flex items-center justify-center rounded-lg px-3 whitespace-nowrap outline-none select-none",
              "focus-visible:ring-ring/50 transition-colors duration-150 focus-visible:ring-3",
              "hover:text-foreground disabled:pointer-events-none disabled:opacity-50 [&_svg]:size-4",
              size === "md" ? "h-9 text-[15px]" : "h-8",
              selected && "text-foreground font-medium",
            )}
          >
            {selected && (
              <motion.span
                layoutId={layoutId}
                transition={SPRING}
                className="bg-muted absolute inset-0 rounded-lg"
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
