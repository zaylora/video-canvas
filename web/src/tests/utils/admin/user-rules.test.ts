import { describe, expect, test } from "bun:test";

import {
  batchPlan,
  concurrencyView,
  creditDelta,
  creditPreview,
  creditUndoRequest,
  denyReason,
  formatAbsolute,
  formatDurationMs,
  formatMonthDay,
  formatRelative,
  auditNote,
  isUserNotFound,
  taskStatusView,
  loginResultView,
  parseAmount,
  parseUserAgent,
  stackChip,
  validateLimit,
  validateNote,
} from "@/utils/admin/user-rules";

const SA = { id: 1, role: "super_admin" as const };
const ADMIN = { id: 2, role: "admin" as const };
const user = { id: 10, role: "user" as const, status: "active" as const };
const otherAdmin = { id: 3, role: "admin" as const, status: "active" as const };
const otherSA = { id: 4, role: "super_admin" as const, status: "active" as const };

describe("denyReason 权限禁用原因", () => {
  test("admin 不能操作管理员和超级管理员账号", () => {
    expect(denyReason(ADMIN, otherAdmin, "ban")).toBe("admin 不能操作管理员账号");
    expect(denyReason(ADMIN, otherSA, "credit")).toBe("admin 不能操作管理员账号");
    expect(denyReason(ADMIN, otherAdmin, "limit")).toBe("admin 不能操作管理员账号");
  });

  test("admin 可以操作普通用户的封禁、积分、并发", () => {
    expect(denyReason(ADMIN, user, "ban")).toBeNull();
    expect(denyReason(ADMIN, user, "credit")).toBeNull();
    expect(denyReason(ADMIN, user, "limit")).toBeNull();
  });

  test("super_admin 可以操作任何其他账号", () => {
    expect(denyReason(SA, otherAdmin, "ban")).toBeNull();
    expect(denyReason(SA, otherSA, "ban")).toBeNull();
    expect(denyReason(SA, otherAdmin, "role")).toBeNull();
    expect(denyReason(SA, otherAdmin, "reset")).toBeNull();
  });

  test("任何人不能封禁、改自己的角色", () => {
    const self = { id: 1, role: "super_admin" as const };
    expect(denyReason(SA, self, "ban")).toBe("不能对自己执行此操作");
    expect(denyReason(SA, self, "role")).toBe("不能对自己执行此操作");
    expect(denyReason(SA, self, "cancel")).toBe("不能对自己执行此操作");
  });

  test("改角色、重置密码只有 super_admin", () => {
    expect(denyReason(ADMIN, user, "role")).toBe("仅超级管理员可用");
    expect(denyReason(ADMIN, user, "reset")).toBe("仅超级管理员可用");
  });

  test("admin 不能给自己调积分，super_admin 可以", () => {
    const self = { id: 2, role: "admin" as const };
    expect(denyReason(ADMIN, self, "credit")).toBe("admin 不能给自己调整积分");
    expect(denyReason(SA, { id: 1, role: "super_admin" }, "credit")).toBeNull();
  });

  test("admin 改自己的并发上限不受「管理员账号」限制", () => {
    expect(denyReason(ADMIN, { id: 2, role: "admin" }, "limit")).toBeNull();
  });

  test("没有当前身份（还没加载）时一律禁用", () => {
    expect(denyReason(null, user, "ban")).toBe("正在确认管理权限");
  });
});

describe("parseAmount", () => {
  test("只接受非负整数的数字串", () => {
    expect(parseAmount("100")).toBe(100);
    expect(parseAmount("0")).toBe(0);
    expect(parseAmount("007")).toBe(7);
  });

  test("空、非数字、小数、负数都是 null", () => {
    expect(parseAmount("")).toBeNull();
    expect(parseAmount("  ")).toBeNull();
    expect(parseAmount("abc")).toBeNull();
    expect(parseAmount("1.5")).toBeNull();
    expect(parseAmount("-3")).toBeNull();
  });

  test("过长的数字拒绝，避免超出安全整数", () => {
    expect(parseAmount("1234567890")).toBeNull();
  });
});

