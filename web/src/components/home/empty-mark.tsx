import type { ReactNode } from "react";

import { Logo } from "@/components/brand/logo";
import { cn } from "@/lib/utils";

/**
 * 空状态：淡色的连镜 Logo + 标题 + 一句说明，居中。
 * 资产页没有内容、对话还没有记录时共用。
 */
export function EmptyMark({
  title,
  hint,
  className,
}: {
  title: string;
  hint?: ReactNode;
  className?: string;
}) {
  return (
    <div
      data-slot="empty-mark"
      className={cn(
        "text-muted-foreground flex min-h-[46vh] flex-col items-center justify-center gap-1.5 text-[13px]",
        className,
      )}
    >
      <Logo size={72} className="text-foreground/20 mb-2.5" />
      <b className="text-foreground text-sm font-medium">{title}</b>
      {hint && <span>{hint}</span>}
    </div>
  );
}
