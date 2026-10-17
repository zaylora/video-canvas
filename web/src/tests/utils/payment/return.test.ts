import { describe, expect, test } from "bun:test";

import type { OrderDto } from "@/api/payment/type";
import { describeReturn, parseReturnOrderNo } from "@/utils/payment/return";

const order = (over: Partial<OrderDto> = {}): OrderDto => ({
  orderNo: "VC1",
  status: "pending",
  amountFen: 10000,
  credits: 10500,
  method: "alipay",
  checkout: null,
  expiresAt: 0,
  creditedAt: null,
  closeReason: null,
  latePaid: false,
  abnormalReason: null,
  ...over,
});

describe("parseReturnOrderNo：从回跳地址取订单号", () => {
  test("取 order_no 参数，去掉首尾空白", () => {
    expect(parseReturnOrderNo("?order_no=VC2610160930124821330")).toBe("VC2610160930124821330");
    expect(parseReturnOrderNo("?order_no=%20VC1%20")).toBe("VC1");
  });

  test("渠道可能在后面追加自己的参数，不影响取值", () => {
    expect(parseReturnOrderNo("?order_no=VC1&out_trade_no=VC1&trade_no=2026&sign=abc")).toBe("VC1");
  });

  test("缺失、为空、格式不对（含脚本字符）一律返回 null，不去请求后端", () => {
    expect(parseReturnOrderNo("")).toBeNull();
    expect(parseReturnOrderNo("?order_no=")).toBeNull();
    expect(parseReturnOrderNo("?order_no=<script>alert(1)</script>")).toBeNull();
    expect(parseReturnOrderNo("?order_no=" + "A".repeat(33))).toBeNull();
    expect(parseReturnOrderNo("?other=1")).toBeNull();
  });
});

describe("describeReturn：回跳页按订单状态给用户的提示", () => {
  test("已入账：成功，带积分数", () => {
    const d = describeReturn(order({ status: "credited" }));
    expect(d.kind).toBe("success");
    expect(d.title).toContain("10,500");
  });

  test("迟到入账也算成功，并说明原因", () => {
    const d = describeReturn(order({ status: "credited", latePaid: true }));
    expect(d.kind).toBe("success");
    expect(d.detail).toContain("关闭");
  });

  test("待支付：确认中，告诉用户回原页面等，不要说失败（回调可能还在路上）", () => {
    const d = describeReturn(order({ status: "pending" }));
    expect(d.kind).toBe("waiting");
    expect(d.title).not.toContain("失败");
  });

  test("已关闭：说明订单关闭，并提示已付款会在确认后到账", () => {
    const d = describeReturn(order({ status: "closed", closeReason: "timeout" }));
    expect(d.kind).toBe("closed");
    expect(d.detail).toContain("到账");
  });

  test("异常：提示联系管理员并保留订单号", () => {
    const d = describeReturn(order({ status: "abnormal" }));
    expect(d.kind).toBe("abnormal");
    expect(d.detail).toContain("VC1");
  });

  test("下单失败状态：当作订单无效", () => {
    expect(describeReturn(order({ status: "create_failed" })).kind).toBe("closed");
  });

  test("没有订单（订单号找不到或没登录）：给出可操作的提示", () => {
    const d = describeReturn(null);
    expect(d.kind).toBe("unknown");
    expect(d.detail.length).toBeGreaterThan(0);
  });
});