describe("creditPreview 积分预览", () => {
  test("增加：可用 + 数值", () => {
    expect(creditPreview({ mode: "add", amount: 100, available: 24 })).toEqual({
      after: 124,
      error: null,
      valid: true,
    });
  });

  test("增加 0 不合法但不报错", () => {
    expect(creditPreview({ mode: "add", amount: 0, available: 24 })).toEqual({
      after: 24,
      error: null,
      valid: false,
    });
  });

  test("扣减超过可用：报「最多可扣」并禁止提交", () => {
    const r = creditPreview({ mode: "sub", amount: 30, available: 24 });
    expect(r.error).toBe("最多可扣 24");
    expect(r.valid).toBe(false);
  });

  test("扣减恰好等于可用合法", () => {
    expect(creditPreview({ mode: "sub", amount: 24, available: 24 })).toEqual({
      after: 0,
      error: null,
      valid: true,
    });
  });

  test("设为：直接把可用积分设成指定值，允许 0", () => {
    expect(creditPreview({ mode: "set", amount: 0, available: 24 })).toEqual({
      after: 0,
      error: null,
      valid: true,
    });
    expect(creditPreview({ mode: "set", amount: 500, available: 24 }).after).toBe(500);
  });

  test("没填数值：没有预览结果", () => {
    expect(creditPreview({ mode: "add", amount: null, available: 24 })).toEqual({
      after: null,
      error: null,
      valid: false,
    });
  });
});

describe("creditDelta", () => {
  test("add / sub / set 换算成可用积分的变化量", () => {
    expect(creditDelta("add", 100, 24)).toBe(100);
    expect(creditDelta("sub", 10, 24)).toBe(-10);
    expect(creditDelta("set", 5, 24)).toBe(-19);
    expect(creditDelta("set", 50, 24)).toBe(26);
  });
});

describe("creditUndoRequest 撤销 = 反向调整", () => {
  test("增加的撤销是扣减，备注加「撤销：」前缀", () => {
    expect(creditUndoRequest(100, "活动补发")).toEqual({
      mode: "sub",
      amount: 100,
      note: "撤销：活动补发",
    });
  });

  test("扣减的撤销是增加", () => {
    expect(creditUndoRequest(-30, "误扣")).toEqual({ mode: "add", amount: 30, note: "撤销：误扣" });
  });

  test("变化量为 0 没什么可撤销", () => {
    expect(creditUndoRequest(0, "x")).toBeNull();
  });

  test("备注总长不超过后端的 100 字", () => {
    const r = creditUndoRequest(1, "字".repeat(100));
    expect(r?.note.length).toBe(100);
    expect(r?.note.startsWith("撤销：")).toBe(true);
  });
});

describe("validateNote", () => {
  test("必填，1 到 100 字", () => {
    expect(validateNote("")).not.toBeNull();
    expect(validateNote("   ")).not.toBeNull();
    expect(validateNote("ok")).toBeNull();
    expect(validateNote("字".repeat(101))).not.toBeNull();
  });
});

describe("stackChip 快捷档累加", () => {
  test("空值直接填入，已有值累加", () => {
    expect(stackChip("", 100)).toBe("100");
    expect(stackChip("100", 100)).toBe("200");
    expect(stackChip("abc", 50)).toBe("50");
  });
});

describe("validateLimit 并发上限", () => {
  test("使用默认时不需要数值", () => {
    expect(validateLimit("", true)).toEqual({ value: null, valid: true });
  });

  test("自定义要在 1 到 64 之间的整数", () => {
    expect(validateLimit("8", false)).toEqual({ value: 8, valid: true });
    expect(validateLimit("0", false).valid).toBe(false);
    expect(validateLimit("65", false).valid).toBe(false);
    expect(validateLimit("", false).valid).toBe(false);
    expect(validateLimit("2.5", false).valid).toBe(false);
  });
});

