import type { CSSProperties, ReactNode } from "react";

import { SHIMMER } from "@/lib/motion";
import { cn } from "@/lib/utils";

/**
 * 流光文字：高光沿文字从左到右扫过，表示「正在进行」。只用于生成中的状态指示（规范允许的循环动画）；
 * 减少动态效果时由 agent-shimmer 工具类降级为静态的次要文字。
 */
export function ShimmerText({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <span
      className={cn("agent-shimmer", className)}
      style={{ "--shimmer": `${SHIMMER}s` } as CSSProperties}
    >
      {children}
    </span>
  );
}
