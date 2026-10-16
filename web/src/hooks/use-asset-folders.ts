import { useCallback, useEffect, useState } from "react";

import { createFolder, deleteFolder, listFolders, renameFolder } from "@/api/asset-library";
import type { FolderDto } from "@/api/asset-library/type";
import {
  addFolder,
  adjustFolderCount,
  removeFolder,
  renameFolder as renameFolderIn,
} from "@/utils/assets/folder-list";

/**
 * 当前用户的文件夹列表（每用户不超过 100 个，一次拉完）。
 * 资产库页和「加入资产库」选择器共用：enabled 变为 true 时（选择器每次打开）会重新拉一遍，
 * 先显示手里的旧数据，所以素材数可能稍后才刷新。新建、重命名、删除成功后就地更新，不整体重拉。
 * 失败提示由请求层统一弹，这里只记 error 让界面显示「重试」。
 * @param enabled 是否需要数据；false 时不请求
 */
export function useAssetFolders(enabled = true) {
  const [folders, setFolders] = useState<FolderDto[] | null>(null);
  const [error, setError] = useState(false);
  /** 递增一次就重拉一次 */
  const [reloadKey, setReloadKey] = useState(0);

  useEffect(() => {
    if (!enabled) return;
    let active = true;
    listFolders()
      .then((list) => {
        if (!active) return;
        setFolders(list);
        setError(false);
      })
      .catch(() => {
        if (active) setError(true);
      });
    return () => {
      active = false;
    };
  }, [enabled, reloadKey]);

  const reload = useCallback(() => setReloadKey((key) => key + 1), []);

  /** 新建；失败抛出（请求层已弹提示），调用方的对话框据此保持打开 */
  const create = useCallback(async (name: string) => {
    const folder = await createFolder(name);
    setFolders((prev) => addFolder(prev ?? [], folder));
    return folder;
  }, []);

  /** 重命名；失败抛出 */
  const rename = useCallback(async (id: string, name: string) => {
    const folder = await renameFolder(id, name);
    setFolders((prev) => (prev ? renameFolderIn(prev, id, folder.name) : prev));
  }, []);

  /** 删除；失败抛出。返回被移出的素材数 */
  const remove = useCallback(async (id: string) => {
    const removed = await deleteFolder(id);
    setFolders((prev) => (prev ? removeFolder(prev, id) : prev));
    return removed;
  }, []);

  /** 本地调整素材数：上传、移动、移出后先改界面，稍后 reload 校准 */
  const adjust = useCallback((id: string | null, delta: number) => {
    setFolders((prev) => (prev ? adjustFolderCount(prev, id, delta) : prev));
  }, []);

  return {
    folders,
    /** 还没拿到第一份数据 */
    loading: enabled && folders === null && !error,
    error: error && folders === null,
    reload,
    create,
    rename,
    remove,
    adjust,
  };
}

export type AssetFoldersState = ReturnType<typeof useAssetFolders>;