describe("concurrencyView", () => {
  test("用满、自定义的判断", () => {
    expect(
      concurrencyView({ active_tasks: 2, effective_max_active_tasks: 2, max_active_tasks: 2 }),
    ).toEqual({ active: 2, limit: 2, full: true, custom: true, ratio: 1 });
    expect(
      concurrencyView({ active_tasks: 1, effective_max_active_tasks: 4, max_active_tasks: null }),
    ).toEqual({ active: 1, limit: 4, full: false, custom: false, ratio: 0.25 });
  });

  test("超出上限时进度不超过 1", () => {
    expect(
      concurrencyView({ active_tasks: 5, effective_max_active_tasks: 4, max_active_tasks: null })
        .ratio,
    ).toBe(1);
  });
});

describe("batchPlan 批量跳过规则", () => {
  const users = [
    { id: 10, role: "user" as const, status: "active" as const, username: "a" },
    { id: 11, role: "user" as const, status: "disabled" as const, username: "b" },
    { id: 3, role: "admin" as const, status: "active" as const, username: "boss" },
    { id: 2, role: "admin" as const, status: "active" as const, username: "me" },
  ];

  test("admin 批量封禁：跳过管理员和自己，已停用的不算跳过", () => {
    const plan = batchPlan(ADMIN, users, "ban");
    expect(plan.run.map((u) => u.id)).toEqual([10]);
    expect(plan.noop.map((u) => u.id)).toEqual([11]);
    expect(plan.skipped.map((s) => [s.user.id, s.reason])).toEqual([
      [3, "admin 不能操作管理员账号"],
      [2, "不能封禁自己"],
    ]);
  });

  test("批量启用：只处理已停用的", () => {
    const plan = batchPlan(ADMIN, users, "unban");
    expect(plan.run.map((u) => u.id)).toEqual([11]);
    expect(plan.noop.map((u) => u.id)).toEqual([10]);
  });

  test("super_admin 批量封禁跳过自己，其他都执行", () => {
    const withSelf = [
      ...users,
      { id: 1, role: "super_admin" as const, status: "active" as const, username: "root" },
    ];
    const plan = batchPlan(SA, withSelf, "ban");
    expect(plan.run.map((u) => u.id)).toEqual([10, 3, 2]);
    expect(plan.skipped.map((s) => s.user.id)).toEqual([1]);
  });

  test("批量发积分：admin 跳过管理员（含自己，因为不能给自己调积分）", () => {
    const plan = batchPlan(ADMIN, users, "credit");
    expect(plan.run.map((u) => u.id)).toEqual([10, 11]);
    expect(plan.skipped.map((s) => s.user.id)).toEqual([3, 2]);
  });

  test("跳过说明：去重原因并写出数量", () => {
    const plan = batchPlan(ADMIN, users, "ban");
    expect(plan.skipText).toBe("已跳过 2 个：admin 不能操作管理员账号；不能封禁自己");
    expect(batchPlan(SA, [users[0]], "ban").skipText).toBe("");
  });
});

describe("formatRelative 相对时间", () => {
  const now = new Date(2026, 9, 5, 12, 0, 0);
  const at = (...a: [number, number, number, number?, number?]) =>
    new Date(a[0], a[1], a[2], a[3] ?? 0, a[4] ?? 0).toISOString();

  test("从未登录", () => {
    expect(formatRelative(null, now)).toBe("从未登录");
  });

  test("今天、昨天带时分", () => {
    expect(formatRelative(at(2026, 9, 5, 10, 21), now)).toBe("今天 10:21");
    expect(formatRelative(at(2026, 9, 4, 22, 40), now)).toBe("昨天 22:40");
  });

  test("按自然日算：昨天 23:59 离现在不到一天也是昨天", () => {
    expect(formatRelative(at(2026, 9, 4, 23, 59), new Date(2026, 9, 5, 0, 5))).toBe("昨天 23:59");
  });

  test("几天、几周、几月、几年前", () => {
    expect(formatRelative(at(2026, 9, 2, 9, 0), now)).toBe("3 天前");
    expect(formatRelative(at(2026, 8, 28, 9, 0), now)).toBe("1 周前");
    expect(formatRelative(at(2026, 7, 5, 9, 0), now)).toBe("2 月前");
    expect(formatRelative(at(2024, 9, 5, 9, 0), now)).toBe("2 年前");
  });

  test("时间在未来（时钟偏差）按今天处理；解析失败原样返回", () => {
    expect(formatRelative(at(2026, 9, 5, 18, 0), now)).toBe("今天 18:00");
    expect(formatRelative("not-a-date", now)).toBe("not-a-date");
  });
});

