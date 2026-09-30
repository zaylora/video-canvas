import { useCallback, useEffect, useRef, useState } from "react";

import { listChannels, listPlugins } from "@/api/admin-ai";
import type { ChannelView, PluginView } from "@/api/admin-ai/type";

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

/** 插件与渠道清单：三个标签页共用一份（模型页的渠道下拉、渠道页的插件版本下拉都要用） */
export type AdminCatalog = {
  plugins: PluginView[];
  pluginsStatus: LoadStatus;
  channels: ChannelView[];
  channelsStatus: LoadStatus;
  reloadPlugins: () => Promise<void>;
  reloadChannels: () => Promise<void>;
};

export function useAdminCatalog(enabled: boolean): AdminCatalog {
  const aliveRef = useAliveRef();
  const [plugins, setPlugins] = useState<PluginView[]>([]);
  const [pluginsStatus, setPluginsStatus] = useState<LoadStatus>("loading");
  const [channels, setChannels] = useState<ChannelView[]>([]);
  const [channelsStatus, setChannelsStatus] = useState<LoadStatus>("loading");

  const reloadPlugins = useCallback(async () => {
    try {
      const list = await listPlugins();
      if (!aliveRef.current) return;
      setPlugins(list);
      setPluginsStatus("ready");
    } catch {
      if (aliveRef.current)
        setPluginsStatus((prev) => (prev === "ready" ? prev : "error"));
    }
  }, [aliveRef]);

  const reloadChannels = useCallback(async () => {
    try {
      const list = await listChannels();
      if (!aliveRef.current) return;
      setChannels(list);
      setChannelsStatus("ready");
    } catch {
      if (aliveRef.current)
        setChannelsStatus((prev) => (prev === "ready" ? prev : "error"));
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
