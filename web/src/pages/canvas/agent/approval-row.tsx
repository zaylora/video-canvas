import {
  Check,
  ClipboardList,
  CornerDownRight,
  FilePen,
  Hand,
  MessageCircleQuestion,
  KeyRound,
  Play,
  Sparkles,
  Trash2,
  Undo2,
  X,
} from "lucide-react";

import type {
  AgentApprovalDto,
  AgentAskPayload,
  AgentDeletePayload,
  AgentGeneratePayload,
  AdminSecretPayload,
  AdminSecretResult,
  AdminTestRunPayload,
  AdminTestRunResult,
} from "@/api/agent/type";
import type { AgentController } from "@/hooks/use-agent-controller";
import { cn } from "@/lib/utils";
import { useAgentHighlight } from "@/store/agent-highlight";
import {
  adminChangeTitle,
  canRollback,
  adminChangePayload,
  resultSummary,
  rollbackMessage,
} from "@/utils/admin/assistant/change-order";
import { editPayload, editTitle } from "@/utils/agent/drama";
import { formatSeconds } from "@/utils/agent/activity";

/** 删除审批的内容；服务端的空列表可能是 null（旧数据），统一补成空数组 */
export function deletePayload(raw: unknown): AgentDeletePayload {
  const p = (raw ?? {}) as Partial<AgentDeletePayload>;
  return {
    reason: p.reason ?? "",
    node_ids: p.node_ids ?? [],
    labels: p.labels ?? [],
    outputs: p.outputs ?? [],
    edge_ids: p.edge_ids ?? [],
  };
}

/** 审批的一句话标题：卡片和记录行共用 */
export function approvalTitle(approval: AgentApprovalDto) {
  switch (approval.kind) {
    case "generate":
      return `申请生成 ${(approval.payload as AgentGeneratePayload).items.length} 个节点`;
    case "delete": {
      const p = deletePayload(approval.payload);
      return `申请删除 ${p.node_ids.length} 个节点${p.edge_ids.length ? `、${p.edge_ids.length} 条连线` : ""}`;
    }
    case "ask":
      return (approval.payload as AgentAskPayload).question;
    // 后台助手的变更单
    case "admin_change":
      return adminChangeTitle(approval);
    case "admin_secret":
      return `填写密钥：${(approval.payload as AdminSecretPayload).channel_label}`;
    case "admin_test_run":
      return `申请试跑：${(approval.payload as AdminTestRunPayload).model_label}`;
    // 短剧助手的修改审批；不认识的种类也按它显示
    default:
      return editTitle(editPayload(approval.payload));
  }
}

/** 决定里附带的文字：提问的回答，或拒绝时给 Agent 的说明 */
const answerOf = (approval: AgentApprovalDto) =>
  approval.kind !== "admin_secret" && typeof approval.decision?.answer === "string"
    ? approval.decision.answer
    : "";

