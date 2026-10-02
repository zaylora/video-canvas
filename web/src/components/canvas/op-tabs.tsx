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
  options: { value: T; label: string }[];
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
              onClick={() => onValueChange(option.value)}
              className={cn(
                "relative h-7.5 shrink-0 rounded-full px-3.5 text-[13px] whitespace-nowrap transition-colors outline-none",
                "focus-visible:ring-node-ring/60 focus-visible:ring-2 disabled:opacity-50",
                active ? "text-foreground" : "text-muted-foreground hover:text-foreground",
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