describe("formatAbsolute / formatMonthDay", () => {
  test("绝对时间 YYYY-MM-DD HH:mm", () => {
    expect(formatAbsolute(new Date(2026, 9, 5, 9, 3).toISOString())).toBe("2026-10-05 09:03");
    expect(formatAbsolute(null)).toBe("—");
  });

  test("同年省略年份", () => {
    const now = new Date(2026, 9, 5);
    expect(formatMonthDay(new Date(2026, 8, 20).toISOString(), now)).toBe("9月20日");
    expect(formatMonthDay(new Date(2025, 8, 20).toISOString(), now)).toBe("2025年9月20日");
  });
});

describe("parseUserAgent", () => {
  test("浏览器与系统", () => {
    expect(
      parseUserAgent(
        "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
      ),
    ).toBe("Chrome · macOS");
    expect(
      parseUserAgent(
        "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
      ),
    ).toBe("Safari · iOS");
    expect(
      parseUserAgent(
        "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0",
      ),
    ).toBe("Edge · Windows");
    expect(
      parseUserAgent("Mozilla/5.0 (X11; Linux x86_64; rv:121.0) Gecko/20100101 Firefox/121.0"),
    ).toBe("Firefox · Linux");
  });

  test("空或认不出", () => {
    expect(parseUserAgent("")).toBe("未知设备");
    expect(parseUserAgent("curl/8.0")).toBe("未知设备");
  });
});

describe("loginResultView", () => {
  test("注册、成功、密码错误、账号停用", () => {
    expect(loginResultView("register", "ok")).toEqual({ label: "注册", tone: "info" });
    expect(loginResultView("login", "ok")).toEqual({ label: "成功", tone: "success" });
    expect(loginResultView("login", "badpw")).toEqual({ label: "密码错误", tone: "danger" });
    expect(loginResultView("login", "blocked")).toEqual({ label: "账号已停用", tone: "danger" });
  });
});

describe("taskStatusView", () => {
  test("终态与进行中", () => {
    expect(taskStatusView("succeeded")).toEqual({ label: "成功", tone: "success", running: false });
    expect(taskStatusView("failed").tone).toBe("danger");
    expect(taskStatusView("canceled").label).toBe("已取消");
    for (const s of ["pending", "queued", "running", "finalizing"]) {
      expect(taskStatusView(s).running).toBe(true);
    }
  });
});

describe("auditNote", () => {
  test("从 JSON 字符串或对象里取备注，取不到返回空串", () => {
    expect(auditNote('{"note":"活动补发","amount":50}')).toBe("活动补发");
    expect(auditNote({ note: "误扣" })).toBe("误扣");
    expect(auditNote("not json")).toBe("");
    expect(auditNote(null)).toBe("");
    expect(auditNote({ amount: 1 })).toBe("");
  });
});

describe("isUserNotFound", () => {
  test("404 或业务码 20001", () => {
    expect(isUserNotFound({ status: 404 })).toBe(true);
    expect(isUserNotFound({ code: 20001, status: 400 })).toBe(true);
    expect(isUserNotFound({ code: 10004, status: 403 })).toBe(false);
    expect(isUserNotFound(null)).toBe(false);
  });
});

describe("formatDurationMs", () => {
  test("毫秒转成秒或分秒，未完成显示破折号", () => {
    expect(formatDurationMs(null)).toBe("—");
    expect(formatDurationMs(820)).toBe("1s");
    expect(formatDurationMs(34_000)).toBe("34s");
    expect(formatDurationMs(125_000)).toBe("2m5s");
    expect(formatDurationMs(0)).toBe("0s");
  });
});
