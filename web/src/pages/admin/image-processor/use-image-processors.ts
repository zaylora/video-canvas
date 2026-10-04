import { useCallback, useEffect, useState } from "react";

import { listImageProcessors, listProcessorPresets } from "@/api/admin-image-processor";
import type { ProcessorPreset, ProcessorView } from "@/api/admin-image-processor/type.d";
import { listStorages } from "@/api/admin-storage";
import type { StorageView } from "@/api/admin-storage/type.d";

import { useAliveRef, type LoadStatus } from "../use-admin";

/**
 * 处理服务、厂商预设与存储列表：进页面时一起加载；之后 reload 静默刷新，失败时保留上一次的数据。
 * 存储列表用来判定能绑定哪些存储，请求错误的全局提示由拦截器负责，这里只记状态。
 * @returns 三份数据、加载状态，以及刷新整表与替换 / 追加单条的方法
 */
export function useImageProcessors() {
  const aliveRef = useAliveRef();
  const [processors, setProcessors] = useState<ProcessorView[]>([]);
  const [presets, setPresets] = useState<ProcessorPreset[]>([]);
  const [storages, setStorages] = useState<StorageView[]>([]);
  const [status, setStatus] = useState<LoadStatus>("loading");

  const reload = useCallback(async () => {
    try {
      const [list, presetList, storageList] = await Promise.all([
        listImageProcessors(),
        listProcessorPresets(),
        listStorages(),
      ]);
      if (!aliveRef.current) return;
      setProcessors(list);
      setPresets(presetList);
      setStorages(storageList);
      setStatus("ready");
    } catch {
      if (aliveRef.current) setStatus((prev) => (prev === "ready" ? prev : "error"));
    }
  }, [aliveRef]);

  useEffect(() => {
    void reload();
  }, [reload]);

  /** 用最新视图替换列表里的同一条；列表里还没有（刚在抽屉里新建）就追加 */
  const upsertOne = useCallback((view: ProcessorView) => {
    setProcessors((prev) =>
      prev.some((item) => item.id === view.id)
        ? prev.map((item) => (item.id === view.id ? view : item))
        : [...prev, view],
    );
  }, []);

  return { processors, presets, storages, status, reload, upsertOne };
}
