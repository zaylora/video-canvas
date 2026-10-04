import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

/**
 * 向导步骤条：
 * <Stepper><StepperItem index={1} state="done">选厂商</StepperItem>…</Stepper>
 * state：done 已完成（显示对勾）、current 当前、todo 未到；当前项带 aria-current，不只靠颜色。
 */
function Stepper({ className, ...props }: ComponentProps<"ol">) {
  return (
    <ol
      data-slot="stepper"
      className={cn("flex items-center gap-1 text-xs", className)}
      {...props}
    />
  );
}

function StepperItem({
  className,
  index,
  state,
  children,
  ...props
}: Omit<ComponentProps<"li">, "children"> & {
  /** 步骤序号，从 1 开始 */
  index: number;
  state: "done" | "current" | "todo";
  children: string;
}) {
  return (
    <li
      data-slot="stepper-item"
      data-state={state}
      aria-current={state === "current" ? "step" : undefined}
      className={cn(
        "text-muted-foreground flex min-w-0 flex-1 items-center gap-1.5",
        "data-[state=current]:text-foreground data-[state=current]:font-medium",
        className,
      )}
      {...props}
    >
      <span
        aria-hidden="true"
        className={cn(
          "grid size-5 shrink-0 place-items-center rounded-full border font-mono tabular-nums",
          state === "current" && "border-foreground bg-foreground text-background",
          state === "done" &&
            "border-emerald-500/40 bg-emerald-500/10 text-emerald-700 dark:text-emerald-400",
        )}
      >
        {state === "done" ? "✓" : index}
      </span>
      <span className="truncate">{children}</span>
    </li>
  );
}

export { Stepper, StepperItem };
