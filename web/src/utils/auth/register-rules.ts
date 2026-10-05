/** 验证码重发冷却（秒）：与后端同邮箱 60 秒限频一致 */
export const CODE_COOLDOWN_SECONDS = 60;

const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

/** 注册表单的取值 */
export type RegisterValues = {
  /** 用户名 */
  username: string;
  /** 邮箱 */
  email: string;
  /** 邮箱验证码 */
  code: string;
  /** 密码 */
  password: string;
  /** 确认密码 */
  confirm: string;
};

/** 注册表单的字段级错误，没有错误的字段不出现 */
export type RegisterErrors = Partial<Record<keyof RegisterValues, string>>;

/**
 * 校验邮箱格式，登录页和后台测试邮件收件人共用同一条规则
 * @param value 邮箱输入
 * @returns 错误文案；通过返回 null
 */
export function validateEmail(value: string): string | null {
  const email = value.trim();
  if (!email) return "请输入邮箱";
  return EMAIL_PATTERN.test(email) ? null : "邮箱格式不正确";
}

/**
 * 校验用户名：3 到 64 个字符，首尾空格不计入
 * @param value 用户名输入
 * @returns 错误文案；通过返回 null
 */
export function validateUsername(value: string): string | null {
  const length = [...value.trim()].length;
  if (length === 0) return "请输入用户名";
  if (length < 3) return "用户名至少 3 个字符";
  if (length > 64) return "用户名最多 64 个字符";
  return null;
}

/**
 * 校验密码：6 到 128 个字符
 * @param value 密码输入
 * @returns 错误文案；通过返回 null
 */
export function validatePassword(value: string): string | null {
  if (!value) return "请输入密码";
  if (value.length < 6) return "密码至少 6 位";
  if (value.length > 128) return "密码最多 128 位";
  return null;
}

/**
 * 校验确认密码与密码一致
 * @param password 密码
 * @param confirm 确认密码
 * @returns 错误文案；通过返回 null
 */
export function validateConfirm(password: string, confirm: string): string | null {
  if (!confirm) return "请再次输入密码";
  return password === confirm ? null : "两次输入的密码不一致";
}

/**
 * 校验整张注册表单
 * @param values 表单取值
 * @param needCode 是否需要验证码（auth/config 的 email_verify_required）
 * @returns 字段级错误；全部通过时为空对象
 */
export function validateRegisterForm(values: RegisterValues, needCode: boolean): RegisterErrors {
  const errors: RegisterErrors = {};
  const username = validateUsername(values.username);
  if (username) errors.username = username;
  const email = validateEmail(values.email);
  if (email) errors.email = email;
  if (needCode && !/^\d{6}$/.test(values.code.trim())) errors.code = "请输入 6 位数字验证码";
  const password = validatePassword(values.password);
  if (password) errors.password = password;
  const confirm = validateConfirm(values.password, values.confirm);
  if (confirm) errors.confirm = confirm;
  return errors;
}

/**
 * 是否存在字段错误
 * @param errors 字段级错误
 */
export function hasRegisterErrors(errors: RegisterErrors): boolean {
  return Object.keys(errors).length > 0;
}

/**
 * 倒计时走一秒，到 0 停住
 * @param remaining 当前剩余秒数
 * @returns 下一秒的剩余秒数
 */
export function nextCountdown(remaining: number): number {
  return Math.max(0, remaining - 1);
}

/**
 * 「发送验证码」按钮的状态：发送中、冷却中、邮箱不合法时都禁用
 * @param input 剩余冷却秒数、是否发送中、邮箱是否合法
 * @returns 是否禁用与按钮文案
 */
export function codeButtonState(input: {
  remaining: number;
  sending: boolean;
  emailValid: boolean;
}): { disabled: boolean; label: string } {
  if (input.sending) return { disabled: true, label: "发送中..." };
  if (input.remaining > 0) return { disabled: true, label: `${input.remaining} 秒后重发` };
  return { disabled: !input.emailValid, label: "发送验证码" };
}

/** 登录 / 注册业务错误的就地提示：field 为 form 表示表单级 */
export type AuthErrorHint = {
  /** 提示落在哪个字段，form 表示整张表单 */
  field: keyof RegisterValues | "form";
  /** 提示文案 */
  message: string;
};

/**
 * 把后端业务错误码翻译成就地提示。文案与全局 toast 一致，且不泄露额外信息；
 * 其他错误返回 null，只靠全局 toast。
 * @param code ApiError.code
 * @returns 就地提示；不需要就地提示返回 null
 */
export function mapAuthError(code: unknown): AuthErrorHint | null {
  switch (code) {
    case 20002:
      return { field: "username", message: "用户名已被占用" };
    case 53001:
      return { field: "form", message: "暂未开放注册" };
    case 53002:
      return { field: "email", message: "该邮箱已注册" };
    case 53003:
      return { field: "code", message: "验证码错误或已过期" };
    case 53004:
      return { field: "form", message: "账号已被停用，请联系管理员" };
    default:
      return null;
  }
}

/**
 * 注册成功后是否提示「你是首个用户，已成为超级管理员」：以服务端返回的角色为准。
 * 不能用「是否需要验证码」推断——没启用邮件服务或关了验证开关时，所有人都不需要验证码。
 * @param role 注册响应里的角色
 */
export function becameFirstAdmin(role: string | null | undefined): boolean {
  return role === "super_admin";
}
