import type { ChannelCheckResult } from "@/api/admin/ai/type";

import { isPluginOpFailed, isRunnerDown, isSecretMissing } from "./errors";

/**
 * 连通性检查的结果分类。要能区分：
 * 成功、插件不支持（中性，不是错误）、Key 未设置（引导去设 Key）、runner 不可用、插件本身出错（钩子抛异常等）；
 * 其余失败（地址不通、上游返回错误）归为 failed。
 */
export type CheckOutcome = {
  /** 结果类型 */
  kind: "ok" | "unsupported" | "no-key" | "runner-down" | "failed" | "plugin-fault" | "error";
  /** 展示语气 */
  tone: "success" | "neutral" | "warning" | "danger";
  /** 一句话结论 */
  title: string;
  /** 补充说明（后端 message、耗时等） */
  detail?: string;
  /** 后端返回的原始说明（不含耗时）；failed 和 plugin-fault 带，复制修复信息时原样交给 AI */
  message?: string;
};

/** 后端对“插件没实现 buildCheckRequest”给的固定说明 */
export const CHECK_UNSUPPORTED_MESSAGE = "插件不支持连通性检查";

/** 检查接口正常返回（HTTP 200）的结果 → 分类 */
export function classifyCheck(result: ChannelCheckResult): CheckOutcome {
  const duration = result.duration_ms > 0 ? `耗时 ${Math.round(result.duration_ms)}ms` : "";
  if (result.ok) {
    return {
      kind: "ok",
      tone: "success",
      title: "连通",
      detail: [result.message, duration].filter(Boolean).join(" · "),
    };
  }
  if (result.message.includes(CHECK_UNSUPPORTED_MESSAGE)) {
    return {
      kind: "unsupported",
      tone: "neutral",
      title: "该插件不支持连通性检查",
      detail: "不是故障。可以直接去模型页试跑来验证。",
    };
  }
  return {
    kind: "failed",
    tone: "danger",
    title: "不通",
    detail: [result.message, duration].filter(Boolean).join(" · "),
    message: result.message,
  };
}

/** 检查接口抛错（409 / 50015、503 / 50021 等）→ 分类 */
export function classifyCheckError(error: unknown): CheckOutcome {
  if (isSecretMissing(error)) {
    return {
      kind: "no-key",
      tone: "warning",
      title: "还没有设置 Key",
      detail: "先给这个渠道设置 Key，再检查。",
    };
  }
  if (isRunnerDown(error)) {
    return {
      kind: "runner-down",
      tone: "warning",
      title: "插件运行器暂时不可用",
      detail: "稍后重试；这不代表渠道本身有问题。",
    };
  }
  if (isPluginOpFailed(error)) {
    // 插件在检查钩子里抛异常或返回了非法的请求描述：这是插件的问题，原因（已脱敏）就在后端文案里
    const message = errorMessage(error);
    return {
      kind: "plugin-fault",
      tone: "danger",
      title: "插件本身出错",
      detail: message || "详见页面顶部的错误提示。",
      ...(message ? { message } : {}),
    };
  }
  return { kind: "error", tone: "danger", title: "检查失败", detail: "详见页面顶部的错误提示。" };
}

/** 请求错误对象里的文案；没有返回空串 */
function errorMessage(error: unknown): string {
  const message =
    typeof error === "object" && error !== null ? (error as { message?: unknown }).message : "";
  return typeof message === "string" ? message.trim() : "";
}
