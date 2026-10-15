import type { HealthTone, ModelStatus } from "@/api/admin/ai/type";

export type { HealthTone, ModelStatus };

/**
 * 健康色调对应到 Tag / StatusLabel 的 tone。
 * 渠道健康只有 ok / warn / off（后端 service.ChannelHealthOf）；bad 保留给“已上架但用不了”的模型状态。
 */
export const HEALTH_UI_TONE = {
  ok: "success",
  bad: "danger",
  warn: "warning",
  off: "neutral",
} as const satisfies Record<HealthTone, string>;

/**
 * 模型状态的中文标签。状态由后端 service.ModelHealthOf 算好，前端只做展示映射，不重算：
 * 在线 / 不可用（已上架但渠道或协议用不了）/ 未上线 / 待配置（没启用，且还没选能力或协议）。
 */
export const MODEL_STATUS_LABEL: Record<ModelStatus, string> = {
  online: "在线",
  broken: "不可用",
  offline: "未上线",
  pending: "待配置",
};

/** 模型状态的色调：待配置用 warning 的语气，提醒运营还要选能力 / 协议 */
export const MODEL_STATUS_TONE: Record<ModelStatus, HealthTone> = {
  online: "ok",
  broken: "bad",
  offline: "off",
  pending: "warn",
};
