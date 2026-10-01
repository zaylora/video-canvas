import type { ComponentProps } from "react";
import { Check } from "lucide-react";

import { cn } from "@/lib/utils";

/** 可选中的小片（设计稿 chip）：选中是反色底 + 对勾，用于时长、比例这类少量选项 */
function ToggleChip({
  className,
  pressed,
  children,
  ...props
}: ComponentProps<"button"> & { pressed?: boolean }) {
  return (
    <button
      type="button"
      aria-pressed={!!pressed}
      data-slot="toggle-chip"
      data-pressed={pressed || undefined}
      className={cn(
        "border-input bg-background text-muted-foreground inline-flex h-8 items-center gap-1.5 rounded-md border px-3 text-xs font-medium transition",
        "hover:text-foreground data-pressed:border-foreground/80 data-pressed:bg-foreground data-pressed:text-background",
        "disabled:pointer-events-none disabled:opacity-50 [&>svg]:size-3.5",
        className,
      )}
      {...props}
    >
      {pressed && <Check />}
      {children}
    </button>
  );
}

export { ToggleChip };
