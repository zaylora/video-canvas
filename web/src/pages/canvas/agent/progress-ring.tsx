import { cn } from "@/lib/utils";

/**
 * 18px 进度环：value / total 的占比。动画只改 stroke-dashoffset（DURATION.slow）。
 * 颜色由 className 给到 stroke-*，比如计划用 stroke-status-running、预算用 stroke-credit。
 */
export function ProgressRing({
  value,
  total,
  className,
}: {
  value: number;
  total: number;
  className?: string;
}) {
  const r = 7;
  const c = 2 * Math.PI * r;
  const ratio = total > 0 ? Math.min(1, Math.max(0, value / total)) : 0;
  return (
    <svg viewBox="0 0 18 18" className="size-[18px] shrink-0 -rotate-90" aria-hidden>
      <circle cx="9" cy="9" r={r} fill="none" strokeWidth="2" className="stroke-foreground/15" />
      <circle
        cx="9"
        cy="9"
        r={r}
        fill="none"
        strokeWidth="2"
        strokeLinecap="round"
        className={cn(
          "transition-[stroke-dashoffset,stroke] duration-[240ms] ease-(--motion-ease)",
          className,
        )}
        strokeDasharray={c}
        strokeDashoffset={c * (1 - ratio)}
      />
    </svg>
  );
}
