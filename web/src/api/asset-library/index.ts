import { mapAsset } from "@/api/asset";
import service from "@/utils/requests/service";
import type {
  BackendCursorPage,
  BackendFolderDto,
  BackendGeneratedItemDto,
  BackendLibraryItemDto,
  CursorPageDto,
  FolderDto,
  GeneratedItemDto,
  GeneratedQueryDto,
  LibraryItemDto,
  LibraryQueryDto,
} from "./type";

/** 每页条数：生成历史与资产库共用，滚动到底再加载下一页 */
export const ASSET_PAGE_SIZE = 40;

const mapFolder = (raw: BackendFolderDto): FolderDto => ({
  id: raw.id,
  name: raw.name,
  itemCount: raw.item_count ?? 0,
  createdAt: raw.created_at,
});

const mapLibraryItem = (raw: BackendLibraryItemDto): LibraryItemDto => ({
  id: raw.id,
  asset: mapAsset(raw.asset),
  folderId: raw.folder_id ?? null,
  createdAt: raw.created_at,
});

const mapGeneratedItem = (raw: BackendGeneratedItemDto): GeneratedItemDto => ({
  asset: mapAsset(raw.asset),
  prompt: raw.prompt ?? "",
  modelLabel: raw.model_label ?? "",
  canvas: raw.canvas
    ? { id: raw.canvas.id, title: raw.canvas.title ?? "", deleted: raw.canvas.deleted === true }
    : null,
  createdAt: raw.created_at,
  libraryItemId: raw.library_item_id ?? null,
});

/** 把游标分页响应转成前端结构：null 列表兜底为空，null 游标当作到底 */
const mapPage = <B, T>(page: BackendCursorPage<B>, map: (raw: B) => T): CursorPageDto<T> => ({
  items: (page.items ?? []).map(map),
  nextCursor: page.next_cursor ?? "",
});

/**
 * 获取全部文件夹（每用户不超过 100 个，不分页）
 * @returns 文件夹列表，带各自的素材数
 */
export const listFolders = async (): Promise<FolderDto[]> => {
  const response = await service.get<{ items: BackendFolderDto[] | null }>("/asset-folders");
  return (response.items ?? []).map(mapFolder);
};

/**
 * 新建文件夹；重名、数量上限由后端用业务错误返回
 * @param name 名称，调用方先做 1–30 字校验
 * @returns 新文件夹
 */
export const createFolder = async (name: string): Promise<FolderDto> =>
  mapFolder(await service.post<BackendFolderDto>("/asset-folders", { name }));

/**
 * 重命名文件夹
 * @param id 文件夹 ID
 * @param name 新名称
 * @returns 改名后的文件夹
 */
export const renameFolder = async (id: string, name: string): Promise<FolderDto> =>
  mapFolder(await service.patch<BackendFolderDto>(`/asset-folders/${id}`, { name }));

/**
 * 删除文件夹，里面的素材一起移出资产库；素材文件本身与画布里的引用不受影响
 * @param id 文件夹 ID
 * @returns 被移出资产库的素材数
 */
export const deleteFolder = async (id: string): Promise<number> => {
  const response = await service.delete<{ removed: number }>(`/asset-folders/${id}`);
  return response.removed ?? 0;
};

/**
 * 分页获取资产库里的素材
 * @param query 文件夹、类型、关键字与游标
 * @returns 一页素材
 */
export const listLibraryItems = async (
  query: LibraryQueryDto,
): Promise<CursorPageDto<LibraryItemDto>> =>
  mapPage(
    await service.get<BackendCursorPage<BackendLibraryItemDto>>("/asset-library/items", {
      folder_id: query.folderId,
      kind: query.kind,
      q: query.q,
      cursor: query.cursor,
      limit: query.limit ?? ASSET_PAGE_SIZE,
    }),
    mapLibraryItem,
  );

/**
 * 把素材加入资产库；已经在库里时后端幂等返回现有归属
 * @param assetId 素材 ID
 * @param folderId 目标文件夹，null 是根目录
 * @returns 资产库条目
 */
export const addToLibrary = async (
  assetId: string,
  folderId: string | null,
): Promise<LibraryItemDto> =>
  mapLibraryItem(
    await service.post<BackendLibraryItemDto>("/asset-library/items", {
      asset_id: Number(assetId),
      folder_id: folderId,
    }),
  );

/**
 * 把资产库里的素材移到另一个文件夹
 * @param id 资产库条目 ID
 * @param folderId 目标文件夹，null 是根目录
 */
export const moveLibraryItem = async (id: string, folderId: string | null): Promise<void> => {
  await service.patch(`/asset-library/items/${id}`, { folder_id: folderId });
};

/**
 * 把素材移出资产库；只删归属，不动素材文件
 * @param id 资产库条目 ID
 */
export const removeLibraryItem = async (id: string): Promise<void> => {
  await service.delete(`/asset-library/items/${id}`);
};

/**
 * 分页获取生成历史（首页对话与所有画布合并）
 * @param query 类型、来源、时间、排序、关键字与游标
 * @returns 一页生成历史
 */
export const listGeneratedAssets = async (
  query: GeneratedQueryDto,
): Promise<CursorPageDto<GeneratedItemDto>> =>
  mapPage(
    await service.get<BackendCursorPage<BackendGeneratedItemDto>>("/assets/generated", {
      kind: query.kind,
      source: query.source,
      canvas_id: query.canvasId,
      created_from: query.createdFrom,
      order: query.order,
      q: query.q,
      cursor: query.cursor,
      limit: query.limit ?? ASSET_PAGE_SIZE,
    }),
    mapGeneratedItem,
  );
