import { useCallback, useEffect, useRef, useState } from "react";
import { useOutletContext } from "react-router";

import { listChannels, listModels, listPlugins } from "@/api/admin/ai";
import type { ChannelView, ConfigListItem, PluginView } from "@/api/admin/ai/type.d";

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

/**
 * 取插件与渠道清单。
 * @param enabled 为 true 时才开始请求（角色确认后再请求，避免普通用户白白吃一次 403）
 */
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
      if (aliveRef.current) setPluginsStatus((prev) => (prev === "ready" ? prev : "error"));
    }
  }, [aliveRef]);

  const reloadChannels = useCallback(async () => {
    try {
      const list = await listChannels();
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
 * 模型清单（总览、渠道、插件页用来算“谁在用”“影响几个模型”；模型页有自己的工作区，不用这个）。
 * @returns 清单、加载状态与刷新函数
 */
export function useModelList() {
  const aliveRef = useAliveRef();
  const [models, setModels] = useState<ConfigListItem[]>([]);
  const [status, setStatus] = useState<LoadStatus>("loading");
  const reload = useCallback(async () => {
    try {
      const list = await listModels();
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
  return { models, status, reload };
}

/** 布局通过 Outlet 传给三个子页的上下文 */
export type AdminOutletContext = {
  /** 插件与渠道清单（三页共用一份，避免各自请求） */
  catalog: AdminCatalog;
};

/** 子页取布局上下文 */
export const useAdminOutlet = () => useOutletContext<AdminOutletContext>();
