import { useCallback, useEffect, useState } from "react";

import { getMarketingProject } from "@/api/marketing";
import type { Project } from "@/api/marketing/type";
import { useMarketingStore } from "@/store/marketing";

/**
 * 一个营销项目的详情：读本地缓存，打开页面时拉一次，之后靠 WebSocket 推送并进缓存；
 * 页面开着期间登记为「正在看」，断线重连后会重新拉详情对账。
 * @param id 项目 ID
 * @returns project 详情；status 加载状态（拉失败显示「项目不存在或加载失败」）；reload 重新拉
 */
export function useMarketingProject(id: string | undefined) {
  const project: Project | undefined = useMarketingStore((state) =>
    id ? state.projects[id] : undefined,
  );
  const [status, setStatus] = useState<"loading" | "ready" | "error">(
    project ? "ready" : "loading",
  );

  const load = useCallback(async () => {
    if (!id) return;
    try {
      useMarketingStore.getState().setProject(await getMarketingProject(id));
      setStatus("ready");
    } catch {
      // 全局提示已弹；有缓存时继续用缓存，没有缓存显示失败态
      setStatus(useMarketingStore.getState().projects[id] ? "ready" : "error");
    }
  }, [id]);

  useEffect(() => {
    if (!id) return;
    setStatus(useMarketingStore.getState().projects[id] ? "ready" : "loading");
    void load();
    return useMarketingStore.getState().watch(id);
  }, [id, load]);

  return { project, status, reload: load };
}
