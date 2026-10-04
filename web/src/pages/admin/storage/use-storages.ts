import { useCallback, useEffect, useState } from "react";

import { listStorages, listStoragePresets } from "@/api/admin-storage";
import type { StoragePreset, StorageView } from "@/api/admin-storage/type.d";

import { useAliveRef, type LoadStatus } from "../use-admin";

/**
 * 存储列表与服务商预设：进页面时一起加载；之后 reload 静默刷新，失败时保留上一次的数据。
 * 请求错误的全局提示由拦截器负责，这里只记状态。
 * @returns 列表、预设、加载状态，以及刷新整表与替换单条的方法
 */
export function useStorages() {
  const aliveRef = useAliveRef();
  const [storages, setStorages] = useState<StorageView[]>([]);
  const [presets, setPresets] = useState<StoragePreset[]>([]);
  const [status, setStatus] = useState<LoadStatus>("loading");

  const reload = useCallback(async () => {
    try {
      const [list, presetList] = await Promise.all([listStorages(), listStoragePresets()]);
      if (!aliveRef.current) return;
      setStorages(list);
      setPresets(presetList);
      setStatus("ready");
    } catch {
      if (aliveRef.current) setStatus((prev) => (prev === "ready" ? prev : "error"));
    }
  }, [aliveRef]);

  useEffect(() => {
    void reload();
  }, [reload]);

  /** 用最新的视图替换列表里的同一条（保存、替换凭证、重新拉取之后），不整表刷新 */
  const replaceOne = useCallback((view: StorageView) => {
    setStorages((prev) => prev.map((item) => (item.id === view.id ? view : item)));
  }, []);

  return { storages, presets, status, reload, replaceOne };
}
