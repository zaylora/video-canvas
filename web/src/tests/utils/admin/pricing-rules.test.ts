import { describe, expect, test } from "bun:test";

import type { PricingSettingsView } from "@/api/admin/settings/type";
import {
  canSavePricing,
  describeUsdCny,
  pricingFromView,
  pricingToRequest,
  validatePricing,
  type PricingValues,
} from "@/utils/admin/pricing-rules";

const values = (over: Partial<PricingValues> = {}): PricingValues => ({
  credits_per_yuan: "7.5",
  default_markup_pct: "30",
  usd_cny_override: "",
  ...over,
});

const view = (over: Partial<PricingSettingsView> = {}): PricingSettingsView => ({
  credits_per_yuan: 7.5,
  default_markup_pct: 30,
  usd_cny_override: null,
  usd_cny: {
    rate: 6.8,
    date: "2026-10-30",
    fetched_at: "2026-10-31T01:00:00Z",
    source: "live",
    stale: false,
  },
  ...over,
});

describe("定价设置表单校验", () => {
  test("合法输入没有错误", () => {
    expect(validatePricing(values())).toEqual({});
    expect(validatePricing(values({ usd_cny_override: "6.6529" }))).toEqual({});
  });

  test("积分换算必须大于 0 且最多两位小数；未设置时必填", () => {
    expect(validatePricing(values({ credits_per_yuan: "" })).credits_per_yuan).toBeDefined();
    expect(validatePricing(values({ credits_per_yuan: "0" })).credits_per_yuan).toBeDefined();
    expect(validatePricing(values({ credits_per_yuan: "-1" })).credits_per_yuan).toBeDefined();
    expect(validatePricing(values({ credits_per_yuan: "7.555" })).credits_per_yuan).toBeDefined();
    expect(validatePricing(values({ credits_per_yuan: "abc" })).credits_per_yuan).toBeDefined();
    expect(validatePricing(values({ credits_per_yuan: "7.55" })).credits_per_yuan).toBeUndefined();
    expect(validatePricing(values({ credits_per_yuan: " 8 " })).credits_per_yuan).toBeUndefined();
  });

  test("默认加价率是 0 到 1000 的整数", () => {
    const err = (v: string) =>
      validatePricing(values({ default_markup_pct: v })).default_markup_pct;
    expect(err("0")).toBeUndefined();
    expect(err("1000")).toBeUndefined();
    expect(err("1001")).toBeDefined();
    expect(err("-1")).toBeDefined();
    expect(err("12.5")).toBeDefined();
    expect(err("")).toBeDefined();
  });

  test("汇率覆盖可空；填写时必须大于 0 且最多四位小数", () => {
    const err = (v: string) => validatePricing(values({ usd_cny_override: v })).usd_cny_override;
    expect(err("")).toBeUndefined();
    expect(err("0")).toBeDefined();
    expect(err("6.65291")).toBeDefined();
    expect(err("x")).toBeDefined();
  });
});

describe("定价设置表单与请求", () => {
  test("视图转表单：未设置的积分换算是空串，没有覆盖是空串", () => {
    expect(pricingFromView(view({ credits_per_yuan: null }))).toEqual({
      credits_per_yuan: "",
      default_markup_pct: "30",
      usd_cny_override: "",
    });
    expect(pricingFromView(view({ usd_cny_override: 6.6 })).usd_cny_override).toBe("6.6");
  });

  test("表单转请求：覆盖留空是 null，数字去掉首尾空格", () => {
    expect(pricingToRequest(values({ credits_per_yuan: " 7.5 ", usd_cny_override: "" }))).toEqual({
      credits_per_yuan: 7.5,
      default_markup_pct: 30,
      usd_cny_override: null,
    });
    expect(pricingToRequest(values({ usd_cny_override: "6.6529" })).usd_cny_override).toBe(6.6529);
  });

  test("没有改动、有错误或保存中都不能保存", () => {
    const initial = values();
    expect(canSavePricing({ values: initial, initial })).toBe(false);
    expect(canSavePricing({ values: values({ default_markup_pct: "35" }), initial })).toBe(true);
    expect(canSavePricing({ values: values({ default_markup_pct: "x" }), initial })).toBe(false);
    expect(
      canSavePricing({ values: values({ default_markup_pct: "35" }), initial, saving: true }),
    ).toBe(false);
  });

  test("积分换算尚未设置时，填上就可以保存", () => {
    const initial = values({ credits_per_yuan: "" });
    expect(canSavePricing({ values: initial, initial })).toBe(false);
    expect(canSavePricing({ values: values({ credits_per_yuan: "7.5" }), initial })).toBe(true);
  });
});

describe("汇率的展示", () => {
  test("实时汇率：来源、日期、未过期", () => {
    expect(describeUsdCny(view().usd_cny)).toEqual({
      rate: "6.8",
      source: "实时",
      date: "2026-10-30",
      stale: false,
    });
  });

  test("手动覆盖没有日期；从未拉到且没有覆盖时没有汇率", () => {
    expect(
      describeUsdCny({ rate: 6.6, date: null, fetched_at: null, source: "override", stale: false }),
    ).toEqual({ rate: "6.6", source: "手动", date: null, stale: false });
    expect(
      describeUsdCny({ rate: null, date: null, fetched_at: null, source: "none", stale: false }),
    ).toEqual({ rate: null, source: "未获取", date: null, stale: false });
  });

  test("实时值过期会标出来", () => {
    expect(describeUsdCny({ ...view().usd_cny, stale: true }).stale).toBe(true);
  });
});
