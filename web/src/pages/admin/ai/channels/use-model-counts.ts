import { useCallback, useEffect, useMemo, useState } from "react";

import { listModelOptions } from "@/api/admin/ai";
import type { ConfigListItem } from "@/api/admin/ai/type.d";

import { useAliveRef, type LoadStatus } from "../../use-admin";

/**
 * 全部模型的轻量清单（options）：左栏每个渠道的“N 个模型”，以及深链里只给了模型 key 时反查它属于哪个渠道。
 * 失败时保留上一次的数据，首次失败 status 为 error（全局 toast 已弹）。
 * @returns 模型清单、按渠道 key 统计的模型数、加载状态与刷新函数
 */
export function useModelCounts() {
  const aliveRef = useAliveRef();
  const [models, setModels] = useState<ConfigListItem[]>([]);
  const [status, setStatus] = useState<LoadStatus>("loading");

  const reload = useCallback(async () => {
    try {
      const list = await listModelOptions();
      if (!aliveRef.current) return;
      setModels(list);
      setStatus("ready");
    } catch {
      if (aliveRef.current) setStatus((prev) => (prev === "ready" ? prev : "error"));
    }
  }, [aliveRef]);

  useEffect(() => {
    void reload();
  }, [reload]);

  const counts = useMemo(() => {
    const result: Record<string, number> = {};
    for (const model of models) {
      if (model.channel) result[model.channel] = (result[model.channel] ?? 0) + 1;
    }
    return result;
  }, [models]);

  return { models, counts, status, reload };
}
