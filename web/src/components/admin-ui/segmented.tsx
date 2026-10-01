import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

/**
 * 分段选择（设计稿的 seg）：灰底里一排按钮，选中的浮起来。
 * <Segmented aria-label="能力"><SegmentedItem active>全部</SegmentedItem>…</Segmented>
 */
function Segmented({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      role="group"
      data-slot="segmented"
      className={cn("bg-muted inline-flex gap-0.5 rounded-lg p-0.5 text-xs", className)}
      {...props}
    />
  );
}

function SegmentedItem({
  className,
  active,
  ...props
}: ComponentProps<"button"> & { active?: boolean }) {
  return (
    <button
      type="button"
      aria-pressed={!!active}
      data-slot="segmented-item"
      data-active={active || undefined}
      className={cn(
        "text-muted-foreground hover:text-foreground rounded-md px-3 py-1.5 font-medium",
        "data-active:bg-background data-active:text-foreground data-active:shadow-sm",
        "disabled:cursor-not-allowed disabled:opacity-40",
        className,
      )}
      {...props}
    />
  );
}

export { Segmented, SegmentedItem };
