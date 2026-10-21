import type { AgentFeedbackDto, AgentFeedbackRating, AgentFeedbackReason } from "@/api/agent/type";

/** 补充说明最多多少字（与后端一致） */
export const FEEDBACK_COMMENT_MAX = 500;

/** 保存评价失败时的提示：全局请求提示被关掉了，由这里统一给文案 */
export const FEEDBACK_SAVE_FAILED = "评价没保存，稍后再试";

/** 点踩时可选的原因标签，按展示顺序 */
export const FEEDBACK_REASONS: readonly { key: AgentFeedbackReason; label: string }[] = [
  { key: "misread", label: "没理解我的需求" },
  { key: "wrong_edit", label: "画布改错了" },
  { key: "slow", label: "步骤太多或太慢" },
  { key: "bad_output", label: "生成结果不好" },
  { key: "other", label: "其他" },
];

const REASON_KEYS = new Set<string>(FEEDBACK_REASONS.map((r) => r.key));

/** 前端持有的一轮运行的反馈 */
export type AgentFeedback = AgentFeedbackDto;

/** 原因标签的中文名；不认识的标签返回 null（后端新增了标签而前端还没跟上） */
export const reasonLabel = (key: string): string | null =>
  FEEDBACK_REASONS.find((r) => r.key === key)?.label ?? null;

/**
 * 把接口返回或事件里的反馈整理成前端的形状：评价不是 1 / -1 视为无效；
 * 原因去掉不认识的和重复的，说明缺省补空串
 * @param raw 接口返回或事件的 data
 * @returns 整理后的反馈；无效返回 null
 */
export function normalizeFeedback(raw: unknown): AgentFeedback | null {
  if (typeof raw !== "object" || raw === null) return null;
  const data = raw as Record<string, unknown>;
  if (data.rating !== 1 && data.rating !== -1) return null;
  const reasons = Array.isArray(data.reasons)
    ? [
        ...new Set(
          data.reasons.filter(
            (r): r is AgentFeedbackReason => typeof r === "string" && REASON_KEYS.has(r),
          ),
        ),
      ]
    : [];
  return {
    rating: data.rating,
    reasons,
    comment: typeof data.comment === "string" ? data.comment : "",
  };
}

/** 点一下👍或👎要做什么 */
export type ThumbAction =
  /** 再点已选中的按钮：取消评价 */
  | "cancel"
  /** 👍：直接提交 */
  | "submit-up"
  /** 👎：展开（或收起）原因和说明，提交时才保存 */
  | "toggle-form";

/**
 * 点击👍/👎的语义：点已选中的 = 取消；👍 直接提交；👎 先展开原因
 * @param current 这一轮当前的评价，没评价为 undefined
 * @param clicked 点的是哪个按钮
 */
export function thumbAction(
  current: AgentFeedback | undefined,
  clicked: AgentFeedbackRating,
): ThumbAction {
  if (current?.rating === clicked) return "cancel";
  return clicked === 1 ? "submit-up" : "toggle-form";
}

/**
 * 评价后的一行小字：点赞是感谢；点踩列出选的原因，没选写「未选原因」
 * @param feedback 这一轮的评价
 */
export function feedbackNote(feedback: AgentFeedback): string {
  if (feedback.rating === 1) return "感谢反馈";
  const labels = feedback.reasons.map(reasonLabel).filter((l): l is string => !!l);
  return `已记录：${labels.join("、") || "未选原因"}`;
}
