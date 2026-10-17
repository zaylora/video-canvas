import type {
  PaymentChannel,
  PaymentOrderAdmin,
  PaymentOrderStatus,
} from "@/api/admin/payment/type.d";
import type { TagTone } from "@/components/admin-ui/tag";

/** 订单状态分段筛选：值为空表示全部；creating 是瞬时状态，不单独放，包含在「全部」里 */
export const ORDER_STATUS_FILTERS: ReadonlyArray<readonly [value: string, label: string]> = [
  ["", "全部"],
  ["pending", "待支付"],
  ["credited", "已入账"],
  ["closed", "已关闭"],
  ["abnormal", "异常"],
  ["create_failed", "下单失败"],
];

/** 支付方式筛选下拉 */
export const ORDER_METHOD_FILTERS: ReadonlyArray<readonly [value: string, label: string]> = [
  ["", "全部方式"],
  ["wechat", "微信"],
  ["alipay", "支付宝"],
];

/** 异常原因的中文说明 */
const ABNORMAL_LABEL: Record<string, string> = {
  amount_mismatch: "金额不符",
  identity_mismatch: "商户身份不符",
  trade_no_conflict: "交易号冲突",
};

/**
 * 异常原因文案；未知原因原样显示，空原因为空串
 * @param reason abnormal_reason
 */
export const abnormalReasonLabel = (reason: string): string => ABNORMAL_LABEL[reason] ?? reason;

/** 关单原因的中文说明 */
const CLOSE_LABEL: Record<string, string> = {
  timeout: "到期自动关闭",
  user: "用户取消",
  admin: "管理员关单",
};

/**
 * 关单原因文案；未知原因原样显示，空原因为空串
 * @param reason close_reason
 */
export const closeReasonLabel = (reason: string): string => CLOSE_LABEL[reason] ?? reason;

/** 订单状态标签：文字、语气，以及悬停时的补充说明 */
export interface OrderStatusTag {
  /** 标签文字 */
  label: string;
  /** 标签语气 */
  tone: TagTone;
  /** 悬停提示；没有补充说明时为 null */
  hint: string | null;
}

/**
 * 订单状态标签。已入账且迟到付款显示「已入账·迟到」；异常红色，提示写明原因；下单失败提示写明失败原因。
 * @param order 订单（只用到状态相关字段）
 */
export function orderStatusTag(
  order: Pick<PaymentOrderAdmin, "status" | "late_paid" | "abnormal_reason" | "fail_reason">,
): OrderStatusTag {
  switch (order.status) {
    case "creating":
      return { label: "下单中", tone: "neutral", hint: null };
    case "pending":
      return { label: "待支付", tone: "info", hint: null };
    case "credited":
      return order.late_paid
        ? { label: "已入账·迟到", tone: "gold", hint: "订单关闭后才收到付款通知，仍已入账" }
        : { label: "已入账", tone: "success", hint: null };
    case "closed":
      return { label: "已关闭", tone: "neutral", hint: null };
    case "abnormal":
      return {
        label: "异常",
        tone: "danger",
        hint: abnormalReasonLabel(order.abnormal_reason) || "异常订单，需要人工处理",
      };
    case "create_failed":
      return { label: "下单失败", tone: "orange", hint: order.fail_reason || null };
    default:
      return { label: String(order.status), tone: "neutral", hint: null };
  }
}

/** 渠道通知处理结果的文案与语气 */
const NOTIFY_RESULT: Record<string, { label: string; tone: TagTone }> = {
  credited: { label: "已入账", tone: "success" },
  duplicate: { label: "重复通知", tone: "neutral" },
  unpaid: { label: "未付款", tone: "neutral" },
  amount_mismatch: { label: "金额不符", tone: "danger" },
  identity_mismatch: { label: "商户身份不符", tone: "danger" },
  trade_no_conflict: { label: "交易号冲突", tone: "danger" },
  not_found: { label: "订单不存在", tone: "warning" },
  state_rejected: { label: "状态不允许入账", tone: "warning" },
};

/**
 * 渠道通知处理结果标签；未知结果原样显示
 * @param result 通知记录的 result
 */
export const notifyResultTag = (result: string): { label: string; tone: TagTone } =>
  NOTIFY_RESULT[result] ?? { label: result, tone: "neutral" };

