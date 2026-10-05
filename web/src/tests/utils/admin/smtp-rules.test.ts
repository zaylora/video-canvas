import { describe, expect, test } from "bun:test";

import {
  canSaveRegisterSettings,
  canSaveSmtp,
  canSendTest,
  hasSmtpErrors,
  validateRegisterSettings,
  validateSmtpForm,
  type SmtpFormValues,
} from "@/utils/admin/smtp-rules";

const base: SmtpFormValues = {
  host: "smtp.example.com",
  port: "465",
  encryption: "tls",
  username: "mailer",
  from_address: "noreply@example.com",
  from_name: "Video Canvas",
  enabled: true,
};

describe("SMTP 表单校验", () => {
  test("完整表单无错误", () => {
    expect(hasSmtpErrors(validateSmtpForm(base))).toBe(false);
  });
  test("服务器必填，且不能带协议或路径", () => {
    expect(validateSmtpForm({ ...base, host: "" }).host).toBeTruthy();
    expect(validateSmtpForm({ ...base, host: "smtp://a.com" }).host).toBeTruthy();
    expect(validateSmtpForm({ ...base, host: "a.com/x" }).host).toBeTruthy();
    expect(validateSmtpForm({ ...base, host: " smtp.a.com " }).host).toBeUndefined();
  });
  test("端口必须是 1 到 65535 的整数", () => {
    for (const port of ["", "0", "65536", "25.5", "abc", "-1"]) {
      expect(validateSmtpForm({ ...base, port }).port).toBeTruthy();
    }
    expect(validateSmtpForm({ ...base, port: "587" }).port).toBeUndefined();
  });
  test("发件人地址必须是邮箱，发件人名称可空", () => {
    expect(validateSmtpForm({ ...base, from_address: "x" }).from_address).toBeTruthy();
    expect(validateSmtpForm({ ...base, from_name: "" }).from_name).toBeUndefined();
  });
  test("用户名可空（免认证 SMTP）", () => {
    expect(validateSmtpForm({ ...base, username: "" }).username).toBeUndefined();
  });
});

describe("能否保存与测试", () => {
  test("没改动不能保存；改动且无错误可保存；有错误不能保存", () => {
    expect(canSaveSmtp({ values: base, initial: base, canWrite: true })).toBe(false);
    const changed = { ...base, port: "587" };
    expect(canSaveSmtp({ values: changed, initial: base, canWrite: true })).toBe(true);
    expect(canSaveSmtp({ values: { ...changed, host: "" }, initial: base, canWrite: true })).toBe(
      false,
    );
  });
  test("只读角色或保存中不能保存", () => {
    const changed = { ...base, port: "587" };
    expect(canSaveSmtp({ values: changed, initial: base, canWrite: false })).toBe(false);
    expect(canSaveSmtp({ values: changed, initial: base, canWrite: true, saving: true })).toBe(
      false,
    );
  });
  test("发测试邮件：要有保存过的配置、无未保存改动、收件邮箱合法", () => {
    const args = { dirty: false, configured: true, to: "a@b.com", canWrite: true };
    expect(canSendTest(args)).toBe(true);
    expect(canSendTest({ ...args, dirty: true })).toBe(false);
    expect(canSendTest({ ...args, configured: false })).toBe(false);
    expect(canSendTest({ ...args, to: "bad" })).toBe(false);
    expect(canSendTest({ ...args, canWrite: false })).toBe(false);
    expect(canSendTest({ ...args, busy: true })).toBe(false);
  });
});

describe("注册设置校验", () => {
  test("初始积分为非负整数，默认并发 1 到 64", () => {
    expect(
      hasSmtpErrors(
        validateRegisterSettings({ initial_credits: "100", default_max_active_tasks: "3" }),
      ),
    ).toBe(false);
    expect(
      validateRegisterSettings({ initial_credits: "-1", default_max_active_tasks: "3" })
        .initial_credits,
    ).toBeTruthy();
    expect(
      validateRegisterSettings({ initial_credits: "1.5", default_max_active_tasks: "3" })
        .initial_credits,
    ).toBeTruthy();
    expect(
      validateRegisterSettings({ initial_credits: "0", default_max_active_tasks: "0" })
        .default_max_active_tasks,
    ).toBeTruthy();
    expect(
      validateRegisterSettings({ initial_credits: "0", default_max_active_tasks: "65" })
        .default_max_active_tasks,
    ).toBeTruthy();
  });
  test("能否保存：有改动、无错误、可写", () => {
    const initial = {
      register_enabled: true,
      initial_credits: "100",
      default_max_active_tasks: "3",
    };
    expect(canSaveRegisterSettings({ values: initial, initial, canWrite: true })).toBe(false);
    expect(
      canSaveRegisterSettings({
        values: { ...initial, register_enabled: false },
        initial,
        canWrite: true,
      }),
    ).toBe(true);
    expect(
      canSaveRegisterSettings({
        values: { ...initial, initial_credits: "x" },
        initial,
        canWrite: true,
      }),
    ).toBe(false);
  });
});
