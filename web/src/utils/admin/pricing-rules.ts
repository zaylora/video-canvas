import type {
  PricingSettingsReq,
  PricingSettingsView,
  PricingUsdCny,
} from "@/api/admin/settings/type";

/** 定价设置表单取值：数字用字符串承接输入 */
export type PricingValues = {
  /** 1 元 = N 积分的输入 */
  credits_per_yuan: string;
  /** 默认加价率输入 */
  default_markup_pct: string;
  /** 汇率覆盖输入，空串表示使用实时汇率 */
  usd_cny_override: string;
};

/** 定价设置字段级错误 */
export type PricingErrors = Partial<Record<keyof PricingValues, string>>;

/**
 * 解析正小数：必须大于 0，且小数位数不超过 maxDecimals
 * @param value 输入
 * @param maxDecimals 允许的最多小数位
 * @returns 数值；不合法返回 null
 */
function parsePositiveDecimal(value: string, maxDecimals: number): number | null {
  const text = value.trim();
  if (!new RegExp(`^\\d+(\\.\\d{1,${maxDecimals}})?$`).test(text)) return null;
  const n = Number(text);
  return n > 0 ? n : null;
}

/**
 * 校验定价设置表单
 * @param values 表单取值
 * @returns 字段级错误；全部通过时为空对象
 */
export function validatePricing(values: PricingValues): PricingErrors {
  const errors: PricingErrors = {};
  if (parsePositiveDecimal(values.credits_per_yuan, 2) === null)
    errors.credits_per_yuan = "请输入大于 0 的数，最多两位小数";
  const markup = values.default_markup_pct.trim();
  if (!/^\d+$/.test(markup) || Number(markup) > 1000)
    errors.default_markup_pct = "请输入 0 到 1000 的整数";
  const override = values.usd_cny_override.trim();
  if (override && parsePositiveDecimal(override, 4) === null)
    errors.usd_cny_override = "请输入大于 0 的数，最多四位小数，或留空";
  return errors;
}

/**
 * 服务端视图转表单取值
 * @param view 定价设置视图
 */
export function pricingFromView(view: PricingSettingsView): PricingValues {
  return {
    credits_per_yuan: view.credits_per_yuan === null ? "" : String(view.credits_per_yuan),
    default_markup_pct: String(view.default_markup_pct),
    usd_cny_override: view.usd_cny_override === null ? "" : String(view.usd_cny_override),
  };
}

/**
 * 表单取值转保存请求；调用前要先通过 validatePricing
 * @param values 表单取值
 */
export function pricingToRequest(values: PricingValues): PricingSettingsReq {
  const override = values.usd_cny_override.trim();
  return {
    credits_per_yuan: Number(values.credits_per_yuan.trim()),
    default_markup_pct: Number(values.default_markup_pct.trim()),
    usd_cny_override: override ? Number(override) : null,
  };
}

/**
 * 能否保存：有改动、无校验错误、不在保存中
 * @param input 当前值、已保存值、是否保存中
 */
export function canSavePricing(input: {
  values: PricingValues;
  initial: PricingValues;
  saving?: boolean;
}): boolean {
  if (input.saving) return false;
  const same = (Object.keys(input.values) as (keyof PricingValues)[]).every(
    (key) => input.values[key].trim() === input.initial[key].trim(),
  );
  if (same) return false;
  return Object.keys(validatePricing(input.values)).length === 0;
}

/** 汇率在页面上的展示 */
export type UsdCnyView = {
  /** 汇率文本，没有为 null */
  rate: string | null;
  /** 来源的中文名 */
  source: string;
  /** 汇率日期，没有为 null */
  date: string | null;
  /** 实时值是否过期 */
  stale: boolean;
};

/** 来源的中文名 */
const SOURCE_LABEL: Record<PricingUsdCny["source"], string> = {
  live: "实时",
  override: "手动",
  none: "未获取",
};

/**
 * 当前生效汇率的展示
 * @param usd 服务端给的汇率
 */
export function describeUsdCny(usd: PricingUsdCny): UsdCnyView {
  return {
    rate: usd.rate === null ? null : String(usd.rate),
    source: SOURCE_LABEL[usd.source],
    date: usd.date,
    stale: usd.stale,
  };
}
