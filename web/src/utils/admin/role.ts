import type { AdminRole } from "@/api/admin-ai/type";

/**
 * 把后端返回的角色收窄成已知值：只有明确是 super_admin 才给运维权限，
 * 其余（包括将来新增的未知角色）一律按 admin 处理——宁可少显示写操作，后端也会拦。
 */
export function normalizeRole(role: unknown): AdminRole {
  return role === "super_admin" ? "super_admin" : "admin";
}

/** 插件上传 / 启停 / 删除版本、渠道新建 / 修改 / Key / 连通性检查：只有 super_admin */
export const canManageInfra = (role: AdminRole | null | undefined) =>
  role === "super_admin";

/** 模型编辑、试跑、发布、回滚，以及从渠道导入模型：admin 与 super_admin 都可以 */
export const canManageModels = (role: AdminRole | null | undefined) =>
  role === "admin" || role === "super_admin";

export const ROLE_LABEL: Record<AdminRole, string> = {
  admin: "运营（admin）",
  super_admin: "运维（super_admin）",
};

type ErrorLike = { code?: unknown; status?: unknown };

/** 403 / 业务码 10004：没有权限 */
export function isForbiddenError(error: unknown): boolean {
  const e: ErrorLike = typeof error === "object" && error !== null ? error : {};
  return e.status === 403 || e.code === 10004;
}
