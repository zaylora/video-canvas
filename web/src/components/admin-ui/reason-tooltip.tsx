import type { ReactElement } from "react";

import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

/**
 * 给“被禁用的操作”说明原因：禁用的按钮收不到 hover，所以用外面一层 span 当触发器，键盘也能聚焦看到。
 * reason 为空表示操作可用，直接渲染按钮本身，不多包一层。
 * <ReasonTooltip reason="默认存储不可删除"><Button disabled>删除</Button></ReasonTooltip>
 */
function ReasonTooltip({
  reason,
  children,
  className,
}: {
  /** 禁用原因；为空或 null 表示可用 */
  reason?: string | null;
  children: ReactElement;
  className?: string;
}) {
  if (!reason) return children;
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <span
            data-slot="reason-tooltip"
            tabIndex={0}
            className={cn(
              "inline-flex rounded-md outline-none focus-visible:ring-3 focus-visible:ring-ring/50",
              className,
            )}
          />
        }
      >
        {children}
      </TooltipTrigger>
      <TooltipContent className="max-w-64 leading-relaxed">{reason}</TooltipContent>
    </Tooltip>
  );
}

export { ReasonTooltip };
