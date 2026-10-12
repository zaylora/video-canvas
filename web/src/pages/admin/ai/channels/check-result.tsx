import { CheckCircle2, CircleAlert, Loader2, MinusCircle, TriangleAlert } from "lucide-react";
import { toast } from "sonner";

import { CopyButton } from "@/components/admin-ui/copy-button";
import type { CheckOutcome } from "@/utils/admin/channel-check";
import {
  buildChannelCheckRepairReport,
  REPAIR_COPY_FAILED,
  type RepairPlugin,
} from "@/utils/admin/repair-report";

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
 * 不通（failed）、插件本身出错（plugin-fault，钩子抛异常等）且有说明时，在弹窗里多一个“复制修复信息”：
 * 插件与版本加检查返回的说明，整理成一段文本发给 AI。
 * @param plugin 被检查的渠道所用的插件与版本，拿不到就不传
 */
export function CheckResult({
  state,
  inline,
  className,
  plugin,
}: {
  state: CheckState | undefined;
  inline?: boolean;
  className?: string;
  plugin?: RepairPlugin | null;
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
  // 不通、插件本身出错且有说明可带时，多一个“复制修复信息”
  const repairable =
    (outcome.kind === "failed" || outcome.kind === "plugin-fault") && !!outcome.message;
  if (!repairable) return <CheckStatus outcome={outcome} className={className} />;
  return (
    <div className={cn("flex flex-wrap items-center gap-x-3 gap-y-1", className)}>
      <CheckStatus outcome={outcome} />
      <CopyButton
        variant="outline"
        text={buildChannelCheckRepairReport({ plugin, message: outcome.message })}
        label="复制修复信息"
        onError={() => toast.error(REPAIR_COPY_FAILED)}
      />
    </div>
  );
}

/** 弹窗里的检查结果：一行图标加结论，下面是补充说明 */
function CheckStatus({ outcome, className }: { outcome: CheckOutcome; className?: string }) {
  const { text, icon: Icon } = TONE[outcome.tone];
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
