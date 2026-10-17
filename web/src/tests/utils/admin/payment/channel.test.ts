import { describe, expect, test } from "bun:test";

import type { PaymentChannel, PaymentField } from "@/api/admin/payment/type.d";
import {
  buildChannelRequest,
  changedConfigKeys,
  channelToForm,
  fieldPlaceholder,
  invalidatesValidation,
  isChannelFormDirty,
  isFieldLocked,
  secretPlaceholder,
  validateTimeout,
} from "@/utils/admin/payment/channel-form";
import {
  capabilityTags,
  channelStatus,
  checkoutLabel,
  disableConsequence,
  enableBlockReason,
  methodsText,
  notifyUrlProblem,
  servingText,
} from "@/utils/admin/payment/channel";

const field = (patch: Partial<PaymentField> & Pick<PaymentField, "key">): PaymentField => ({
  label: patch.key,
  secret: false,
  identity: false,
  optional: false,
  value: "",
  secret_set: false,
  ...patch,
});

const channel = (patch: Partial<PaymentChannel> = {}): PaymentChannel => ({
  provider: "epay",
  name: "易支付",
  methods: ["wechat", "alipay"],
  checkout: "redirect",
  can_query: true,
  can_close_upstream: false,
  fields: [
    field({ key: "api_url", value: "https://pay.example.com" }),
    field({ key: "pid", identity: true, value: "1001" }),
    field({ key: "note", optional: true, value: "", default: "充值" }),
    field({ key: "key", secret: true, identity: true, secret_set: true }),
  ],
  configured: true,
  enabled: false,
  validated: false,
  validated_at: null,
  version: 4,
  order_timeout_minutes: 15,
  open_orders: 0,
  notify_url: "https://example.com/api/v1/payment/notify/epay",
  serving: [],
  probe_skipped: false,
  updated_at: null,
  ...patch,
});

describe("渠道状态判定", () => {
  test("已启用 > 已校验未启用 > 待校验 > 未配置", () => {
    expect(channelStatus(channel({ enabled: true, validated: true })).label).toBe("已启用");
    expect(channelStatus(channel({ validated: true })).label).toBe("已校验未启用");
    expect(channelStatus(channel()).label).toBe("待校验");
    expect(channelStatus(channel({ configured: false, version: 0 })).label).toBe("未配置");
  });

  test("语气：启用绿、已校验蓝、待校验黄、未配置灰", () => {
    expect(channelStatus(channel({ enabled: true })).tone).toBe("success");
    expect(channelStatus(channel({ validated: true })).tone).toBe("info");
    expect(channelStatus(channel()).tone).toBe("warning");
    expect(channelStatus(channel({ configured: false })).tone).toBe("neutral");
  });
});

describe("能力标签与文案", () => {
  test("查单绿/红，渠道侧关单绿/仅本地关单黄", () => {
    expect(capabilityTags({ can_query: true, can_close_upstream: true })).toEqual([
      { label: "可查单", tone: "success" },
      { label: "渠道侧关单", tone: "success" },
    ]);
    expect(capabilityTags({ can_query: false, can_close_upstream: false })).toEqual([
      { label: "不能查单", tone: "danger" },
      { label: "仅本地关单", tone: "warning" },
    ]);
  });

  test("收银形态、支付方式、承接文案", () => {
    expect(checkoutLabel("qrcode")).toBe("扫码");
    expect(checkoutLabel("redirect")).toBe("跳转");
    expect(checkoutLabel("mixed")).toBe("跳转或扫码");
    expect(checkoutLabel("future_mode")).toBe("future_mode");
    expect(methodsText(["wechat", "alipay"])).toBe("微信、支付宝");
    expect(servingText(["wechat"])).toBe("承接：微信支付");
    expect(servingText(["wechat", "alipay"])).toBe("承接：微信支付、支付宝");
    expect(servingText([])).toBe("未承接");
  });
});

describe("启用前的回调地址检查", () => {
  test("没有回调地址时要求先配置公网地址", () => {
    expect(notifyUrlProblem({ provider: "epay", notify_url: "" })).toContain("公网地址");
  });

  test("微信渠道必须是 https，其他渠道不限", () => {
    const http = "http://example.com/api/v1/payment/notify/x";
    expect(notifyUrlProblem({ provider: "wechat_native", notify_url: http })).toContain("https");
    expect(notifyUrlProblem({ provider: "epay", notify_url: http })).toBeNull();
    expect(
      notifyUrlProblem({ provider: "wechat_native", notify_url: "https://example.com/x" }),
    ).toBeNull();
  });

  test("没通过校验不能启用；通过且地址没问题可以启用", () => {
    expect(enableBlockReason(channel())).toBe("配置还没有通过校验");
    expect(enableBlockReason(channel({ validated: true }))).toBeNull();
    expect(enableBlockReason(channel({ validated: true, notify_url: "" }))).toContain("公网地址");
  });

  test("停用后果写明它承接的支付方式", () => {
    expect(disableConsequence({ serving: ["wechat"] })).toContain("微信支付会在前台立即不可用");
    expect(disableConsequence({ serving: ["wechat"] })).toContain("已有订单仍会正常入账");
    expect(disableConsequence({ serving: [] })).toContain("不影响前台");
  });
});

