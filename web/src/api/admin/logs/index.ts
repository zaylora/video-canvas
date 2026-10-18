import service from "@/utils/requests/service";
import type { AuditPage, ListAuditParams } from "./type";

/** 契约见 docs/design/日志管理/日志管理.md §8.3；前缀 /api/v1 由 axios baseURL 提供，全部只读 */
const P = "/admin/logs";

/** 去掉空值，免得把 `actor=` 之类的空参数发给后端 */
const clean = (params: Record<string, unknown>) =>
  Object.fromEntries(
    Object.entries(params).filter(([, v]) => v !== undefined && v !== null && v !== ""),
  );

/**
 * 操作审计（admin 与 ai 两张审计表合并，按时间倒序），页码分页
 * @param params 操作人、模块、动作、时间范围与 page / page_size
 * @returns 当页审计与命中总数；list 缺失时补成空数组
 */
export const listAudits = async (params: ListAuditParams = {}): Promise<AuditPage> => {
  const page = await service.get<AuditPage | null>(`${P}/audits`, clean({ ...params }));
  return {
    list: page?.list ?? [],
    total: page?.total ?? 0,
    page: page?.page ?? params.page ?? 1,
    page_size: page?.page_size ?? params.page_size ?? 20,
  };
};
