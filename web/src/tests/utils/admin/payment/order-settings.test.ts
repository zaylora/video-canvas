import { describe, expect, test } from "bun:test";

import type {
  PaymentChannel,
  PaymentOrderAdmin,
  PaymentSettings,
} from "@/api/admin/payment/type.d";
import { isPaymentVersionConflict } from "@/utils/admin/payment/errors";
import {
  abnormalReasonLabel,
  closeBlockReason,
  closeUpstreamWarning,
  notifyResultTag,
  orderStatusTag,
  orderTimeline,
  queryBlockReason,
} from "@/utils/admin/payment/order";
import {
  callbackPreview,
  isSettingsDirty,
  methodOptions,
  normalizeBaseUrl,
  settingsFormToRequest,
  settingsToForm,
  validateSettingsForm,
} from "@/utils/admin/payment/settings";

const order = (patch: Partial<PaymentOrderAdmin> = {}): PaymentOrderAdmin => ({
  order_no: "VC001",
  user_id: 1,
  username: "lin",
  product_name: "10,000 积分",
  amount_fen: 10000,
  credits: 10000,
  method: "wechat",
  provider: "epay",
  provider_name: "易支付",
  status: "pending",
  provider_trade_no: "",
  paid_amount_fen: 0,
  late_paid: false,
  close_reason: "",
  abnormal_reason: "",
  fail_reason: "",
  channel_version: 1,
  expires_at: "2026-10-16T10:15:00Z",
  paid_at: null,
  credited_at: null,
  closed_at: null,
  created_at: "2026-10-16T10:00:00Z",
  ...patch,
});

describe("订单状态标签", () => {
  test("各状态的文字与语气", () => {
    expect(orderStatusTag(order({ status: "pending" }))).toMatchObject({ label: "待支付" });
    expect(orderStatusTag(order({ status: "credited" }))).toMatchObject({
      label: "已入账",
      tone: "success",
    });
    expect(orderStatusTag(order({ status: "closed" }))).toMatchObject({ label: "已关闭" });
    expect(orderStatusTag(order({ status: "creating" }))).toMatchObject({ label: "下单中" });
  });

  test("已入账且迟到付款显示「已入账·迟到」", () => {
    expect(orderStatusTag(order({ status: "credited", late_paid: true })).label).toBe(
      "已入账·迟到",
    );
  });

  test("异常红色，悬停提示写明原因", () => {
    const tag = orderStatusTag(order({ status: "abnormal", abnormal_reason: "amount_mismatch" }));
    expect(tag).toMatchObject({ label: "异常", tone: "danger", hint: "金额不符" });
    expect(abnormalReasonLabel("identity_mismatch")).toBe("商户身份不符");
    expect(abnormalReasonLabel("trade_no_conflict")).toBe("交易号冲突");
  });

  test("下单失败的提示是失败原因", () => {
    const tag = orderStatusTag(order({ status: "create_failed", fail_reason: "渠道超时" }));
    expect(tag).toMatchObject({ label: "下单失败", hint: "渠道超时" });
  });

  test("渠道通知结果标签，未知结果原样显示", () => {
    expect(notifyResultTag("credited")).toEqual({ label: "已入账", tone: "success" });
    expect(notifyResultTag("amount_mismatch").tone).toBe("danger");
    expect(notifyResultTag("whatever")).toEqual({ label: "whatever", tone: "neutral" });
  });
});

describe("订单状态时间线", () => {
  test("正常入账：创建 → 渠道已付 → 入账", () => {
    const timeline = orderTimeline(
      order({
        status: "credited",
        paid_at: "2026-10-16T10:01:00Z",
        credited_at: "2026-10-16T10:01:05Z",
      }),
    );
    expect(timeline.map((e) => e.key)).toEqual(["created", "paid", "credited"]);
  });

  test("关单后迟到付款：按时间排，关闭在付款之前", () => {
    const timeline = orderTimeline(
      order({
        status: "credited",
        late_paid: true,
        close_reason: "timeout",
        closed_at: "2026-10-16T10:15:00Z",
        paid_at: "2026-10-16T10:20:00Z",
        credited_at: "2026-10-16T10:20:03Z",
      }),
    );
    expect(timeline.map((e) => e.key)).toEqual(["created", "closed", "paid", "credited"]);
    expect(timeline[1].label).toBe("订单关闭 · 到期自动关闭");
    expect(timeline[3].label).toBe("积分入账（迟到付款）");
  });

  test("异常和下单失败没有时间点，排在最后并标红", () => {
    const abnormal = orderTimeline(
      order({
        status: "abnormal",
        abnormal_reason: "amount_mismatch",
        paid_at: "2026-10-16T10:01:00Z",
      }),
    );
    expect(abnormal.map((e) => e.key)).toEqual(["created", "paid", "abnormal"]);
    expect(abnormal[2]).toMatchObject({ at: null, tone: "danger" });
    const failed = orderTimeline(order({ status: "create_failed", fail_reason: "超时" }));
    expect(failed.at(-1)?.label).toBe("渠道下单失败 · 超时");
  });
});

