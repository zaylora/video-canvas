import { CheckCircle2, CircleAlert, Loader2, MinusCircle, TriangleAlert } from "lucide-react";

import type { CheckOutcome } from "@/utils/admin/channel-check";

import { cn } from "@/lib/utils";
import type { CheckState } from "./use-channel-check";

/** 检查结果的图标和文字色（和状态列的 StatusLabel 同一套，不另起颜色） */
const TONE: Record<CheckOutcome["tone"], { text: string; icon: typeof CheckCircle2 }> = {
  success: { text: "text-emerald-600 dark:text-emerald-400", icon: CheckCircle2 },
  neutral: { text: "text-muted-foreground", icon: MinusCircle },
  warning: { text: "text-amber-600 dark:text-amber-400", icon: TriangleAlert },
  danger: { text: "text-red-600 dark:text-red-400", icon: CircleAlert },
};

/**
 * 检查结果：图标加文字，不只靠颜色。
 * inline 用于表格状态列的第二行（一行小字，说明放 title）；否则在弹窗里展开说明。
 */
export function CheckResult({
  state,
  inline,
  className,
}: {
  state: CheckState | undefined;
  inline?: boolean;
  className?: string;
}) {
  if (!state) return null;
  if (state.busy) {
    return (
      <span
        className={cn("text-muted-foreground inline-flex items-center gap-1 text-xs", className)}
        role="status"
      >
        <Loader2 className="size-3 animate-spin" />
        检查中…
      </span>
    );
  }
  const { outcome } = state;
  const { text, icon: Icon } = TONE[outcome.tone];
  if (inline) {
    return (
      <span
        role="status"
        data-check-kind={outcome.kind}
        title={[outcome.title, outcome.detail].filter(Boolean).join("：")}
        className={cn("inline-flex items-center gap-1 text-xs", text, className)}
      >
        <Icon className="size-3 shrink-0" />
        <span className="truncate">{outcome.title}</span>
      </span>
    );
  }
  return (
    <div
      className={cn("flex flex-col gap-0.5 text-xs", className)}
      role="status"
      data-check-kind={outcome.kind}
    >
      <span className={cn("inline-flex items-center gap-1 font-medium", text)}>
        <Icon className="size-3.5" />
        {outcome.title}
      </span>
      {outcome.detail && <span className="text-muted-foreground">{outcome.detail}</span>}
    </div>
  );
}
