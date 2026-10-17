import type { PaymentChannel, PaymentMethod, PaymentSettings } from "@/api/admin/payment/type.d";

/** 每人最多未支付订单数的范围，与后端 min=1,max=10 一致 */
export const MAX_PENDING_RANGE = { min: 1, max: 10 } as const;

/** 支付方式的短名，用在表格和下拉里 */
export const METHOD_LABEL: Record<PaymentMethod, string> = {
  wechat: "微信",
  alipay: "支付宝",
};

/** 支付方式的完整名，用在“承接：微信支付”这类句子里 */
export const METHOD_FULL_LABEL: Record<PaymentMethod, string> = {
  wechat: "微信支付",
  alipay: "支付宝",
};

/** 全局设置表单的取值：数字用原始文本，提交前再换算 */
export interface SettingsFormValues {
  /** 支付总开关 */
  enabled: boolean;
  /** 站点公网地址 */
  publicBaseUrl: string;
  /** 微信承接渠道，空串表示不开放 */
  methodWechat: string;
  /** 支付宝承接渠道，空串表示不开放 */
  methodAlipay: string;
  /** 每人最多未支付订单数 */
  maxPending: string;
}

/** 全局设置表单的错误：没问题的字段不出现 */
export type SettingsFormErrors = Partial<Record<"publicBaseUrl" | "maxPending", string>>;

/**
 * 由服务端的设置填充表单
 * @param settings 已保存的设置
 * @returns 表单取值
 */
export const settingsToForm = (settings: PaymentSettings): SettingsFormValues => ({
  enabled: settings.enabled,
  publicBaseUrl: settings.public_base_url,
  methodWechat: settings.method_wechat,
  methodAlipay: settings.method_alipay,
  maxPending: String(settings.max_pending_per_user),
});

/**
 * 去掉首尾空格和结尾的斜杠：回调地址是用 `${base}/api/v1/...` 拼的，结尾斜杠会拼出双斜杠
 * @param value 输入框里的公网地址
 * @returns 规整后的地址
 */
export const normalizeBaseUrl = (value: string): string => value.trim().replace(/\/+$/, "");

/**
 * 校验全局设置表单
 * @param values 表单取值
 * @returns 各字段的错误；全部通过时为空对象。公网地址可以留空（还没配置），但填了就必须是不带路径的 http(s) 地址
 */
export function validateSettingsForm(values: SettingsFormValues): SettingsFormErrors {
  const errors: SettingsFormErrors = {};
  const base = normalizeBaseUrl(values.publicBaseUrl);
  if (base) {
    if (base.length > 200) errors.publicBaseUrl = "地址太长";
    else if (!/^https?:\/\/[^/\s?#]+$/i.test(base))
      errors.publicBaseUrl = "请填写不带路径的地址，如 https://example.com";
  }
  const pending = Number(values.maxPending);
  if (
    !/^\d+$/.test(values.maxPending.trim()) ||
    pending < MAX_PENDING_RANGE.min ||
    pending > MAX_PENDING_RANGE.max
  )
    errors.maxPending = `请填写 ${MAX_PENDING_RANGE.min}–${MAX_PENDING_RANGE.max} 之间的整数`;
  return errors;
}

/**
 * 把表单换算成保存请求。调用前必须先通过 validateSettingsForm。
 * @param values 表单取值
 * @returns 提交给后端的整份设置
 */
export const settingsFormToRequest = (values: SettingsFormValues): PaymentSettings => ({
  enabled: values.enabled,
  public_base_url: normalizeBaseUrl(values.publicBaseUrl),
  method_wechat: values.methodWechat,
  method_alipay: values.methodAlipay,
  max_pending_per_user: Number(values.maxPending),
});

/**
 * 表单相对已保存的设置有没有改动（决定保存按钮能不能点）
 * @param values 表单取值
 * @param saved 已保存的设置
 */
export function isSettingsDirty(values: SettingsFormValues, saved: PaymentSettings): boolean {
  const initial = settingsToForm(saved);
  return (
    values.enabled !== initial.enabled ||
    normalizeBaseUrl(values.publicBaseUrl) !== normalizeBaseUrl(initial.publicBaseUrl) ||
    values.methodWechat !== initial.methodWechat ||
    values.methodAlipay !== initial.methodAlipay ||
    values.maxPending.trim() !== initial.maxPending
  );
}

/**
 * 回调地址预览：与后端拼 notify_url 的规则一致。
 * @param baseUrl 站点公网地址
 * @param provider 渠道标识；不传显示占位符 {provider}
 * @returns 地址文本；还没配置公网地址时为 null
 */
export function callbackPreview(baseUrl: string, provider = "{provider}"): string | null {
  const base = normalizeBaseUrl(baseUrl);
  return base ? `${base}/api/v1/payment/notify/${provider}` : null;
}

/** 承接下拉里的一项 */
export interface MethodOption {
  /** 渠道标识；空串表示“不开放” */
  value: string;
  /** 显示文字 */
  label: string;
  /** 未启用的渠道置灰，不能新选 */
  disabled: boolean;
}

/**
 * 某种支付方式“由谁承接”的下拉选项：第一项是“不开放”，其后是支持该方式的渠道，未启用的置灰并标「（未启用）」。
 * 当前已选的渠道即使未启用也保留可见，避免下拉显示成空白。
 * @param channels 渠道列表
 * @param method 支付方式
 */
export function methodOptions(channels: PaymentChannel[], method: PaymentMethod): MethodOption[] {
  return [
    { value: "", label: "不开放", disabled: false },
    ...channels
      .filter((channel) => channel.methods.includes(method))
      .map((channel) => ({
        value: channel.provider,
        label: channel.enabled ? channel.name : `${channel.name}（未启用）`,
        disabled: !channel.enabled,
      })),
  ];
}
