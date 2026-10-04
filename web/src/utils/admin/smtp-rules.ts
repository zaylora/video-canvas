import type { SmtpEncryption } from "@/api/admin-settings/type";
import { validateEmail } from "@/utils/auth/register-rules";

export type { SmtpEncryption };

/** SMTP 表单取值：端口用字符串承接输入，密码不在表单里（只写不读） */
export type SmtpFormValues = {
  /** SMTP 服务器主机名 */
  host: string;
  /** 端口输入 */
  port: string;
  /** 加密方式 */
  encryption: SmtpEncryption;
  /** 登录用户名，免认证 SMTP 可空 */
  username: string;
  /** 发件人地址 */
  from_address: string;
  /** 发件人名称，可空 */
  from_name: string;
  /** 是否启用 */
  enabled: boolean;
};

/** SMTP 表单字段级错误 */
export type SmtpErrors = Partial<
  Record<"host" | "port" | "from_address" | "from_name" | "username", string>
>;

/** 注册设置表单取值：数字用字符串承接输入 */
export type RegisterSettingsValues = {
  /** 是否开放注册 */
  register_enabled: boolean;
  /** 注册是否需要验证邮箱 */
  verify_email: boolean;
  /** 新用户初始积分输入 */
  initial_credits: string;
  /** 默认并发上限输入 */
  default_max_active_tasks: string;
};

/** 注册设置字段级错误 */
export type RegisterSettingsErrors = Partial<
  Record<"initial_credits" | "default_max_active_tasks", string>
>;

/** 默认端口：加密方式切换时给出常用值 */
export const SMTP_DEFAULT_PORT: Record<SmtpEncryption, string> = {
  none: "25",
  starttls: "587",
  tls: "465",
};

/**
 * 把输入解析成整数，不是纯整数返回 null
 * @param value 输入
 */
function parseInteger(value: string): number | null {
  const text = value.trim();
  return /^-?\d+$/.test(text) ? Number(text) : null;
}

/**
 * 校验 SMTP 表单；内网地址等由后端校验并返回原因
 * @param values 表单取值
 * @returns 字段级错误；全部通过时为空对象
 */
export function validateSmtpForm(values: SmtpFormValues): SmtpErrors {
  const errors: SmtpErrors = {};
  const host = values.host.trim();
  if (!host) errors.host = "请输入服务器地址";
  else if (/[/\s:@]/.test(host)) errors.host = "只填主机名，不带协议、端口或路径";
  const port = parseInteger(values.port);
  if (port === null || port < 1 || port > 65535) errors.port = "端口范围 1 到 65535";
  const from = validateEmail(values.from_address);
  if (from) errors.from_address = from;
  return errors;
}

/**
 * 任一字段有错误
 * @param errors 字段级错误
 */
export function hasSmtpErrors(errors: object): boolean {
  return Object.keys(errors).length > 0;
}

/**
 * 校验注册设置表单
 * @param values 初始积分与默认并发输入
 * @returns 字段级错误
 */
export function validateRegisterSettings(
  values: Pick<RegisterSettingsValues, "initial_credits" | "default_max_active_tasks">,
): RegisterSettingsErrors {
  const errors: RegisterSettingsErrors = {};
  const credits = parseInteger(values.initial_credits);
  if (credits === null || credits < 0) errors.initial_credits = "请输入不小于 0 的整数";
  const limit = parseInteger(values.default_max_active_tasks);
  if (limit === null || limit < 1 || limit > 64) errors.default_max_active_tasks = "范围 1 到 64";
  return errors;
}

/**
 * 两份表单是否一致（逐字段比较，忽略首尾空格）
 */
function sameValues<T extends object>(a: T, b: T): boolean {
  return (Object.keys(a) as (keyof T)[]).every((key) => {
    const x = a[key];
    const y = b[key];
    return typeof x === "string" && typeof y === "string" ? x.trim() === y.trim() : x === y;
  });
}

/**
 * SMTP 能否保存：有权限、有改动、无校验错误、不在保存中
 * @param input 当前值、已保存值、是否可写、是否保存中
 */
export function canSaveSmtp(input: {
  values: SmtpFormValues;
  initial: SmtpFormValues;
  canWrite: boolean;
  saving?: boolean;
}): boolean {
  if (!input.canWrite || input.saving) return false;
  if (sameValues(input.values, input.initial)) return false;
  return !hasSmtpErrors(validateSmtpForm(input.values));
}

/**
 * 能否发测试邮件：测试用的是已保存配置，所以表单有未保存改动时不允许
 * @param input 是否有未保存改动、是否已保存过配置、收件邮箱、是否可写、是否发送中
 */
export function canSendTest(input: {
  dirty: boolean;
  configured: boolean;
  to: string;
  canWrite: boolean;
  busy?: boolean;
}): boolean {
  if (!input.canWrite || input.busy || input.dirty || !input.configured) return false;
  return validateEmail(input.to) === null;
}

/**
 * 注册设置能否保存：有权限、有改动、无校验错误、不在保存中
 * @param input 当前值、已保存值、是否可写、是否保存中
 */
export function canSaveRegisterSettings(input: {
  values: RegisterSettingsValues;
  initial: RegisterSettingsValues;
  canWrite: boolean;
  saving?: boolean;
}): boolean {
  if (!input.canWrite || input.saving) return false;
  if (sameValues(input.values, input.initial)) return false;
  return !hasSmtpErrors(validateRegisterSettings(input.values));
}
