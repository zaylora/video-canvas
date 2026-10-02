import { useEffect, useState } from "react";

import { listChannelLoads } from "@/api/admin-ai";
import type { ChannelLoad } from "@/api/admin-ai/type";

import { useAliveRef } from "../use-admin";

/** 渠道负载的刷新间隔：够实时，又不给后端添负担 */
const POLL_MS = 5000;

/**
 * 各渠道当前的“生成中 / 排队”任务数，按渠道 key 索引（没有未完成任务的渠道不在里面）。
 * 页面可见时每 5 秒刷新一次，切到别的标签页就暂停；失败静默保留上一次的数据，下一轮再试。
 */
export function useChannelLoads(enabled: boolean): Record<string, ChannelLoad> {
  const aliveRef = useAliveRef();
  const [loads, setLoads] = useState<Record<string, ChannelLoad>>({});

  useEffect(() => {
    if (!enabled) return;
    const load = async () => {
      if (document.visibilityState === "hidden") return;
      try {
        const list = await listChannelLoads();
        if (aliveRef.current)
          setLoads(Object.fromEntries(list.map((item) => [item.channel, item])));
      } catch {
        // 全局拦截器已提示过；这里保留旧数据，等下一轮
      }
    };
    void load();
    const timer = window.setInterval(() => void load(), POLL_MS);
    return () => window.clearInterval(timer);
  }, [aliveRef, enabled]);

  return loads;
}
