/** 后台列表的 URL 参数：搜索词、页码和若干筛选，刷新页面不丢，也能把链接发给同事 */
export type ListParams = {
  q: string;
  /** 页码，从 1 开始 */
  page: number;
  /** 筛选项；没选（空或 all）的不出现 */
  filters: Record<string, string>;
};

/** 筛选值为空或 all 视为没选 */
const unset = (value: string | null | undefined): boolean => !value || value === "all";

/**
 * 从 URL 读出列表参数。
 * @param params 当前 URL 的查询参数
 * @param filterKeys 本列表认的筛选参数名（白名单，其它参数忽略）
 */
export function parseListParams(params: URLSearchParams, filterKeys: readonly string[]): ListParams {
  const filters: Record<string, string> = {};
  for (const key of filterKeys) {
    const value = params.get(key);
    if (!unset(value)) filters[key] = value as string;
  }
  return {
    q: params.get("q") ?? "",
    page: Math.max(1, Math.floor(Number(params.get("page"))) || 1),
    filters,
  };
}

/**
 * 转成列表接口的查询参数：搜索词去首尾空格，空值不带，分页固定带上。
 * @param pageSize 每页条数
 */
export function toListQuery(
  { q, page, filters }: ListParams,
  pageSize: number,
): Record<string, string | number> {
  const trimmed = q.trim();
  return { ...(trimmed && { q: trimmed }), ...filters, page, page_size: pageSize };
}

/**
 * 改 URL 里的一个列表参数，返回新的 URLSearchParams（不修改传入对象）。
 * 改搜索词或筛选时页码清零（回到第 1 页）；设置页码时第 1 页不写进 URL。
 */
export function withListParam(
  params: URLSearchParams,
  key: string,
  value: string | null,
): URLSearchParams {
  const next = new URLSearchParams(params);
  if (key === "page") {
    if (unset(value) || value === "1") next.delete("page");
    else next.set("page", value as string);
    return next;
  }
  if (unset(value)) next.delete(key);
  else next.set(key, value as string);
  next.delete("page");
  return next;
}
