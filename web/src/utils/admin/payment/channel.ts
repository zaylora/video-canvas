import type { PaymentChannel, PaymentCheckout, PaymentMethod } from "@/api/admin/payment/type.d";
import type { TagTone } from "@/components/admin-ui/tag";

import { METHOD_FULL_LABEL, METHOD_LABEL } from "./settings";

/** admin 角色下，全局设置与渠道的写操作按钮被禁用时的原因（后端对 admin 同样返回 403） */
export const OPS_ONLY_REASON = "仅运维可修改";

/** 渠道的配置状态，决定列表里的状态标签 */
export type ChannelStatusKey = "unconfigured" | "unvalidated" | "validated" | "enabled";

/** 渠道状态的展示：标签文字与语气 */
export interface ChannelStatusView {
  /** 状态键 */
  key: ChannelStatusKey;
  /** 标签文字 */
  label: string;
  /** 标签语气 */
  tone: TagTone;
}

/**
 * 判定渠道状态：已启用 > 已校验未启用 > 待校验（保存过但还没校验或校验作废）> 未配置
 * @param channel 渠道视图
 */
export function channelStatus(channel: PaymentChannel): ChannelStatusView {
  if (channel.enabled) return { key: "enabled", label: "已启用", tone: "success" };
  if (channel.validated) return { key: "validated", label: "已校验未启用", tone: "info" };
  if (channel.configured) return { key: "unvalidated", label: "待校验", tone: "warning" };
  return { key: "unconfigured", label: "未配置", tone: "neutral" };
}

/** 收银形态的显示文字 */
export const CHECKOUT_LABEL: Record<PaymentCheckout, string> = {
  qrcode: "扫码",
  redirect: "跳转",
  mixed: "跳转或扫码",
};

/**
 * 收银形态文字；遇到后端新增的未知形态时原样显示，不让界面空白
 * @param checkout 收银形态
 */
export const checkoutLabel = (checkout: string): string =>
  CHECKOUT_LABEL[checkout as PaymentCheckout] ?? checkout;

/** 能力标签：文字与语气 */
export interface CapabilityTag {
  /** 标签文字 */
  label: string;
  /** 标签语气 */
  tone: TagTone;
}

/**
 * 渠道的两个能力标签：能否查单（决定丢回调后能不能补单）、能否在渠道侧关单（决定关单后用户还能不能付款）
 * @param channel 渠道视图
 * @returns 固定两项：查单、关单
 */
export function capabilityTags(
  channel: Pick<PaymentChannel, "can_query" | "can_close_upstream">,
): CapabilityTag[] {
  return [
    channel.can_query
      ? { label: "可查单", tone: "success" }
      : { label: "不能查单", tone: "danger" },
    channel.can_close_upstream
      ? { label: "渠道侧关单", tone: "success" }
      : { label: "仅本地关单", tone: "warning" },
  ];
}

/**
 * 渠道支持的支付方式文字，如「微信、支付宝」
 * @param methods 支付方式列表
 */
export const methodsText = (methods: PaymentMethod[]): string =>
  methods.map((method) => METHOD_LABEL[method] ?? method).join("、");

/**
 * 渠道当前承接的支付方式文字：「承接：微信支付」；没有承接时为「未承接」
 * @param serving 当前承接的支付方式
 */
export const servingText = (serving: PaymentMethod[]): string =>
  serving.length > 0
    ? `承接：${serving.map((method) => METHOD_FULL_LABEL[method] ?? method).join("、")}`
    : "未承接";

/**
 * 启用前的回调地址问题。后端同样会拒绝（65013 / 65009），这里提前说明并禁用按钮。
 * @param channel 渠道视图
 * @returns 问题说明；没有问题时为 null
 */
export function notifyUrlProblem(
  channel: Pick<PaymentChannel, "provider" | "notify_url">,
): string | null {
  if (!channel.notify_url) return "请先在全局设置里配置站点公网地址";
  if (channel.provider === "wechat_native" && !channel.notify_url.startsWith("https://"))
    return "微信支付要求回调地址是 https，请把站点公网地址改成 https 开头";
  return null;
}

/**
 * 能否启用：必须已通过校验，且回调地址没有问题
 * @param channel 渠道视图
 * @returns 不能启用的原因；可以启用时为 null
 */
export function enableBlockReason(channel: PaymentChannel): string | null {
  if (!channel.validated) return "配置还没有通过校验";
  return notifyUrlProblem(channel);
}

/**
 * 停用确认框的后果说明：它承接的支付方式立即不可用，已有订单不受影响
 * @param channel 渠道视图
 * @returns 一句话说明
 */
export function disableConsequence(channel: Pick<PaymentChannel, "serving">): string {
  const serving = channel.serving.map((method) => METHOD_FULL_LABEL[method] ?? method);
  const first =
    serving.length > 0
      ? `它当前承接的${serving.join("、")}会在前台立即不可用`
      : "它当前没有承接任何支付方式，停用不影响前台";
  return `${first}；已有订单仍会正常入账。`;
}
