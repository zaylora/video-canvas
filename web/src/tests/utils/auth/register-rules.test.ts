import { describe, expect, test } from "bun:test";

import {
  becameFirstAdmin,
  CODE_COOLDOWN_SECONDS,
  codeButtonState,
  hasRegisterErrors,
  mapAuthError,
  nextCountdown,
  passwordBytes,
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
  test("至少 8 位，7 位拒绝并给统一文案", () => {
    expect(validatePassword("", "alice")).toBe("请输入密码");
    expect(validatePassword("1234567", "alice")).toBe("密码至少 8 位");
    expect(validatePassword("12345678", "alice")).toBeNull();
  });
  test("上限按 UTF-8 字节算：72 字节通过，73 字节拒绝", () => {
    expect(validatePassword("a".repeat(72), "alice")).toBeNull();
    expect(validatePassword("a".repeat(73), "alice")).toBe(
      "密码最多 72 字节（约 24 个汉字或 72 个英文字符）",
    );
    /** 一个汉字 3 字节：24 个正好 72 字节，25 个超出 */
    expect(validatePassword("密".repeat(24), "alice")).toBeNull();
    expect(validatePassword("密".repeat(25), "alice")).toBeTruthy();
  });
  test("下限也按字节算：3 个汉字是 9 字节，算够长", () => {
    expect(validatePassword("密码好", "alice")).toBeNull();
  });
  test("不能与用户名相同（用户名首尾空格不计）", () => {
    expect(validatePassword("alice2026", "alice2026")).toBe("密码不能与用户名相同");
    expect(validatePassword("alice2026", " alice2026 ")).toBe("密码不能与用户名相同");
    expect(validatePassword("alice2026", "")).toBeNull();
  });
  test("两次必须一致，确认为空也提示", () => {
    expect(validateConfirm("secret12", "secret12")).toBeNull();
    expect(validateConfirm("secret12", "secret13")).toBe("两次输入的密码不一致");
    expect(validateConfirm("secret12", "")).toBeTruthy();
  });
  test("密码字节数", () => {
    expect(passwordBytes("abc")).toBe(3);
    expect(passwordBytes("密码")).toBe(6);
  });
});

describe("整表校验", () => {
  const ok = {
    username: "alice",
    email: "a@b.com",
    code: "123456",
    password: "secret12",
    confirm: "secret12",
  };
  test("需要验证码时，验证码必须是 6 位数字", () => {
    expect(hasRegisterErrors(validateRegisterForm(ok, true))).toBe(false);
    expect(validateRegisterForm({ ...ok, code: "12345" }, true).code).toBeTruthy();
    expect(validateRegisterForm({ ...ok, code: "12ab56" }, true).code).toBeTruthy();
  });
  test("不需要验证码时忽略验证码", () => {
    expect(validateRegisterForm({ ...ok, code: "" }, false).code).toBeUndefined();
  });
  test("密码等于用户名时落在密码字段", () => {
    const errors = validateRegisterForm(
      { ...ok, username: "alice2026", password: "alice2026", confirm: "alice2026" },
      true,
    );
    expect(errors.password).toBe("密码不能与用户名相同");
  });
  test("7 位密码不再通过", () => {
    expect(
      validateRegisterForm({ ...ok, password: "secret1", confirm: "secret1" }, true).password,
    ).toBe("密码至少 8 位");
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
  test("55003 密码过于常见落在密码字段，优先用后端文案（等于用户名时文案不同）", () => {
    expect(mapAuthError(55003)).toEqual({
      field: "password",
      message: "这个密码过于常见，请换一个",
    });
    expect(mapAuthError(55003, "密码不能与用户名相同")).toEqual({
      field: "password",
      message: "密码不能与用户名相同",
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
