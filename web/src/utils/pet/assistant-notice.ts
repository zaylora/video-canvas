import type { AgentApprovalKind, AgentRunStatus } from "@/api/agent/type";

import type { BubbleInput } from "./bubble-queue";
import type { PetReactionKind } from "./mood";

/** 点击气泡（打开浮窗）由调用方补上，这里只管内容 */
export type AssistantBubble = Omit<BubbleInput, "onClick" | "action">;

/** 浮窗收起时出现待审批或提问：不自动消失，审批处理完或浮窗打开时按 id 收起 */
export function approvalBubble(approvalId: string, kind: AgentApprovalKind): AssistantBubble {
  return {
    id: `assistant:approval:${approvalId}`,
    source: "assistant",
    priority: "approval",
    text: kind === "ask" ? "有个问题想问问你" : "这一步要你点个头",
    mood: "waiting",
    ttl: null,
  };
}

/** 没有给用户看的原因时，按结束状态说一句 */
const FAILURE_FALLBACK: Partial<Record<AgentRunStatus, string>> = {
  timeout: "运行超时",
  step_limit: "达到步数上限",
  budget_exhausted: "额度用完了",
  interrupted: "被中断了",
  expired: "等太久，已过期",
};

/**
 * 一轮运行结束时的气泡：成功、出错各一条；用户自己停掉的、还没结束的不冒（返回 null）。
 * runId 用来去重，同一轮只冒一次。
 */
export function runEndBubble(
  runId: string,
  status: AgentRunStatus | undefined,
  error: string,
): AssistantBubble | null {
  if (status === "succeeded") {
    return {
      id: `assistant:done:${runId}`,
      source: "assistant",
      priority: "result",
      text: "好啦，回复写完了",
      mood: "jumping",
      ttl: 4000,
    };
  }
  if (status === undefined || status === "canceled") return null;
  const failed = status === "failed" || status in FAILURE_FALLBACK;
  if (!failed) return null;
  const reason = error || FAILURE_FALLBACK[status];
  return {
    id: `assistant:done:${runId}`,
    source: "assistant",
    priority: "error",
    text: reason ? `卡住了：${reason}` : "卡住了，这一轮没跑完",
    mood: "failed",
    ttl: 6000,
  };
}

/** 浮窗打开时一轮结束不冒泡，改让输入框上沿的小镜播一次反应：规则和 runEndBubble 一致 */
export function runEndReaction(status: AgentRunStatus | undefined): PetReactionKind | null {
  const bubble = runEndBubble("", status, "");
  if (!bubble) return null;
  return bubble.mood === "jumping" ? "celebrate" : "failed";
}
