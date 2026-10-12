import type { HealthTone, ModelStatus } from "@/api/admin/ai/type";

export type { HealthTone, ModelStatus };

/** 健康色调对应到 Tag / StatusLabel 的 tone */
export const HEALTH_UI_TONE = {
  ok: "success",
  bad: "danger",
  warn: "warning",
  off: "neutral",
} as const satisfies Record<HealthTone, string>;

export const MODEL_STATUS_LABEL: Record<ModelStatus, string> = {
  online: "在线",
  broken: "不可用",
  offline: "未上线",
};

export const MODEL_STATUS_TONE: Record<ModelStatus, HealthTone> = {
  online: "ok",
  broken: "bad",
  offline: "off",
};