describe("查单 / 关单的可用性", () => {
  const canQuery = { can_query: true, name: "易支付" };
  const noQuery = { can_query: false, name: "虎皮椒" };

  test("查单仅待支付和已关闭可用", () => {
    expect(queryBlockReason(order({ status: "pending" }), canQuery)).toBeNull();
    expect(queryBlockReason(order({ status: "closed" }), canQuery)).toBeNull();
    for (const status of ["credited", "abnormal", "create_failed", "creating"] as const)
      expect(queryBlockReason(order({ status }), canQuery)).not.toBeNull();
  });

  test("渠道不能查单时禁用并说明；渠道未知时不拦", () => {
    expect(queryBlockReason(order(), noQuery)).toContain("虎皮椒不支持查单");
    expect(queryBlockReason(order(), undefined)).toBeNull();
  });

  test("关单仅待支付可用", () => {
    expect(closeBlockReason(order({ status: "pending" }))).toBeNull();
    expect(closeBlockReason(order({ status: "closed" }))).not.toBeNull();
  });

  test("渠道不能在渠道侧关单时提醒迟到付款风险", () => {
    expect(closeUpstreamWarning({ can_close_upstream: false, name: "易支付" })).toContain(
      "迟到入账",
    );
    expect(closeUpstreamWarning({ can_close_upstream: true, name: "微信" })).toBeNull();
    expect(closeUpstreamWarning(undefined)).toBeNull();
  });
});

describe("全局设置", () => {
  const saved: PaymentSettings = {
    enabled: true,
    public_base_url: "https://example.com",
    method_wechat: "wechat_native",
    method_alipay: "",
    max_pending_per_user: 3,
  };

  test("公网地址：可空，填了必须是不带路径的 http(s) 地址，结尾斜杠会被去掉", () => {
    const form = settingsToForm(saved);
    expect(validateSettingsForm(form)).toEqual({});
    expect(validateSettingsForm({ ...form, publicBaseUrl: "" })).toEqual({});
    expect(validateSettingsForm({ ...form, publicBaseUrl: "https://a.com/" })).toEqual({});
    expect(validateSettingsForm({ ...form, publicBaseUrl: "http://a.com:8080" })).toEqual({});
    for (const bad of ["example.com", "ftp://a.com", "https://a.com/pay", "https://a b.com"])
      expect(validateSettingsForm({ ...form, publicBaseUrl: bad }).publicBaseUrl).toBeDefined();
    expect(normalizeBaseUrl(" https://a.com// ")).toBe("https://a.com");
  });

  test("每人最多未支付订单数 1–10", () => {
    const form = settingsToForm(saved);
    for (const ok of ["1", "10", "5"])
      expect(validateSettingsForm({ ...form, maxPending: ok }).maxPending).toBeUndefined();
    for (const bad of ["0", "11", "", "2.5", "x"])
      expect(validateSettingsForm({ ...form, maxPending: bad }).maxPending).toBeDefined();
  });

  test("保存请求带整份设置，数字换算正确", () => {
    const form = { ...settingsToForm(saved), publicBaseUrl: "https://a.com/", maxPending: "5" };
    expect(settingsFormToRequest(form)).toEqual({
      enabled: true,
      public_base_url: "https://a.com",
      method_wechat: "wechat_native",
      method_alipay: "",
      max_pending_per_user: 5,
    });
  });

  test("改动检测忽略结尾斜杠和首尾空格", () => {
    const form = settingsToForm(saved);
    expect(isSettingsDirty(form, saved)).toBe(false);
    expect(isSettingsDirty({ ...form, publicBaseUrl: "https://example.com/" }, saved)).toBe(false);
    expect(isSettingsDirty({ ...form, enabled: false }, saved)).toBe(true);
    expect(isSettingsDirty({ ...form, methodAlipay: "epay" }, saved)).toBe(true);
  });

  test("回调地址预览", () => {
    expect(callbackPreview("https://a.com/")).toBe(
      "https://a.com/api/v1/payment/notify/{provider}",
    );
    expect(callbackPreview("https://a.com", "epay")).toBe(
      "https://a.com/api/v1/payment/notify/epay",
    );
    expect(callbackPreview("  ")).toBeNull();
  });
});

describe("版本冲突识别", () => {
  test("只认业务码 65011，其余错误和非对象都不算", () => {
    expect(isPaymentVersionConflict({ code: 65011, status: 409 })).toBe(true);
    expect(isPaymentVersionConflict({ code: 65010, status: 409 })).toBe(false);
    expect(isPaymentVersionConflict({ status: 409 })).toBe(false);
    expect(isPaymentVersionConflict(null)).toBe(false);
    expect(isPaymentVersionConflict("65011")).toBe(false);
  });
});

describe("承接渠道下拉选项", () => {
  const ch = (
    provider: string,
    name: string,
    methods: PaymentChannel["methods"],
    enabled: boolean,
  ) => ({ provider, name, methods, enabled }) as PaymentChannel;
  const channels = [
    ch("wechat_native", "微信支付 Native", ["wechat"], true),
    ch("alipay_page", "支付宝电脑网站支付", ["alipay"], false),
    ch("epay", "易支付", ["wechat", "alipay"], false),
  ];

  test("第一项是不开放，只列支持该方式的渠道，未启用的置灰并标注", () => {
    expect(methodOptions(channels, "wechat")).toEqual([
      { value: "", label: "不开放", disabled: false },
      { value: "wechat_native", label: "微信支付 Native", disabled: false },
      { value: "epay", label: "易支付（未启用）", disabled: true },
    ]);
    expect(methodOptions(channels, "alipay").map((o) => o.value)).toEqual([
      "",
      "alipay_page",
      "epay",
    ]);
  });
});
