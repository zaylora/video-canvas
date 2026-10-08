import { Check, Hand, MessageCircleQuestion, Sparkles, Trash2, X } from "lucide-react";

import type {
  AgentApprovalDto,
  AgentAskPayload,
  AgentDeletePayload,
  AgentGeneratePayload,
} from "@/api/agent/type";
import type { AgentController } from "@/hooks/use-agent-controller";
import { cn } from "@/lib/utils";
import { useAgentHighlight } from "@/store/agent-highlight";

/** 审批的一句话标题：卡片和记录行共用 */
export function approvalTitle(approval: AgentApprovalDto) {
  switch (approval.kind) {
    case "generate":
      return `申请生成 ${(approval.payload as AgentGeneratePayload).items.length} 个节点`;
    case "delete": {
      const p = approval.payload as AgentDeletePayload;
      return `申请删除 ${p.node_ids.length} 个节点${p.edge_ids.length ? `、${p.edge_ids.length} 条连线` : ""}`;
    }
    case "ask":
      return (approval.payload as AgentAskPayload).question;
  }
}

/** 决定里附带的文字：提问的回答，或拒绝时给 Agent 的说明 */
const answerOf = (approval: AgentApprovalDto) =>
  typeof approval.decision?.answer === "string" ? approval.decision.answer : "";

/**
 * 消息流里的审批 / 提问记录：只占一行，完整的决定框钉在输入框的位置。
 * 等你决定时带一个小圆点；决定后写明结果（拒绝的理由、你的回答跟在后面）。删除类悬停时画布上标红被删的节点。
 */
export function ApprovalRow({ approvalId, ctl }: { approvalId: string; ctl: AgentController }) {
  const approval = ctl.state.approvals[approvalId];
  if (!approval) return null;
  const title = approvalTitle(approval);
  const answer = answerOf(approval);
  const pending = approval.status === "pending";
  const ask = approval.kind === "ask";

  let icon = pending ? (
    approval.kind === "generate" ? (
      <Sparkles className="text-status-warning" />
    ) : approval.kind === "delete" ? (
      <Trash2 className="text-destructive" />
    ) : (
      <MessageCircleQuestion className="text-status-running" />
    )
  ) : (
    <Check />
  );
  let text: string;
  if (pending) text = ask ? `向你提问：${title}` : title;
  else if (ask) text = answer ? `你的回答：${answer}` : `没有回答：${title}`;
  else {
    switch (approval.status) {
      case "rejected":
        icon = <X />;
        text = `已拒绝：${title}${answer ? ` · 你的说明：${answer}` : ""}`;
        break;
      case "expired":
        icon = <X />;
        text = `已失效：${title}`;
        break;
      case "failed":
        icon = <X className="text-destructive" />;
        text = `已批准，但执行失败：${title}`;
        break;
      case "partially_approved":
        text = `已部分批准：${title}`;
        break;
      default:
        text = `已批准：${title}`;
    }
  }
  const quote =
    approval.kind === "generate" && approval.quote_credits > 0 ? approval.quote_credits : null;

  return (
    <div
      className="text-muted-foreground flex items-center gap-2 text-[13px] [&_svg]:size-3.5 [&_svg]:shrink-0"
      onMouseEnter={() =>
        pending &&
        approval.kind === "delete" &&
        useAgentHighlight.getState().setDanger((approval.payload as AgentDeletePayload).node_ids)
      }
      onMouseLeave={() => approval.kind === "delete" && useAgentHighlight.getState().setDanger([])}
    >
      {pending && <span className="bg-status-warning size-1.5 shrink-0 rounded-full" />}
      {icon}
      <span className={cn("min-w-0 truncate", pending && "text-foreground")}>{text}</span>
      {quote !== null && <span className="text-credit shrink-0 tabular-nums">· ✦ {quote}</span>}
      {pending && (
        <span className="flex shrink-0 items-center gap-1">
          · 等你{ask ? "回答" : "确认"}
          <Hand />
        </span>
      )}
    </div>
  );
}
