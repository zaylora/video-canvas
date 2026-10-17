import type {
  PaymentChannel,
  PaymentField,
  UpdatePaymentChannelRequest,
} from "@/api/admin/payment/type.d";

import { parseNonNegativeInt } from "./money";

/** 未支付自动关闭的分钟数范围，与后端 min=5,max=120 一致 */
export const TIMEOUT_RANGE = { min: 5, max: 120 } as const;

/** 渠道配置表单的取值 */
export interface ChannelFormValues {
  /** 非敏感字段的当前输入，键是字段 key，所有非敏感字段都有 */
  config: Record<string, string>;
  /** 敏感字段本次填写的新值；没填的字段不出现。已设置的密钥不回显，这里只有用户新输入的 */
  secrets: Record<string, string>;
  /** 未支付自动关闭分钟数的输入文本 */
  timeout: string;
}

/**
 * 由渠道视图初始化表单：普通字段带出当前值，密钥一律空（只写不读）
 * @param channel 渠道视图
 */
export function channelToForm(channel: PaymentChannel): ChannelFormValues {
  const config: Record<string, string> = {};
  for (const field of channel.fields) {
    if (!field.secret) config[field.key] = field.value;
  }
  return { config, secrets: {}, timeout: String(channel.order_timeout_minutes) };
}

/**
 * 有未完成订单时，商户身份字段和密钥字段锁定（后端同样会以 65010 拒绝）
 * @param channel 渠道视图
 * @param field 字段定义
 */
export const isFieldLocked = (channel: Pick<PaymentChannel, "open_orders">, field: PaymentField) =>
  channel.open_orders > 0 && (field.identity || field.secret);

/**
 * 校验超时分钟数
 * @param timeout 输入文本
 * @returns 错误文案；通过时为 null
 */
export function validateTimeout(timeout: string): string | null {
  const minutes = parseNonNegativeInt(timeout);
  if (minutes === null || minutes < TIMEOUT_RANGE.min || minutes > TIMEOUT_RANGE.max)
    return `请填写 ${TIMEOUT_RANGE.min}–${TIMEOUT_RANGE.max} 之间的整数分钟数`;
  return null;
}

/**
 * 与已保存配置相比，哪些非敏感字段被改了
 * @param channel 渠道视图（已保存的值）
 * @param values 表单取值
 * @returns 改动过的字段 key
 */
export function changedConfigKeys(channel: PaymentChannel, values: ChannelFormValues): string[] {
  return channel.fields
    .filter((field) => !field.secret)
    .filter((field) => (values.config[field.key] ?? "").trim() !== field.value.trim())
    .map((field) => field.key);
}

/**
 * 本次填了新值的密钥字段 key（空白不算）
 * @param values 表单取值
 */
export const filledSecretKeys = (values: ChannelFormValues): string[] =>
  Object.entries(values.secrets)
    .filter(([, value]) => value.trim() !== "")
    .map(([key]) => key);

/**
 * 保存会不会让渠道的校验作废（已启用的会自动停用）：只有非敏感配置或密钥变了才会，只改超时时间不会
 * @param channel 渠道视图
 * @param values 表单取值
 */
export const invalidatesValidation = (
  channel: PaymentChannel,
  values: ChannelFormValues,
): boolean => changedConfigKeys(channel, values).length > 0 || filledSecretKeys(values).length > 0;

/**
 * 表单相对已保存的配置有没有任何改动（决定保存按钮能不能点）。
 * 未配置过的渠道（version 为 0）即使没改任何字段也允许保存，因为保存才会产生第一份配置。
 * @param channel 渠道视图
 * @param values 表单取值
 */
export function isChannelFormDirty(channel: PaymentChannel, values: ChannelFormValues): boolean {
  if (!channel.configured) return true;
  return (
    invalidatesValidation(channel, values) ||
    values.timeout.trim() !== String(channel.order_timeout_minutes)
  );
}

/**
 * 生成保存请求。config 带上全部非敏感字段的完整取值（后端把缺少的字段视为清空），
 * secrets 只带用户真的填了的密钥；version 用打开抽屉时读到的值。调用前必须先通过 validateTimeout。
 * @param channel 渠道视图
 * @param values 表单取值
 */
export function buildChannelRequest(
  channel: PaymentChannel,
  values: ChannelFormValues,
): UpdatePaymentChannelRequest {
  const config: Record<string, string> = {};
  for (const field of channel.fields) {
    if (!field.secret) config[field.key] = (values.config[field.key] ?? "").trim();
  }
  const secrets: Record<string, string> = {};
  for (const key of filledSecretKeys(values)) secrets[key] = values.secrets[key].trim();
  return {
    version: channel.version,
    config,
    secrets,
    order_timeout_minutes: parseNonNegativeInt(values.timeout) ?? channel.order_timeout_minutes,
  };
}

/**
 * 密钥输入框的占位文字：已设置的提示留空不修改，没设置的提示未设置
 * @param field 密钥字段
 */
export const secretPlaceholder = (field: PaymentField): string =>
  field.secret_set ? "已设置 · 留空不修改" : "未设置";

/**
 * 普通字段输入框的占位文字：优先用后端给的提示，其次缺省值
 * @param field 普通字段
 */
export const fieldPlaceholder = (field: PaymentField): string =>
  field.placeholder || (field.default ? `缺省 ${field.default}` : "");
