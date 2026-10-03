import type { ComponentProps } from "react";
import { Lock } from "lucide-react";

import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

/**
 * 锁定提示：放在字段标题旁，鼠标悬停或键盘聚焦时说明为什么这个字段不能改。
 * 被禁用的输入框收不到 hover，所以提示挂在标题旁的锁图标上。
 * <FormField label={<>Bucket<LockHint reason="已有素材引用" /></>} />
 */
function LockHint({
  reason,
  className,
  ...props
}: Omit<ComponentProps<"span">, "children"> & {
  /** 锁定原因，同时作为读屏文本 */
  reason: string;
}) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <span
            data-slot="lock-hint"
            tabIndex={0}
            role="img"
            aria-label={reason}
            className={cn(
              "text-muted-foreground inline-flex rounded-sm outline-none focus-visible:ring-3 focus-visible:ring-ring/50",
              className,
            )}
            {...props}
          />
        }
      >
        <Lock className="size-3.5" />
      </TooltipTrigger>
      <TooltipContent className="max-w-64 leading-relaxed">{reason}</TooltipContent>
    </Tooltip>
  );
}

export { LockHint };