describe("渠道配置表单", () => {
  test("初始化：普通字段带出当前值，密钥不回显，超时取已保存值", () => {
    const form = channelToForm(channel());
    expect(form.config).toEqual({ api_url: "https://pay.example.com", pid: "1001", note: "" });
    expect(form.secrets).toEqual({});
    expect(form.timeout).toBe("15");
  });

  test("保存请求带全部非敏感字段，只带真的填了的密钥，version 取抽屉打开时的值", () => {
    const c = channel();
    const form = channelToForm(c);
    form.config.pid = " 2002 ";
    form.secrets.key = "";
    expect(buildChannelRequest(c, form)).toEqual({
      version: 4,
      config: { api_url: "https://pay.example.com", pid: "2002", note: "" },
      secrets: {},
      order_timeout_minutes: 15,
    });
    form.secrets.key = " new-secret ";
    form.timeout = "30";
    const body = buildChannelRequest(c, form);
    expect(body.secrets).toEqual({ key: "new-secret" });
    expect(body.order_timeout_minutes).toBe(30);
  });

  test("没改任何普通字段时，config 仍然是完整取值（缺少会被后端当成清空）", () => {
    const c = channel();
    expect(Object.keys(buildChannelRequest(c, channelToForm(c)).config).sort()).toEqual([
      "api_url",
      "note",
      "pid",
    ]);
  });

  test("差异计算：哪些普通字段被改了、密钥是否填了", () => {
    const c = channel();
    const form = channelToForm(c);
    expect(changedConfigKeys(c, form)).toEqual([]);
    form.config.api_url = "https://other.example.com";
    expect(changedConfigKeys(c, form)).toEqual(["api_url"]);
    form.config.api_url = " https://pay.example.com ";
    expect(changedConfigKeys(c, form)).toEqual([]);
    form.secrets.key = "   ";
    expect(invalidatesValidation(c, form)).toBe(false);
    form.secrets.key = "x";
    expect(invalidatesValidation(c, form)).toBe(true);
  });

  test("只改超时时间不会让校验作废，但算有改动", () => {
    const c = channel({ enabled: true, validated: true });
    const form = channelToForm(c);
    expect(isChannelFormDirty(c, form)).toBe(false);
    form.timeout = "30";
    expect(invalidatesValidation(c, form)).toBe(false);
    expect(isChannelFormDirty(c, form)).toBe(true);
  });

  test("没配置过的渠道没改动也允许保存", () => {
    const c = channel({ configured: false, version: 0 });
    expect(isChannelFormDirty(c, channelToForm(c))).toBe(true);
  });

  test("超时范围 5–120 的整数", () => {
    expect(validateTimeout("5")).toBeNull();
    expect(validateTimeout("120")).toBeNull();
    for (const bad of ["4", "121", "", "abc", "15.5", "-5"])
      expect(validateTimeout(bad)).not.toBeNull();
  });

  test("有未完成订单时锁定商户身份与密钥字段，普通字段不锁", () => {
    const c = channel({ open_orders: 2 });
    const byKey = Object.fromEntries(c.fields.map((f) => [f.key, f]));
    expect(isFieldLocked(c, byKey.pid)).toBe(true);
    expect(isFieldLocked(c, byKey.key)).toBe(true);
    expect(isFieldLocked(c, byKey.api_url)).toBe(false);
    expect(isFieldLocked(channel({ open_orders: 0 }), byKey.pid)).toBe(false);
  });

  test("占位文字：密钥看是否已设置，普通字段优先 placeholder 再缺省值", () => {
    expect(secretPlaceholder(field({ key: "k", secret: true, secret_set: true }))).toBe(
      "已设置 · 留空不修改",
    );
    expect(secretPlaceholder(field({ key: "k", secret: true }))).toBe("未设置");
    expect(fieldPlaceholder(field({ key: "a", placeholder: "https://…", default: "x" }))).toBe(
      "https://…",
    );
    expect(fieldPlaceholder(field({ key: "a", default: "充值" }))).toBe("缺省 充值");
    expect(fieldPlaceholder(field({ key: "a" }))).toBe("");
  });
});
