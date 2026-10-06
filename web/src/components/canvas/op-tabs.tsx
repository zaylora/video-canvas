import { LayoutGroup, motion } from "motion/react";

import { SPRING } from "@/lib/motion";
import { cn } from "@/lib/utils";

/**
 * 生成面板顶上的分段 Tabs（设计稿 6.4）：选中块是 layoutId 滑块，在选项间弹簧滑动。
 * id 用节点 id，免得同时开着的两个面板滑块互相飞。
 */
export function OpTabs<T extends string>({
  id,
  value,
  options,
  onValueChange,
  disabled,
}: {
  id: string;
  value: T | undefined;
  /** disabledHint 有值就灰掉该项，鼠标悬浮时用 title 说明原因 */
  options: { value: T; label: string; disabledHint?: string }[];
  onValueChange: (value: T) => void;
  disabled?: boolean;
}) {
  return (
    <LayoutGroup id={`op-tabs-${id}`}>
      <div
        role="tablist"
        aria-label="生成方式"
        className="bg-muted/70 inline-flex max-w-full overflow-x-auto rounded-full p-[3px] [scrollbar-width:none]"
      >
        {options.map((option) => {
          const active = option.value === value;
          return (
            <button
              key={option.value}
              type="button"
              role="tab"
              aria-selected={active}
              disabled={disabled}
              aria-disabled={!!option.disabledHint}
              title={option.disabledHint}
              onClick={() => !option.disabledHint && onValueChange(option.value)}
              className={cn(
                "relative h-7.5 shrink-0 rounded-full px-3.5 text-[13px] whitespace-nowrap transition-colors outline-none",
                "focus-visible:ring-node-ring/60 focus-visible:ring-2 disabled:opacity-50",
                option.disabledHint
                  ? "text-muted-foreground/40 cursor-not-allowed"
                  : active
                    ? "text-foreground"
                    : "text-muted-foreground hover:text-foreground",
              )}
            >
              {active && (
                <motion.span
                  layoutId="op-tab"
                  transition={SPRING}
                  className="bg-background dark:bg-accent absolute inset-0 rounded-full shadow-sm"
                />
              )}
              <span className="relative">{option.label}</span>
            </button>
          );
        })}
      </div>
    </LayoutGroup>
  );
}
