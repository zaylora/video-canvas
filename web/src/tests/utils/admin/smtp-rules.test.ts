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
  const ok = {
    initial_credits: "100",
    default_max_active_tasks: "3",
    queue_multiplier: "5",
    queue_max_wait_minutes: "60",
  };

  test("初始积分为非负整数，默认并发 1 到 64", () => {
    expect(hasSmtpErrors(validateRegisterSettings(ok))).toBe(false);
    expect(validateRegisterSettings({ ...ok, initial_credits: "-1" }).initial_credits).toBeTruthy();
    expect(
      validateRegisterSettings({ ...ok, initial_credits: "1.5" }).initial_credits,
    ).toBeTruthy();
    expect(
      validateRegisterSettings({ ...ok, default_max_active_tasks: "0" }).default_max_active_tasks,
    ).toBeTruthy();
    expect(
      validateRegisterSettings({ ...ok, default_max_active_tasks: "65" }).default_max_active_tasks,
    ).toBeTruthy();
  });

  test("排队倍数是 1 到 20 的整数", () => {
    for (const queue_multiplier of ["", "0", "21", "2.5", "abc", "-1"]) {
      expect(validateRegisterSettings({ ...ok, queue_multiplier }).queue_multiplier).toBe(
        "范围 1 到 20",
      );
    }
    for (const queue_multiplier of ["1", "5", "20", " 8 "]) {
      expect(
        validateRegisterSettings({ ...ok, queue_multiplier }).queue_multiplier,
      ).toBeUndefined();
    }
  });

  test("排队超时是 5 到 1440 分钟的整数", () => {
    for (const queue_max_wait_minutes of ["", "4", "1441", "60.5", "x", "0"]) {
      expect(
        validateRegisterSettings({ ...ok, queue_max_wait_minutes }).queue_max_wait_minutes,
      ).toBe("范围 5 到 1440");
    }
    for (const queue_max_wait_minutes of ["5", "60", "1440"]) {
      expect(
        validateRegisterSettings({ ...ok, queue_max_wait_minutes }).queue_max_wait_minutes,
      ).toBeUndefined();
    }
  });

  test("能否保存：有改动、无错误、可写", () => {
    const initial = { register_enabled: true, ...ok };
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

  test("只改排队倍数或排队超时也算有改动；越界不能保存", () => {
    const initial = { register_enabled: true, ...ok };
    expect(
      canSaveRegisterSettings({
        values: { ...initial, queue_multiplier: "8" },
        initial,
        canWrite: true,
      }),
    ).toBe(true);
    expect(
      canSaveRegisterSettings({
        values: { ...initial, queue_max_wait_minutes: "120" },
        initial,
        canWrite: true,
      }),
    ).toBe(true);
    expect(
      canSaveRegisterSettings({
        values: { ...initial, queue_multiplier: "21" },
        initial,
        canWrite: true,
      }),
    ).toBe(false);
    expect(
      canSaveRegisterSettings({
        values: { ...initial, queue_max_wait_minutes: "4" },
        initial,
        canWrite: true,
      }),
    ).toBe(false);
  });
});
