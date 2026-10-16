import type { GeneratedItemDto, LibraryItemDto } from "@/api/asset-library/type";

import type { PagedList } from "./paged-list";

/** 资产库改动用到的接口：由调用方注入，测试时换成假的 */
export type LibraryApi = {
  /** 加入资产库，已在库里时幂等返回现有归属 */
  addToLibrary: (assetId: string, folderId: string | null) => Promise<LibraryItemDto>;
  /** 移到另一个文件夹 */
  moveLibraryItem: (id: string, folderId: string | null) => Promise<void>;
  /** 移出资产库 */
  removeLibraryItem: (id: string) => Promise<void>;
};

/** 资产库列表当前在看什么：决定移动后素材是否还留在这个列表里 */
export type LibraryView = {
  /** 所在文件夹；null 是根目录 */
  folder: string | null;
  /** 是否在搜索：搜索是跨文件夹平铺的，素材换了文件夹仍在结果里 */
  search: boolean;
};

/** 后端对「已不存在」的统一回应是 404；用鸭子类型认，避免工具函数依赖请求层 */
const isNotFound = (error: unknown) => (error as { status?: number } | null)?.status === 404;

/**
 * 把生成历史里的素材加入资产库：成功后给该素材写上归属 ID，卡片就显示「已在资产库」。
 * 失败的提示由请求层弹，列表不动。
 * @param store 生成历史列表
 * @param item 要加入的历史条目
 * @param folderId 目标文件夹，null 是根目录
 * @param api 接口
 * @returns 资产库条目；失败时 null
 */
export async function addGeneratedToLibrary(
  store: PagedList<GeneratedItemDto>,
  item: GeneratedItemDto,
  folderId: string | null,
  api: LibraryApi,
): Promise<LibraryItemDto | null> {
  try {
    const result = await api.addToLibrary(item.asset.id, folderId);
    store.update((items) =>
      items.map((entry) =>
        entry.asset.id === item.asset.id ? { ...entry, libraryItemId: result.id } : entry,
      ),
    );
    return result;
  } catch (error) {
    if (isNotFound(error)) store.load();
    return null;
  }
}

/**
 * 乐观地从列表里拿掉一项，返回回滚函数：把它放回原位，期间新插入的内容不受影响。
 * @param store 列表
 * @param id 要拿掉的条目 ID
 */
function takeOut(store: PagedList<LibraryItemDto>, id: string) {
  let removed: LibraryItemDto | undefined;
  let index = -1;
  store.update((items) => {
    index = items.findIndex((item) => item.id === id);
    if (index < 0) return items;
    removed = items[index];
    return items.filter((item) => item.id !== id);
  });
  return () => {
    const item = removed;
    if (!item) return;
    store.update((items) => {
      if (items.some((entry) => entry.id === item.id)) return items;
      const at = Math.min(index, items.length);
      return [...items.slice(0, at), item, ...items.slice(at)];
    });
  };
}

/**
 * 移出资产库：先从列表里消失，请求失败再回滚。404 说明别处已经删了，回滚后重新拉取当前列表。
 * @param store 资产库列表
 * @param id 资产库条目 ID
 * @param api 接口
 * @returns 是否成功
 */
export async function removeFromLibrary(
  store: PagedList<LibraryItemDto>,
  id: string,
  api: LibraryApi,
): Promise<boolean> {
  const rollback = takeOut(store, id);
  try {
    await api.removeLibraryItem(id);
    return true;
  } catch (error) {
    if (isNotFound(error)) store.load();
    else rollback();
    return false;
  }
}

/**
 * 移动到文件夹：在文件夹或根目录视图里，移走的素材立刻从当前列表消失；
 * 搜索视图里素材留在原处，只更新归属。失败回滚，404 重新拉取。
 * @param store 资产库列表
 * @param id 资产库条目 ID
 * @param folderId 目标文件夹，null 是根目录
 * @param view 列表当前在看什么
 * @param api 接口
 * @returns 是否成功
 */
export async function moveInLibrary(
  store: PagedList<LibraryItemDto>,
  id: string,
  folderId: string | null,
  view: LibraryView,
  api: LibraryApi,
): Promise<boolean> {
  if (!view.search && view.folder === folderId) return true;
  const rollback = view.search ? () => {} : takeOut(store, id);
  if (view.search) {
    store.update((items) => items.map((item) => (item.id === id ? { ...item, folderId } : item)));
  }
  try {
    await api.moveLibraryItem(id, folderId);
    return true;
  } catch (error) {
    if (isNotFound(error)) store.load();
    else if (view.search) store.load();
    else rollback();
    return false;
  }
}
