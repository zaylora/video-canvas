import { CheckCircle2, CircleAlert, Loader2, MinusCircle, TriangleAlert } from "lucide-react";

import type { CheckOutcome } from "@/utils/admin/channel-check";

import { Tag, type TagTone } from "../shared";
import type { CheckState } from "./use-channel-check";

const TONE: Record<CheckOutcome["tone"], { tag: TagTone; icon: typeof CheckCircle2 }> = {
  success: { tag: "success", icon: CheckCircle2 },
  neutral: { tag: "neutral", icon: MinusCircle },
  warning: { tag: "warning", icon: TriangleAlert },
  danger: { tag: "danger", icon: CircleAlert },
};

/**
 * 检查结果：图标加文字，不只靠颜色。
 * compact 用于列表行（一个小标签，说明放 title）；否则内联展开说明（抽屉里）。
 */
export function CheckResult({ state, compact }: { state: CheckState | undefined; compact?: boolean }) {
  if (!state) return null;
  if (state.busy) {
    return (
      <span className="text-muted-foreground inline-flex items-center gap-1 text-xs" role="status">
        <Loader2 className="size-3 animate-spin" />
        检查中…
      </span>
    );
  }
  const { outcome } = state;
  const { tag, icon: Icon } = TONE[outcome.tone];
  if (compact) {
    return (
      <Tag tone={tag} title={[outcome.title, outcome.detail].filter(Boolean).join("：")}>
        <Icon className="size-3" />
        {outcome.title}
      </Tag>
    );
  }
  return (
    <div className="flex flex-col gap-0.5 text-xs" role="status" data-check-kind={outcome.kind}>
      <Tag tone={tag} className="self-start">
        <Icon className="size-3" />
        {outcome.title}
      </Tag>
      {outcome.detail && <span className="text-muted-foreground">{outcome.detail}</span>}
    </div>
  );
}
