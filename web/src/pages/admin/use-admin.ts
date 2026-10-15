import { useCallback, useEffect, useRef, useState } from "react";

import { listChannelOptions, listPluginOptions } from "@/api/admin/ai";
import type { ChannelOption, PluginView } from "@/api/admin/ai/type.d";

export type LoadStatus = "loading" | "ready" | "error";

/** 组件还挂着吗：异步回来后先问一句再 setState */
export function useAliveRef() {
  const aliveRef = useRef(true);
  useEffect(() => {
    aliveRef.current = true;
    return () => {
      aliveRef.current = false;
    };
  }, []);
  return aliveRef;
}

/** 插件与渠道清单：渠道与模型主从页、插件页共用（渠道下拉、模型弹窗的协议列表都要用） */
export type AdminCatalog = {
  plugins: PluginView[];
  pluginsStatus: LoadStatus;
  /** 全部渠道（options：带健康状态，渠道顺序），主从页左栏、弹窗下拉和概览用 */
  channels: ChannelOption[];
  channelsStatus: LoadStatus;
  reloadPlugins: () => Promise<void>;
  reloadChannels: () => Promise<void>;
};

/**
 * 取插件与渠道清单。
 * @param enabled 为 true 时才开始请求（角色确认后再请求，避免普通用户白白吃一次 403）
 */
export function useAdminCatalog(enabled: boolean): AdminCatalog {
  const aliveRef = useAliveRef();
  const [plugins, setPlugins] = useState<PluginView[]>([]);
  const [pluginsStatus, setPluginsStatus] = useState<LoadStatus>("loading");
  const [channels, setChannels] = useState<ChannelOption[]>([]);
  const [channelsStatus, setChannelsStatus] = useState<LoadStatus>("loading");

  const reloadPlugins = useCallback(async () => {
    try {
      const list = await listPluginOptions();
      if (!aliveRef.current) return;
      setPlugins(list);
      setPluginsStatus("ready");
    } catch {
      if (aliveRef.current) setPluginsStatus((prev) => (prev === "ready" ? prev : "error"));
    }
  }, [aliveRef]);

  const reloadChannels = useCallback(async () => {
    try {
      const list = await listChannelOptions();
      if (!aliveRef.current) return;
      setChannels(list);
      setChannelsStatus("ready");
    } catch {
      if (aliveRef.current) setChannelsStatus((prev) => (prev === "ready" ? prev : "error"));
    }
  }, [aliveRef]);

  useEffect(() => {
    if (!enabled) return;
    void reloadPlugins();
    void reloadChannels();
  }, [enabled, reloadChannels, reloadPlugins]);

  return {
    plugins,
    pluginsStatus,
    channels,
    channelsStatus,
    reloadPlugins,
    reloadChannels,
  };
}

/**
 * 页面自己取全量插件与渠道（options）：只有用到的页面才请求，进后台不再预加载。
 * 渠道与模型主从页、插件页要用。
 */
export function useAdminCatalogOptions(): { catalog: AdminCatalog } {
  return { catalog: useAdminCatalog(true) };
}
