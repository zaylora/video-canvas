import type { UsageStatusView } from "@/utils/admin/log-rules";

type Tone = UsageStatusView["tone"];

/** 状态点颜色：全部走设计 token，不写死色值 */
export const TONE_DOT: Record<Tone, string> = {
  success: "bg-status-success",
  danger: "bg-destructive",
  warning: "bg-status-warning",
  info: "bg-status-running",
  neutral: "bg-muted-foreground/50",
};

/** 结论卡的淡色底 */
export const TONE_SOFT: Record<Tone, string> = {
  success: "bg-status-success/10",
  danger: "bg-destructive/10",
  warning: "bg-status-warning/10",
  info: "bg-status-running/10",
  neutral: "bg-muted",
};
