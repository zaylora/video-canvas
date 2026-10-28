import type { ReactNode } from "react";

import { cn } from "@/lib/utils";
import type { BadgeTone } from "@/utils/drama/episode-cards";

const TONE: Record<BadgeTone, string> = {
  success: "bg-status-success/15 text-status-success",
  running: "bg-status-running/15 text-status-running",
  warning: "bg-status-warning/15 text-status-warning",
  destructive: "bg-destructive/10 text-destructive",
  muted: "bg-muted text-muted-foreground",
};

/** 小徽标：分集卡片、片段状态共用，颜色只用语义 token */
export function StatusPill({
  tone,
  className,
  children,
}: {
  tone: BadgeTone;
  className?: string;
  children: ReactNode;
}) {
  return (
    <span
      className={cn(
        "inline-flex h-5 shrink-0 items-center gap-1 rounded-full px-2 text-[11px] leading-none font-medium [&_svg]:size-3",
        TONE[tone],
        className,
      )}
    >
      {children}
    </span>
  );
}
