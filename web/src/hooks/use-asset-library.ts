import { ASSET_PAGE_SIZE, listLibraryItems } from "@/api/asset-library";
import type { AssetKind } from "@/api/asset-library/type";

import { usePagedList } from "./use-paged-list";

/**
 * 资产页「个人资产库」的素材：按文件夹与类型游标分页。
 * 搜索时跨文件夹平铺（folder_id=all），所以关键字非空就忽略 folder。
 * @param folder 当前文件夹 ID，null 是根目录
 * @param q 文件名关键字（已防抖），空串表示不搜索
 * @param kind 素材类型，不传则各类型混在一起
 * @returns 列表实例与分页状态
 */
export function useAssetLibrary(folder: string | null, q: string, kind?: AssetKind) {
  const keyword = q.trim();
  const folderId = keyword ? "all" : (folder ?? "root");
  return usePagedList(JSON.stringify([folderId, kind, keyword]), (cursor) =>
    listLibraryItems({
      folderId,
      kind,
      q: keyword || undefined,
      cursor,
      limit: ASSET_PAGE_SIZE,
    }),
  );
}
