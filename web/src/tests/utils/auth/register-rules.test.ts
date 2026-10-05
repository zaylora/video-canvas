import { describe, expect, test } from "bun:test";

import {
  becameFirstAdmin,
  CODE_COOLDOWN_SECONDS,
  codeButtonState,
  hasRegisterErrors,
  mapAuthError,
  nextCountdown,
  validateConfirm,
  validateEmail,
  validatePassword,
  validateRegisterForm,
  validateUsername,
} from "@/utils/auth/register-rules";

describe("用户名", () => {
  test("空、过短、过长都拒绝，首尾空格不计入", () => {
    expect(validateUsername("")).toBeTruthy();
    expect(validateUsername("  ab  ")).toBeTruthy();
    expect(validateUsername("a".repeat(65))).toBeTruthy();
  });
  test("3 到 64 个字符通过", () => {
    expect(validateUsername("abc")).toBeNull();
    expect(validateUsername(" alice ")).toBeNull();
    expect(validateUsername("a".repeat(64))).toBeNull();
  });
});

describe("邮箱", () => {
  test("格式不对拒绝", () => {
    for (const value of ["", "abc", "a@b", "a@@b.com", "a b@c.com", "@b.com"]) {
      expect(validateEmail(value)).toBeTruthy();
    }
  });
  test("常规邮箱通过，忽略首尾空格", () => {
    expect(validateEmail("a@b.com")).toBeNull();
    expect(validateEmail(" first.last+tag@mail.example.co ")).toBeNull();
  });
});

describe("密码与确认密码", () => {
  test("长度 6 到 128", () => {
    expect(validatePassword("12345")).toBeTruthy();
    expect(validatePassword("123456")).toBeNull();
    expect(validatePassword("a".repeat(129))).toBeTruthy();
  });
  test("两次必须一致，确认为空也提示", () => {
    expect(validateConfirm("secret1", "secret1")).toBeNull();
    expect(validateConfirm("secret1", "secret2")).toBeTruthy();
    expect(validateConfirm("secret1", "")).toBeTruthy();
  });
});

describe("整表校验", () => {
  const ok = {
    username: "alice",
    email: "a@b.com",
    code: "123456",
    password: "secret1",
    confirm: "secret1",
  };
  test("需要验证码时，验证码必须是 6 位数字", () => {
    expect(hasRegisterErrors(validateRegisterForm(ok, true))).toBe(false);
    expect(validateRegisterForm({ ...ok, code: "12345" }, true).code).toBeTruthy();
    expect(validateRegisterForm({ ...ok, code: "12ab56" }, true).code).toBeTruthy();
  });
  test("不需要验证码时忽略验证码", () => {
    expect(validateRegisterForm({ ...ok, code: "" }, false).code).toBeUndefined();
  });
  test("逐字段给出错误", () => {
    const errors = validateRegisterForm({ ...ok, username: "a", confirm: "x" }, true);
    expect(errors.username).toBeTruthy();
    expect(errors.confirm).toBeTruthy();
    expect(errors.email).toBeUndefined();
    expect(hasRegisterErrors(errors)).toBe(true);
  });
});

describe("验证码倒计时与按钮", () => {
  test("冷却 60 秒，每秒减一，到 0 停住", () => {
    expect(CODE_COOLDOWN_SECONDS).toBe(60);
    expect(nextCountdown(60)).toBe(59);
    expect(nextCountdown(1)).toBe(0);
    expect(nextCountdown(0)).toBe(0);
  });
  test("发送中禁用并显示发送中", () => {
    expect(codeButtonState({ remaining: 0, sending: true, emailValid: true })).toEqual({
      disabled: true,
      label: "发送中...",
    });
  });
  test("倒计时中禁用并显示剩余秒数", () => {
    expect(codeButtonState({ remaining: 42, sending: false, emailValid: true })).toEqual({
      disabled: true,
      label: "42 秒后重发",
    });
  });
  test("邮箱不合法时禁用，合法时可发送", () => {
    expect(codeButtonState({ remaining: 0, sending: false, emailValid: false }).disabled).toBe(
      true,
    );
    expect(codeButtonState({ remaining: 0, sending: false, emailValid: true })).toEqual({
      disabled: false,
      label: "发送验证码",
    });
  });
});

describe("业务错误码就地提示", () => {
  test("注册相关错误落到对应字段", () => {
    expect(mapAuthError(20002)).toEqual({ field: "username", message: "用户名已被占用" });
    expect(mapAuthError(53002)).toEqual({ field: "email", message: "该邮箱已注册" });
    expect(mapAuthError(53003)).toEqual({ field: "code", message: "验证码错误或已过期" });
  });
  test("注册关闭与账号停用是表单级提示", () => {
    expect(mapAuthError(53001)).toEqual({ field: "form", message: "暂未开放注册" });
    expect(mapAuthError(53004)).toEqual({
      field: "form",
      message: "账号已被停用，请联系管理员",
    });
  });
  test("其他错误不就地提示（全局 toast 已弹）", () => {
    expect(mapAuthError(10005)).toBeNull();
    expect(mapAuthError("NETWORK_ERROR")).toBeNull();
    expect(mapAuthError(undefined)).toBeNull();
  });
});

describe("becameFirstAdmin（是否弹「你是首个用户」提示）", () => {
  test("只有注册后角色是 super_admin 才算首个用户", () => {
    expect(becameFirstAdmin("super_admin")).toBe(true);
  });
  test("普通用户注册不提示，不管需不需要验证码", () => {
    expect(becameFirstAdmin("user")).toBe(false);
    expect(becameFirstAdmin("")).toBe(false);
    expect(becameFirstAdmin(undefined)).toBe(false);
  });
});
