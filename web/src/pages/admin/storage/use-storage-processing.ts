import { useEffect, useState } from "react";

import { listProcessorOptions, listProcessorPresets } from "@/api/admin/image-processor";
import type { ProcessorOption, ProcessorPreset } from "@/api/admin/image-processor/type.d";

import { useAliveRef } from "../use-admin";

/**
 * 存储配置页「图片处理」列要的数据：全部处理服务的精简视图 + 厂商预设（判定哪些存储能启用）。
 * 进页面加载一次；失败时保持空数组，列里按“未启用”显示，错误提示由请求拦截器弹。
 * @returns processors 处理服务精简视图；presets 厂商预设
 */
export function useStorageProcessing() {
  const aliveRef = useAliveRef();
  const [processors, setProcessors] = useState<ProcessorOption[]>([]);
  const [presets, setPresets] = useState<ProcessorPreset[]>([]);

  useEffect(() => {
    void (async () => {
      try {
        const [options, presetList] = await Promise.all([
          listProcessorOptions(),
          listProcessorPresets(),
        ]);
        if (!aliveRef.current) return;
        setProcessors(options);
        setPresets(presetList);
      } catch {
        // 请求失败的全局 toast 已弹
      }
    })();
  }, [aliveRef]);

  return { processors, presets: presets };
}
