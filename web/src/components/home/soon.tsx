import type { ReactElement } from "react";

import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";

/**
 * 还没有后端能力的入口：外面套一层 span 接住悬停（禁用的按钮自己不响应指针），
 * 提示「即将上线」。里面的控件自己负责 disabled。
 */
export function SoonTip({
  children,
  side = "bottom",
  className,
}: {
  children: ReactElement;
  side?: "top" | "bottom" | "left" | "right";
  className?: string;
}) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={<span data-slot="soon" className={className ?? "inline-flex cursor-not-allowed"} />}
      >
        {children}
      </TooltipTrigger>
      <TooltipContent side={side} sideOffset={6}>
        即将上线
      </TooltipContent>
    </Tooltip>
  );
}