/** 状态时间线的一项 */
export interface OrderTimelineEntry {
  /** 稳定的键 */
  key: string;
  /** 事件文案 */
  label: string;
  /** 发生时间（RFC3339）；后端没有记录时间的事件为 null，排在最后 */
  at: string | null;
  /** 语气：用来给异常、失败的节点上色 */
  tone: "default" | "success" | "danger";
}

/**
 * 用订单上的各个时间点拼状态时间线，按发生时间升序，没有时间的事件排在最后。
 * 例如关单后迟到付款：创建 → 订单关闭 → 渠道报告付款 → 积分入账。
 * @param order 订单
 */
export function orderTimeline(order: PaymentOrderAdmin): OrderTimelineEntry[] {
  const entries: OrderTimelineEntry[] = [
    { key: "created", label: "创建订单", at: order.created_at, tone: "default" },
  ];
  if (order.paid_at)
    entries.push({ key: "paid", label: "渠道报告已付款", at: order.paid_at, tone: "default" });
  if (order.credited_at)
    entries.push({
      key: "credited",
      label: order.late_paid ? "积分入账（迟到付款）" : "积分入账",
      at: order.credited_at,
      tone: "success",
    });
  if (order.closed_at || order.status === "closed") {
    const reason = closeReasonLabel(order.close_reason);
    entries.push({
      key: "closed",
      label: reason ? `订单关闭 · ${reason}` : "订单关闭",
      at: order.closed_at,
      tone: "default",
    });
  }
  if (order.status === "abnormal") {
    const reason = abnormalReasonLabel(order.abnormal_reason);
    entries.push({
      key: "abnormal",
      label: reason ? `标记异常 · ${reason}，不自动入账` : "标记异常，不自动入账",
      at: null,
      tone: "danger",
    });
  }
  if (order.status === "create_failed")
    entries.push({
      key: "create_failed",
      label: order.fail_reason ? `渠道下单失败 · ${order.fail_reason}` : "渠道下单失败",
      at: null,
      tone: "danger",
    });
  const time = (entry: OrderTimelineEntry) =>
    entry.at ? Date.parse(entry.at) : Number.POSITIVE_INFINITY;
  return entries
    .map((entry, index) => ({ entry, index }))
    .sort((a, b) => {
      const diff = time(a.entry) - time(b.entry);
      // 两个都没有时间时 Infinity - Infinity 是 NaN，按原顺序
      return Number.isNaN(diff) || diff === 0 ? a.index - b.index : diff;
    })
    .map(({ entry }) => entry);
}

/** 能查单的订单状态：待支付，以及已关闭（可能迟到付款） */
const QUERYABLE_STATUS: PaymentOrderStatus[] = ["pending", "closed"];

/**
 * 查单按钮的禁用原因。渠道信息取自渠道列表；找不到渠道时不拦，交给后端判断（65008）。
 * @param order 订单
 * @param channel 订单所属渠道；未知时为 undefined
 * @returns 禁用原因；可用时为 null
 */
export function queryBlockReason(
  order: Pick<PaymentOrderAdmin, "status" | "provider_name">,
  channel: Pick<PaymentChannel, "can_query" | "name"> | undefined,
): string | null {
  if (!QUERYABLE_STATUS.includes(order.status)) return "只有待支付或已关闭的订单可以查单";
  if (channel && !channel.can_query)
    return `${channel.name || order.provider_name}不支持查单，只能等渠道回调`;
  return null;
}

/**
 * 关单按钮的禁用原因：只有待支付订单可以关单
 * @param order 订单
 * @returns 禁用原因；可用时为 null
 */
export function closeBlockReason(order: Pick<PaymentOrderAdmin, "status">): string | null {
  return order.status === "pending" ? null : "只有待支付的订单可以关单";
}

/**
 * 关单确认框里的补充提醒：渠道不能在渠道侧关单时，本地关单后用户仍可能付款
 * @param channel 订单所属渠道；未知时为 undefined
 * @returns 提醒文案；没有额外风险时为 null
 */
export function closeUpstreamWarning(
  channel: Pick<PaymentChannel, "can_close_upstream" | "name"> | undefined,
): string | null {
  if (!channel || channel.can_close_upstream) return null;
  return `${channel.name}只能在本地关单：关单后用户仍可能付款成功，系统收到通知后照样入账（标记为迟到入账）。`;
}

/**
 * 把通知的业务字段排版成可读的 JSON 文本
 * @param payload 通知的 payload
 */
export function formatPayload(payload: Record<string, unknown> | null): string {
  return JSON.stringify(payload ?? {}, null, 2);
}
