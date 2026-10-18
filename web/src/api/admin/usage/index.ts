import service from "@/utils/requests/service";
import type {
  ListUsageParams,
  UsageDetail,
  UsageItem,
  UsagePage,
  UsageQuery,
  UsageSummary,
} from "./type";

/** 契约见 docs/design/日志管理/日志管理.md §8.3；前缀 /api/v1 由 axios baseURL 提供，全部只读 */
const P = "/admin/usage";

/** 去掉空值，免得把 `q=` 之类的空参数发给后端 */
const clean = (params: Record<string, unknown>) =>
  Object.fromEntries(
    Object.entries(params).filter(([, v]) => v !== undefined && v !== null && v !== ""),
  );

/**
 * 任务流水（全站，按 id 倒序），页码分页
 * @param params 筛选条件与 page / page_size
 * @returns 当页任务与命中总数；list 缺失时补成空数组
 */
export const listUsage = async (params: ListUsageParams = {}): Promise<UsagePage<UsageItem>> => {
  const page = await service.get<UsagePage<UsageItem> | null>(P, clean({ ...params }));
  return {
    list: page?.list ?? [],
    total: page?.total ?? 0,
    page: page?.page ?? params.page ?? 1,
    page_size: page?.page_size ?? params.page_size ?? 20,
  };
};

/**
 * 当前筛选下的任务概况，筛选参数与列表一致
 * @param query 筛选条件
 */
export const getUsageSummary = (query: UsageQuery = {}) =>
  service.get<UsageSummary>(`${P}/summary`, clean({ ...query }));

/**
 * 任务详情；任务不存在时后端返回 40004（HTTP 404）
 * @param ref 32 位十六进制任务编号
 * @returns 详情，数组字段缺失时补成空数组
 */
export const getUsageDetail = async (ref: string): Promise<UsageDetail> => {
  const detail = await service.get<UsageDetail>(`${P}/${encodeURIComponent(ref)}`, undefined);
  return {
    ...detail,
    input: detail.input ?? {},
    outputs: detail.outputs ?? [],
    events: detail.events ?? [],
    ledger: detail.ledger ?? [],
    trace: detail.trace ?? null,
  };
};
