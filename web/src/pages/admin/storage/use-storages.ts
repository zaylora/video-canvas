import { useCallback, useEffect, useState } from "react";

import { listStorages, listStoragePresets } from "@/api/admin/storage";
import type { StoragePage, StoragePreset, StorageView } from "@/api/admin/storage/type.d";

import { useAdminList } from "../use-admin-list";
import { useAliveRef } from "../use-admin";

/**
 * 存储列表（服务端分页 + 搜索）与服务商预设。
 * 列表按 URL 里的 q / page 请求当前页；预设进页面时加载一次。
 * 默认存储的完整视图随列表一起返回，不受搜索和分页影响，所以搜索时默认存储卡片不会消失。
 * @returns 列表状态与操作，以及预设
 */
export function useStorages() {
  const aliveRef = useAliveRef();
  const list = useAdminList<StorageView, StoragePage>([], (query) => listStorages(query));
  const [presets, setPresets] = useState<StoragePreset[]>([]);

  const loadPresets = useCallback(async () => {
    try {
      const next = await listStoragePresets();
      if (aliveRef.current) setPresets(next);
    } catch {
      // 请求失败的全局 toast 已弹；预设为空时表单里服务商名称回退为 id
    }
  }, [aliveRef]);
  useEffect(() => {
    void loadPresets();
  }, [loadPresets]);

  return { ...list, presets, defaultStorage: list.result?.default ?? null };
}
