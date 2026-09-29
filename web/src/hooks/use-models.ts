import { useCallback, useEffect, useMemo } from "react";

import type { ModelInfo } from "@/api/model/type";
import { useModelsStore, type ModelsEntry } from "@/store/models";
import type { ModelOption } from "@/types";

const EMPTY_ENTRY: ModelsEntry = { status: "idle", models: [], loadedAt: 0 };

/** 服务端模型 -> 输入框下拉认得的形状：key 就是节点里存的 model */
export const toModelOption = (info: ModelInfo): ModelOption => ({
  id: info.key,
  label: info.label,
  credits: info.credits,
  hint: info.hint,
});

/**
 * 读某种类的服务端模型清单（带缓存）。
 * status 让调用方分清「还在加载」「加载失败」和「加载成功但没有这个模型」——
 * 只有最后一种才该判定模型已下线。
 */
export function useRemoteModels(kind: string, enabled = true) {
  const entry = useModelsStore((state) => state.byKind[kind]) ?? EMPTY_ENTRY;
  const load = useModelsStore((state) => state.load);

  useEffect(() => {
    if (enabled) void load(kind);
  }, [enabled, kind, load]);

  const reload = useCallback(() => load(kind, true), [kind, load]);
  const options = useMemo(() => entry.models.map(toModelOption), [entry.models]);

  return {
    status: entry.status,
    models: entry.models,
    options,
    reload,
  };
}