/** 结果链接允许 http(s) 或站内绝对路径，禁止脚本与协议相对地址 */
const safeResultLink = (url?: string) =>
  url && (/^https?:\/\//i.test(url) || /^\/(?![/\\])/.test(url)) ? url : undefined;

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
  const adminChange = approval.kind === "admin_change";
  const secret = approval.kind === "admin_secret";
  const trial = approval.kind === "admin_test_run";
  const trialResult = trial ? (approval.result as AdminTestRunResult | null) : null;

  let icon = pending ? (
    approval.kind === "generate" ? (
      <Sparkles className="text-status-warning" />
    ) : approval.kind === "delete" ? (
      <Trash2 className="text-destructive" />
    ) : ask ? (
      <MessageCircleQuestion className="text-status-warning" />
    ) : adminChange ? (
      <ClipboardList className="text-status-warning" />
    ) : secret ? (
      <KeyRound className="text-status-warning" />
    ) : trial ? (
      <Play className="text-status-warning" />
    ) : (
      <FilePen className="text-status-warning" />
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
    // 变更单决定之后写明结果：几项已应用 / 失败 / 跳过、谁在什么时候应用的
    const summary = adminChange && approval.status !== "rejected" ? resultSummary(approval) : null;
    if (summary) text = `变更单 ${adminChangePayload(approval)?.order_no ?? ""} · ${summary}`;
  }
  if (secret && !pending) {
    const configured = (approval.result as AdminSecretResult | null)?.configured;
    text =
      approval.status === "rejected"
        ? "已跳过"
        : configured
          ? "已保存密钥"
          : approval.status === "failed" || configured === false
            ? "密钥未保存"
            : approval.status === "expired"
              ? "密钥请求已失效"
              : "正在确认密钥";
  }
  if (trial && !pending) {
    const state = trialResult?.status;
    const outcome =
      state === "succeeded"
        ? "试跑成功"
        : state === "timeout"
          ? /** 后端等满十分钟就交回结果，上游任务没有取消、仍在跑，不算失败 */
            "试跑仍在运行（已等 10 分钟）"
          : state === "failed" || approval.status === "failed"
            ? "试跑失败"
            : approval.status === "rejected"
              ? "未试跑"
              : approval.status === "expired"
                ? "试跑请求已失效"
                : "仍在运行";
    text = [
      outcome,
      (approval.payload as AdminTestRunPayload).model_label,
      trialResult?.duration_ms !== undefined
        ? `耗时 ${formatSeconds(trialResult.duration_ms / 1000)}`
        : null,
      trialResult?.error_class ? `错误分类：${trialResult.error_class}` : null,
      trialResult?.error,
    ]
      .filter(Boolean)
      .join(" · ");
    if (state === "failed") icon = <X className="text-destructive" />;
  }
  const quote =
    !approval.kind.startsWith("admin_") &&
    approval.kind !== "ask" &&
    approval.kind !== "delete" &&
    approval.quote_credits > 0
      ? approval.quote_credits
      : null;

  // 答过的提问（设计稿 6.12）：「↳ 问题  答案」挂在过程轨上，问题淡、答案实
  if (ask && !pending && answer)
    return (
      <div
        aria-label={`你的回答：${answer}`}
        className="text-muted-foreground flex h-7 min-w-0 items-center gap-2 text-[12.5px] whitespace-nowrap [&_svg]:size-3.5 [&_svg]:shrink-0"
      >
        <CornerDownRight />
        <span className="max-w-[55%] truncate">{title}</span>
        <span className="text-foreground min-w-0 truncate font-medium">{answer}</span>
      </div>
    );

  return (
    <div
      className="text-muted-foreground flex items-center gap-2 text-[13px] [&_svg]:size-3.5 [&_svg]:shrink-0"
      onMouseEnter={() =>
        pending &&
        approval.kind === "delete" &&
        useAgentHighlight.getState().setDanger(deletePayload(approval.payload).node_ids)
      }
      onMouseLeave={() => approval.kind === "delete" && useAgentHighlight.getState().setDanger([])}
    >
      {pending && <span className="bg-status-warning size-1.5 shrink-0 rounded-full" />}
      {icon}
      <span
        title={trial ? text : undefined}
        className={cn(
          "min-w-0",
          trial ? "wrap-anywhere" : "truncate",
          pending && "text-foreground",
        )}
      >
        {text}
      </span>
      {trial && safeResultLink(trialResult?.trace_url) && (
        <a
          href={safeResultLink(trialResult?.trace_url)}
          target="_blank"
          rel="noreferrer noopener"
          className="shrink-0 underline underline-offset-2"
        >
          查看 trace
        </a>
      )}
      {trial && safeResultLink(trialResult?.output_url) && (
        <a
          href={safeResultLink(trialResult?.output_url)}
          target="_blank"
          rel="noreferrer noopener"
          className="shrink-0 underline underline-offset-2"
        >
          查看产物
        </a>
      )}
      {quote !== null && <span className="text-credit shrink-0 tabular-nums">· ✦ {quote}</span>}
      {pending && (
        <span className="flex shrink-0 items-center gap-1">
          · 等你{ask ? "回答" : "确认"}
          <Hand />
        </span>
      )}
      {adminChange && canRollback(approval) && (
        <button
          type="button"
          disabled={ctl.busy}
          title={ctl.busy ? "运行中不能回滚" : undefined}
          onClick={() => {
            const orderNo = adminChangePayload(approval)?.order_no;
            if (orderNo) void ctl.send(rollbackMessage(orderNo));
          }}
          className="hover:bg-chrome-hover hover:text-foreground focus-visible:ring-node-ring/60 ml-auto inline-flex h-6 shrink-0 items-center gap-1 rounded-md px-2 text-xs outline-none transition-colors duration-120 focus-visible:ring-2 disabled:opacity-40"
        >
          <Undo2 aria-hidden />
          回滚
        </button>
      )}
    </div>
  );
}
