import { useCallback, useEffect, useState } from "react";

import {
  listImageProcessors,
  listProcessorOptions,
  listProcessorPresets,
} from "@/api/admin/image-processor";
import type {
  ProcessorOption,
  ProcessorPage,
  ProcessorPreset,
  ProcessorView,
} from "@/api/admin/image-processor/type.d";
import { listStorageOptions } from "@/api/admin/storage";
import type { StorageOption } from "@/api/admin/storage/type.d";

import { useAdminList } from "../use-admin-list";
import { useAliveRef, type LoadStatus } from "../use-admin";

/** 状态筛选取值 */
export const PROCESSOR_FILTERS = ["status"] as const;

/**
 * 图片服务页的数据：
 * - 处理服务列表：服务端分页 + 搜索 + 状态筛选，按 URL 里的 q / status / page 请求当前页；
 * - 厂商预设、全部存储和全部处理服务的精简视图：判定“存储能绑哪家、有没有被占用”要用全量，
 *   进页面加载一次，列表有变化（reload）时一起刷新。
 * 请求错误的全局提示由拦截器负责，这里只记状态。
 */
export function useImageProcessors() {
  const aliveRef = useAliveRef();
  const list = useAdminList<ProcessorView, ProcessorPage>(PROCESSOR_FILTERS, (query) =>
    listImageProcessors(query),
  );
  const [presets, setPresets] = useState<ProcessorPreset[]>([]);
  const [storages, setStorages] = useState<StorageOption[]>([]);
  const [bindings, setBindings] = useState<ProcessorOption[]>([]);
  const [auxStatus, setAuxStatus] = useState<LoadStatus>("loading");

  const loadAux = useCallback(async () => {
    try {
      const [presetList, storageList, bindingList] = await Promise.all([
        listProcessorPresets(),
        listStorageOptions(),
        listProcessorOptions(),
      ]);
      if (!aliveRef.current) return;
      setPresets(presetList);
      setStorages(storageList);
      setBindings(bindingList);
      setAuxStatus("ready");
    } catch {
      if (aliveRef.current) setAuxStatus((prev) => (prev === "ready" ? prev : "error"));
    }
  }, [aliveRef]);
  useEffect(() => {
    void loadAux();
  }, [loadAux]);

  const { reload: reloadList } = list;
  /** 处理服务有变化后：重新拉当前页，并刷新绑定判定用的全量精简视图 */
  const reload = useCallback(() => {
    reloadList();
    void loadAux();
  }, [reloadList, loadAux]);

  const status: LoadStatus =
    list.state === "error" || auxStatus === "error"
      ? "error"
      : list.state === "ready" && auxStatus === "ready"
        ? "ready"
        : "loading";

  return { ...list, reload, status, presets, storages, bindings };
}
