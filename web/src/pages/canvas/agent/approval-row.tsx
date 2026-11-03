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
  X,
} from "lucide-react";

import type {
  AgentApprovalDto,
  AdminSecretResult,
  AdminTestRunPayload,
  AdminTestRunResult,
} from "@/api/agent/type";
import type { AgentController } from "@/hooks/use-agent-controller";
import { cn } from "@/lib/utils";
import { useAgentHighlight } from "@/store/agent-highlight";
import {
  adminChangeResult,
  adminChangePayload,
  resultSummary,
} from "@/utils/admin/assistant/change-order";
import { formatSeconds } from "@/utils/agent/activity";
import { askAnsweredRows, askCount } from "@/utils/agent/ask";

import { ApprovalCard } from "./approval-card";
import { approvalTitle, deletePayload } from "./approval-title";
import { ChangeResultCard } from "./change-result-card";

export { approvalTitle, deletePayload } from "./approval-title";

/** 决定里附带的文字：提问的回答，或拒绝时给 Agent 的说明 */
const answerOf = (approval: AgentApprovalDto) =>
  approval.kind !== "admin_secret" && typeof approval.decision?.answer === "string"
    ? approval.decision.answer
    : "";

/** 结果链接允许 http(s) 或站内绝对路径，禁止脚本与协议相对地址 */
const safeResultLink = (url?: string) =>
  url && (/^https?:\/\//i.test(url) || /^\/(?![/\\])/.test(url)) ? url : undefined;

/**
 * 消息流里的审批 / 提问：等你决定时（运行正停在它上面）直接是完整的交互卡片 ApprovalCard；
 * 变更单执行后原地变成结果卡 ChangeResultCard；其余情况只占一行——决定后写明结果（拒绝的理由、你的回答跟在后面），
 * 答过的提问是「↳ 问题 回答」，跳过的提问是「↳ 跳过了这个问题」。
 */
export function ApprovalRow({ approvalId, ctl }: { approvalId: string; ctl: AgentController }) {
  const approval = ctl.state.approvals[approvalId];
  if (!approval) return null;
  // 运行正停在这条审批上：完整的交互卡片（过期、运行已结束而仍待决定的单子只读，走下面的一行）
  if (approval.status === "pending" && ctl.pending?.id === approval.id)
    return <ApprovalCard approval={approval} ctl={ctl} />;
  // 变更单执行后：原地变成结果卡（读不出内容或结果的，退回一行摘要）
  if (
    approval.kind === "admin_change" &&
    approval.status !== "rejected" &&
    adminChangePayload(approval) &&
    adminChangeResult(approval)
  )
    return <ChangeResultCard approval={approval} ctl={ctl} />;
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

  // 答过的提问（设计稿 6.12）：每题一行「↳ 问题  答案」挂在过程轨上，问题淡、答案实；选填没答的写「没回答」
  const rows = ask && !pending && approval.status !== "rejected" ? askAnsweredRows(approval) : [];
  const rowClass =
    "text-muted-foreground flex h-7 min-w-0 items-center gap-2 text-[12.5px] whitespace-nowrap [&_svg]:size-3.5 [&_svg]:shrink-0";
  const answerRow = (question: string, reply: string, key?: number) => (
    <div key={key} aria-label={`你的回答：${reply || "没回答"}`} className={rowClass}>
      <CornerDownRight />
      <span className="max-w-[55%] truncate">{question}</span>
      <span
        className={cn("min-w-0 truncate", reply ? "text-foreground font-medium" : "opacity-70")}
      >
        {reply || "没回答"}
      </span>
    </div>
  );
  if (ask && !pending && approval.status === "rejected" && !answer) {
    // 跳过的提问（用户改发了新消息）：多题收成一行「跳过了 N 个问题」
    const count = askCount(approval);
    return (
      <div
        aria-label={count > 1 ? `跳过了 ${count} 个问题` : "跳过了这个问题"}
        className={rowClass}
      >
        <CornerDownRight />
        {count > 1 ? (
          <span className="min-w-0 truncate">跳过了 {count} 个问题</span>
        ) : (
          <>
            <span className="max-w-[55%] truncate">{title}</span>
            <span className="min-w-0 truncate">跳过了这个问题</span>
          </>
        )}
      </div>
    );
  }
  if (rows.length === 1) return answerRow(rows[0].question, rows[0].answer);
  if (rows.length > 1)
    return (
      <div className="flex flex-col">{rows.map((r, i) => answerRow(r.question, r.answer, i))}</div>
    );
  if (ask && !pending && answer) return answerRow(title, answer);

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
    </div>
  );
}
