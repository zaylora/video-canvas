import type { ChannelView, PluginView } from "@/api/admin/ai/type";

import { channelHealth } from "./health";
import { availableUpgrade, channelSupportsKind, latestVersion, metaSupportsKind } from "./plugin";

/** 渠道表的筛选条件；空字符串表示“全部” */
export type ChannelFilter = {
  query: string;
  /** 能力：text / image / video / audio */
  kind: string;
  /** 插件 key */
  plugin: string;
  /** ok / warn / bad / off（同状态列的健康状态）或 upgrade（插件有更新版本） */
  status: string;
};

/** 插件表的筛选条件；空字符串表示“全部” */
export type PluginFilter = {
  query: string;
  kind: string;
  /** builtin / uploaded */
  source: string;
  /** on / off / outdated（有渠道固定在旧版本上） */
  status: string;
};

/** 搜索：任一字段包含关键字（忽略大小写和首尾空格）；关键字为空恒为真 */
const matches = (query: string, fields: readonly string[]) => {
  const text = query.trim().toLowerCase();
  return !text || fields.some((field) => field.toLowerCase().includes(text));
};

/**
 * 渠道表的前端筛选。能力按渠道所用插件版本的声明筛；读不到插件版本时（清单没加载好）
 * 不当成“不支持”，免得一瞬间整张表被筛空。
 */
export function filterChannels(
  channels: readonly ChannelView[],
  plugins: readonly PluginView[],
  filter: ChannelFilter,
): ChannelView[] {
  return channels.filter((channel) => {
    if (!matches(filter.query, [channel.name, channel.key, channel.base_url])) return false;
    if (filter.kind && channelSupportsKind(plugins, channel, filter.kind) === false) return false;
    if (filter.plugin && channel.plugin_key !== filter.plugin) return false;
    if (filter.status === "upgrade") return !!availableUpgrade(plugins, channel);
    if (filter.status) return channelHealth(channel, plugins).tone === filter.status;
    return true;
  });
}

/** 插件表的前端筛选：能力按最新版本的声明筛 */
export function filterPlugins(
  plugins: readonly PluginView[],
  channels: readonly ChannelView[],
  filter: PluginFilter,
): PluginView[] {
  return plugins.filter((plugin) => {
    if (!matches(filter.query, [plugin.name, plugin.key])) return false;
    if (filter.kind) {
      const meta = latestVersion(plugin)?.meta;
      if (!meta || !metaSupportsKind(meta, filter.kind)) return false;
    }
    if (filter.source && plugin.source !== filter.source) return false;
    if (filter.status === "on") return plugin.enabled;
    if (filter.status === "off") return !plugin.enabled;
    if (filter.status === "outdated") {
      return channels.some(
        (channel) => channel.plugin_key === plugin.key && !!availableUpgrade([plugin], channel),
      );
    }
    return true;
  });
}

/**
 * 前端分页：页码从 1 开始，超出范围时夹到有效范围
 * （筛选或数据刷新后总页数变少，当前页不会落空）。
 */
export function paginate<T>(list: readonly T[], page: number, size: number) {
  const pageCount = Math.max(1, Math.ceil(list.length / size));
  const current = Math.min(Math.max(1, page), pageCount);
  return { rows: list.slice((current - 1) * size, current * size), page: current, pageCount };
}
