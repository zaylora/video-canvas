import { ASSET_PAGE_SIZE, listGeneratedAssets } from "@/api/asset-library";
import type { AssetKind } from "@/api/asset-library/type";
import { buildGeneratedQuery, type GeneratedFilter } from "@/utils/assets/generated-filter";

import { usePagedList } from "./use-paged-list";

/**
 * 资产页「生成历史」的数据：按类型与筛选条件游标分页，条件变了就从首页重新加载。
 * 时间范围在每次请求时才换算成 created_from，所以重试拿到的是新鲜的「今天」。
 * @param kind 素材类型
 * @param filter 筛选状态（关键字请传防抖后的值）
 * @returns 列表实例与分页状态
 */
export function useGeneratedAssets(kind: AssetKind, filter: GeneratedFilter) {
  return usePagedList(JSON.stringify([kind, filter]), (cursor) =>
    listGeneratedAssets({
      ...buildGeneratedQuery(kind, filter),
      cursor,
      limit: ASSET_PAGE_SIZE,
    }),
  );
}
