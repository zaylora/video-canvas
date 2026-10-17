import type { OrderDto } from "@/api/payment/type";

import { formatCredits } from "./format";

/** 订单号的合法格式：和后端路径参数的校验一致（字母数字下划线短横线，最长 32 位） */
const ORDER_NO_RE = /^[A-Za-z0-9_-]{1,32}$/;

/** 回跳页对用户的提示 */
export interface ReturnNotice {
  /**
   * 提示类型：
   * - success 已到账
   * - waiting 渠道还没通知到，等一会儿会自动到账
   * - closed 订单已关闭或无效
   * - abnormal 金额等核对不通过，转人工
   * - unknown 找不到订单
   */
  kind: "success" | "waiting" | "closed" | "abnormal" | "unknown";
  /** 标题 */
  title: string;
  /** 补充说明 */
  detail: string;
}

/**
 * 从回跳地址的查询串里取订单号。渠道可能在后面追加自己的参数，只认 order_no；
 * 格式不对（含脚本字符、过长）一律当作没有，不去请求后端。
 * @param search location.search，如 "?order_no=VC1"
 * @returns 订单号；没有或格式不对返回 null
 */
export function parseReturnOrderNo(search: string): string | null {
  const value = new URLSearchParams(search).get("order_no")?.trim() ?? "";
  return ORDER_NO_RE.test(value) ? value : null;
}

/**
 * 回跳页按订单状态给用户的提示。回跳页只是「付款完成后的落地页」，不是支付成功的依据：
 * 渠道的回调才是，所以 pending 时只说「确认中」，绝不说失败。
 * @param order 查到的订单；找不到为 null
 * @returns 要展示的提示
 */
export function describeReturn(order: OrderDto | null): ReturnNotice {
  if (!order) {
    return {
      kind: "unknown",
      title: "没有找到这笔订单",
      detail: "请回到原来的页面查看充值结果；如果已经付款，积分会在确认后自动到账。",
    };
  }
  switch (order.status) {
    case "credited":
      return {
        kind: "success",
        title: `支付成功，已到账 ${formatCredits(order.credits)} 积分`,
        detail: order.latePaid
          ? "这笔是订单关闭后才收到的付款，已照常入账。"
          : "现在可以回到原来的页面继续创作了。",
      };
    case "pending":
    case "creating":
      return {
        kind: "waiting",
        title: "正在确认付款结果",
        detail: "付款成功后积分会自动到账，通常几秒钟。请回到原来的页面查看，不要重复付款。",
      };
    case "abnormal":
      return {
        kind: "abnormal",
        title: "支付金额异常",
        detail: `已通知管理员处理，请保留付款凭证并联系管理员。订单号 ${order.orderNo}`,
      };
    default:
      return {
        kind: "closed",
        title: "订单已关闭",
        detail: "如果你已经付款，积分会在渠道确认后自动到账；否则请回到商城重新下单。",
      };
  }
}
