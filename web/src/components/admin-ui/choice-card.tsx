import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

/**
 * 卡片式单选项（设计稿里的“模型能力”“所属渠道”）：整块可点，选中加底色与描边。
 * indicator 显示左侧的单选圆点；内容（图标、标题、说明、右侧标签）由 children 决定。
 */
function ChoiceCard({
  className,
  selected,
  indicator,
  children,
  ...props
}: ComponentProps<"button"> & { selected?: boolean; indicator?: boolean }) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={!!selected}
      data-slot="choice-card"
      data-selected={selected || undefined}
      className={cn(
        "flex items-start gap-3 rounded-lg border p-3 text-left transition",
        "hover:bg-accent/50 data-selected:border-foreground/60 data-selected:bg-accent",
        "disabled:pointer-events-none disabled:opacity-50",
        className,
      )}
      {...props}
    >
      {indicator && (
        <span
          aria-hidden="true"
          className={cn(
            "mt-0.5 grid size-4 shrink-0 place-items-center rounded-full border",
            selected ? "border-foreground" : "border-input",
          )}
        >
          {selected && <span className="bg-foreground size-2 rounded-full" />}
        </span>
      )}
      {children}
    </button>
  );
}

/** 一组 ChoiceCard 的容器（radiogroup） */
function ChoiceCardGroup({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      role="radiogroup"
      data-slot="choice-card-group"
      className={cn("grid gap-2", className)}
      {...props}
    />
  );
}

export { ChoiceCard, ChoiceCardGroup };
