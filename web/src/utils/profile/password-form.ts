import type { LoginResponse } from "@/api/auth";
import type { ChangePasswordRequest } from "@/api/me/type";
import {
  PASSWORD_MIN_BYTES,
  passwordBytes,
  validatePassword,
  WEAK_PASSWORD_CODE,
} from "@/utils/auth/register-rules";
import { ApiError } from "@/utils/requests/request";

/** 当前密码错误（Msg 里附带剩余次数） */
const WRONG_OLD_CODE = 55001;
/** 新密码与当前密码相同 */
const SAME_AS_OLD_CODE = 55002;
/** 尝试过于频繁，已锁定 */
const LOCKED_CODE = 55004;
/** 参数错误：改密码时只会是新密码长度不合法 */
const INVALID_PARAM_CODE = 10001;

/** 输错太多次后的锁定时长：15 分钟，与后端一致 */
export const PASSWORD_LOCK_MS = 15 * 60 * 1000;

/** 改密码表单的取值 */
export type PasswordValues = {
  /** 当前密码 */
  old: string;
  /** 新密码 */
  next: string;
  /** 确认新密码 */
  confirm: string;
};

/** 字段下方提示：muted 说明、ok 通过、error 错误 */
export type FieldHint = {
  /** 颜色语义 */
  tone: "muted" | "ok" | "error";
  /** 文案 */
  text: string;
};

/** 后端错误的就地提示：form 表示表单顶部的错误框 */
export type PasswordErrorHint = {
  /** 落在哪个字段 */
  field: "old" | "next" | "form";
  /** 文案 */
  message: string;
  /** 为 true 时表单锁定 15 分钟并倒计时 */
  lock?: boolean;
};

/**
 * 新密码与确认密码的即时提示（只做提示，以后端为准）。规则与注册共用 validatePassword
 * @param values 表单取值
 * @param username 用户名
 * @returns 新密码提示与确认密码提示（确认为空时为 null）
 */
export function passwordHints(
  values: PasswordValues,
  username: string,
): { next: FieldHint; confirm: FieldHint | null } {
  const { old, next, confirm } = values;
  let nextHint: FieldHint;
  if (!next) {
    nextHint = { tone: "muted", text: "8–72 字节，不要求字符组合；允许粘贴，可配合密码管理器" };
  } else if (passwordBytes(next) < PASSWORD_MIN_BYTES) {
    nextHint = { tone: "muted", text: `已输入 ${passwordBytes(next)} / 至少 8 位` };
  } else {
    const error = validatePassword(next, username);
    if (error) nextHint = { tone: "error", text: error };
    else if (old && next === old) nextHint = { tone: "error", text: "新密码不能与当前密码相同" };
    else nextHint = { tone: "ok", text: `已输入 ${passwordBytes(next)} 字节 · 长度符合` };
  }
  let confirmHint: FieldHint | null = null;
  if (confirm) {
    confirmHint =
      confirm === next
        ? { tone: "ok", text: "两次输入一致" }
        : { tone: "error", text: "两次输入的密码不一致" };
  }
  return { next: nextHint, confirm: confirmHint };
}

/**
 * 提交按钮是否可用：当前密码已填、新密码合法且与当前密码不同、两次一致、未锁定、不在提交中
 * @param values 表单取值
 * @param username 用户名
 * @param state 是否锁定、是否提交中
 */
export function canSubmitPassword(
  values: PasswordValues,
  username: string,
  state: { locked: boolean; saving: boolean },
): boolean {
  if (state.locked || state.saving || !values.old) return false;
  if (validatePassword(values.next, username)) return false;
  return values.next !== values.old && values.confirm === values.next;
}

/**
 * 改密码的业务错误翻译成就地提示；其他错误只靠全局 toast，返回 null
 * @param code ApiError.code
 * @param message 后端文案：55001 里带剩余次数，55003 区分「过于常见」和「与用户名相同」
 */
export function mapPasswordError(code: unknown, message: string): PasswordErrorHint | null {
  switch (code) {
    case WRONG_OLD_CODE:
      return { field: "old", message: message || "当前密码错误" };
    case SAME_AS_OLD_CODE:
      return { field: "next", message: "新密码不能与当前密码相同" };
    case WEAK_PASSWORD_CODE:
      return { field: "next", message: message || "这个密码过于常见，请换一个" };
    case INVALID_PARAM_CODE:
      return { field: "next", message: message || "新密码长度不合法" };
    case LOCKED_CODE:
      return { field: "form", message: "尝试过于频繁，请 15 分钟后再试", lock: true };
    default:
      return null;
  }
}

/**
 * 锁定倒计时文案 mm:ss，向上取整到秒
 * @param remainingMs 剩余毫秒
 */
export function formatLockCountdown(remainingMs: number): string {
  const seconds = Math.max(0, Math.ceil(remainingMs / 1000));
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${pad(Math.floor(seconds / 60))}:${pad(seconds % 60)}`;
}

/** 提交改密码需要的外部能力，测试里可替换 */
export type PasswordSubmitDeps = {
  /** 调 PUT /me/password */
  changePassword: (body: ChangePasswordRequest) => Promise<LoginResponse>;
  /** 写入新令牌 */
  setToken: (token: string, expireAt: number, role?: string) => void;
};

/**
 * 提交改密码：成功后先用响应里的新令牌 setToken（同浏览器其他标签页共享 localStorage 一并续上；
 * 随后服务端断开 WS，重连时用新令牌申请 ticket）；业务错误翻译成就地提示
 * @param values 表单取值
 * @param deps 接口与令牌写入
 * @returns 成功，或失败与就地提示（null 表示只靠全局 toast）
 */
export async function submitPasswordChange(
  values: PasswordValues,
  deps: PasswordSubmitDeps,
): Promise<{ ok: true } | { ok: false; hint: PasswordErrorHint | null }> {
  try {
    const result = await deps.changePassword({
      old_password: values.old,
      new_password: values.next,
    });
    deps.setToken(result.token, result.expire_at, result.role);
    return { ok: true };
  } catch (error) {
    if (error instanceof ApiError) {
      return { ok: false, hint: mapPasswordError(error.code, error.message) };
    }
    return { ok: false, hint: null };
  }
}
