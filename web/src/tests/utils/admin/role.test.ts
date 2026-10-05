import { describe, expect, test } from "bun:test";

import {
  canEnterAdmin,
  canManageInfra,
  canManageModels,
  isForbiddenError,
  normalizeRole,
} from "@/utils/admin/role";

describe("normalizeRole", () => {
  test("只有明确是 super_admin 才是 super_admin", () => {
    expect(normalizeRole("super_admin")).toBe("super_admin");
    expect(normalizeRole("admin")).toBe("admin");
  });

  test("未知、空值、大小写不符的角色一律按 admin 处理", () => {
    expect(normalizeRole("root")).toBe("admin");
    expect(normalizeRole("SUPER_ADMIN")).toBe("admin");
    expect(normalizeRole(undefined)).toBe("admin");
    expect(normalizeRole(null)).toBe("admin");
    expect(normalizeRole(1)).toBe("admin");
  });
});

describe("权限判断", () => {
  test("运维权限只有 super_admin", () => {
    expect(canManageInfra("super_admin")).toBe(true);
    expect(canManageInfra("admin")).toBe(false);
    expect(canManageInfra(null)).toBe(false);
    expect(canManageInfra(undefined)).toBe(false);
  });

  test("模型管理权限 admin 与 super_admin 都有，未确认角色没有", () => {
    expect(canManageModels("admin")).toBe(true);
    expect(canManageModels("super_admin")).toBe(true);
    expect(canManageModels(null)).toBe(false);
  });
});

describe("isForbiddenError", () => {
  test("HTTP 403 或业务码 10004 都算无权限", () => {
    expect(isForbiddenError({ status: 403 })).toBe(true);
    expect(isForbiddenError({ code: 10004 })).toBe(true);
  });

  test("其他错误、非对象都不算", () => {
    expect(isForbiddenError({ status: 500, code: 50000 })).toBe(false);
    expect(isForbiddenError(null)).toBe(false);
    expect(isForbiddenError("403")).toBe(false);
  });
});

describe("canEnterAdmin（是否显示「管理后台」入口）", () => {
  test("只有 admin 与 super_admin 可见", () => {
    expect(canEnterAdmin("admin")).toBe(true);
    expect(canEnterAdmin("super_admin")).toBe(true);
  });
  test("普通用户、未知角色、没有角色都不可见", () => {
    expect(canEnterAdmin("user")).toBe(false);
    expect(canEnterAdmin("owner")).toBe(false);
    expect(canEnterAdmin(null)).toBe(false);
    expect(canEnterAdmin(undefined)).toBe(false);
  });
});
