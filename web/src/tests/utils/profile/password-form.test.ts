import { describe, expect, test } from "bun:test";

import { ApiError } from "@/utils/requests/request";
import {
  canSubmitPassword,
  formatLockCountdown,
  mapPasswordError,
  PASSWORD_LOCK_MS,
  passwordHints,
  submitPasswordChange,
} from "@/utils/profile/password-form";

const values = (part: Partial<{ old: string; next: string; confirm: string }> = {}) => ({
  old: "oldpass123",
  next: "newpass456",
  confirm: "newpass456",
  ...part,
});

describe("前端即时提示", () => {
  test("新密码：空时给规则说明，不足 8 位显示已输入字节数", () => {
    expect(passwordHints(values({ next: "", confirm: "" }), "lin").next.tone).toBe("muted");
    expect(passwordHints(values({ next: "abc" }), "lin").next).toEqual({
      tone: "muted",
      text: "已输入 3 / 至少 8 位",
    });
    expect(passwordHints(values({ next: "密码" }), "lin").next.text).toBe("已输入 6 / 至少 8 位");
  });
  test("新密码：超过 72 字节、与当前密码相同、与用户名相同都报错", () => {
    expect(passwordHints(values({ next: "a".repeat(73) }), "lin").next).toEqual({
      tone: "error",
      text: "密码最多 72 字节（约 24 个汉字或 72 个英文字符）",
    });
    expect(passwordHints(values({ next: "oldpass123" }), "lin").next).toEqual({
      tone: "error",
      text: "新密码不能与当前密码相同",
    });
    expect(passwordHints(values({ next: "lin_studio" }), "lin_studio").next).toEqual({
      tone: "error",
      text: "密码不能与用户名相同",
    });
    expect(passwordHints(values(), "lin").next.tone).toBe("ok");
  });
  test("确认密码：空时不提示，不一致报错，一致给通过提示", () => {
    expect(passwordHints(values({ confirm: "" }), "lin").confirm).toBeNull();
    expect(passwordHints(values({ confirm: "x" }), "lin").confirm).toEqual({
      tone: "error",
      text: "两次输入的密码不一致",
    });
    expect(passwordHints(values(), "lin").confirm?.tone).toBe("ok");
  });
  test("可提交：三项都合法、未锁定、不在提交中", () => {
    const state = { locked: false, saving: false };
    expect(canSubmitPassword(values(), "lin", state)).toBe(true);
    expect(canSubmitPassword(values({ old: "" }), "lin", state)).toBe(false);
    expect(canSubmitPassword(values({ next: "short", confirm: "short" }), "lin", state)).toBe(
      false,
    );
    expect(canSubmitPassword(values({ confirm: "x" }), "lin", state)).toBe(false);
    expect(
      canSubmitPassword(values({ next: "oldpass123", confirm: "oldpass123" }), "lin", state),
    ).toBe(false);
    expect(canSubmitPassword(values(), "lin", { locked: true, saving: false })).toBe(false);
    expect(canSubmitPassword(values(), "lin", { locked: false, saving: true })).toBe(false);
  });
});

describe("后端错误落点", () => {
  test("55001 落在当前密码下方，带后端给的剩余次数", () => {
    expect(mapPasswordError(55001, "当前密码错误，还可尝试 3 次")).toEqual({
      field: "old",
      message: "当前密码错误，还可尝试 3 次",
    });
    expect(mapPasswordError(55001, "")).toEqual({ field: "old", message: "当前密码错误" });
  });
  test("55002 / 55003 / 10001 落在新密码下方", () => {
    expect(mapPasswordError(55002, "x")).toEqual({
      field: "next",
      message: "新密码不能与当前密码相同",
    });
    expect(mapPasswordError(55003, "")).toEqual({
      field: "next",
      message: "这个密码过于常见，请换一个",
    });
    expect(mapPasswordError(55003, "密码不能与用户名相同")?.message).toBe("密码不能与用户名相同");
    expect(mapPasswordError(10001, "密码最多 72 字节")?.field).toBe("next");
  });
  test("55004 是表单顶部提示并锁定 15 分钟", () => {
    expect(mapPasswordError(55004, "尝试过于频繁，请稍后再试")).toEqual({
      field: "form",
      message: "尝试过于频繁，请 15 分钟后再试",
      lock: true,
    });
    expect(PASSWORD_LOCK_MS).toBe(15 * 60 * 1000);
  });
  test("其他错误只靠全局 toast", () => {
    expect(mapPasswordError("NETWORK_ERROR", "网络异常")).toBeNull();
  });
  test("倒计时 mm:ss", () => {
    expect(formatLockCountdown(PASSWORD_LOCK_MS)).toBe("15:00");
    expect(formatLockCountdown(61_000)).toBe("01:01");
    expect(formatLockCountdown(400)).toBe("00:01");
    expect(formatLockCountdown(-5)).toBe("00:00");
  });
});

describe("提交流程", () => {
  test("成功后用响应里的新令牌调用 setToken", async () => {
    const calls: unknown[][] = [];
    const sent: unknown[] = [];
    const result = await submitPasswordChange(values(), {
      changePassword: async (body) => {
        sent.push(body);
        return { token: "new.jwt", expire_at: 1_900_000_000, role: "user" };
      },
      setToken: (...args) => calls.push(args),
    });
    expect(result).toEqual({ ok: true });
    expect(sent).toEqual([{ old_password: "oldpass123", new_password: "newpass456" }]);
    expect(calls).toEqual([["new.jwt", 1_900_000_000, "user"]]);
  });
  test("业务错误不写令牌，返回字段级提示", async () => {
    let written = false;
    const result = await submitPasswordChange(values(), {
      changePassword: async () => {
        throw new ApiError("当前密码错误，还可尝试 4 次", 55001, 400);
      },
      setToken: () => {
        written = true;
      },
    });
    expect(written).toBe(false);
    expect(result).toEqual({
      ok: false,
      hint: { field: "old", message: "当前密码错误，还可尝试 4 次" },
    });
  });
  test("网络错误没有就地提示", async () => {
    const result = await submitPasswordChange(values(), {
      changePassword: async () => {
        throw new ApiError("网络异常", "NETWORK_ERROR", 0);
      },
      setToken: () => undefined,
    });
    expect(result).toEqual({ ok: false, hint: null });
  });
});
