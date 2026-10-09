import type { ChannelView, PluginView } from "@/api/admin/ai/type";

import { describeAuth, findPluginVersion, metaSupportsKind, shortSha } from "./plugin";
import { readModelChannel, readModelKind } from "./model-body";

/** 模型正文选中的渠道，连同它的插件信息（模型编辑器的“选中后显示”共用） */
export type ModelChannelInfo = {
  /** channels[0].channel；没选为空串 */
  channelKey: string;
  /** 渠道；没选或清单里找不到为 null */
  channel: ChannelView | null;
  /** 插件显示名；找不到插件时用插件 key */
  pluginName: string;
  /** 固定的插件版本号 */
  pluginVersion: string;
  /** 插件版本 sha256 前 8 位；未知为空串 */
  sha8: string;
  /** 鉴权方式说明；插件信息未知为空串 */
  authLabel: string;
  /** 鉴权类型；插件信息未知为 null（这时不能断言“需要 Key”） */
  authType: string | null;
  /** 渠道 Key 是否已设置；没选渠道为 false */
  secretSet: boolean;
  /** 需要 Key 却没设置：上线会被后端拦（50015），前端提前提示 */
  keyMissing: boolean;
  /** 插件版本的 endpoints 里是否有该模型的 kind；插件信息未知为 null */
  supportsKind: boolean | null;
};

/** 解析模型正文里选中的渠道与它的插件信息 */
export function resolveModelChannel(
  body: unknown,
  channels: readonly ChannelView[],
  plugins: readonly PluginView[],
): ModelChannelInfo {
  const { channel: channelKey } = readModelChannel(body);
  const kind = readModelKind(body);
  const channel = channels.find((item) => item.key === channelKey) ?? null;
  if (!channel) {
    return {
      channelKey,
      channel: null,
      pluginName: "",
      pluginVersion: "",
      sha8: "",
      authLabel: "",
      authType: null,
      secretSet: false,
      keyMissing: false,
      supportsKind: null,
    };
  }
  const plugin = plugins.find((item) => item.key === channel.plugin_key);
  const version = findPluginVersion(plugins, channel.plugin_key, {
    id: channel.plugin_version_id,
    version: channel.plugin_version,
  });
  const meta = version?.meta ?? null;
  const authType = meta ? (meta.auth?.type ?? "none") : null;
  return {
    channelKey,
    channel,
    pluginName: plugin?.name ?? channel.plugin_key,
    pluginVersion: channel.plugin_version,
    sha8: shortSha(version?.sha256),
    authLabel: meta ? describeAuth(meta.auth) : "",
    authType,
    secretSet: channel.secret_set,
    keyMissing: authType !== null && authType !== "none" && !channel.secret_set,
    supportsKind: meta && kind ? metaSupportsKind(meta, kind) : null,
  };
}

/**
 * 模型上线被拦的原因；可以上线返回 null。
 * 只挡后端一定会拒绝、且前端有把握判断的情况：渠道不存在、渠道停用、需要 Key 却没设置。
 * 清单还没加载好（channelsReady=false）时不下结论。
 */
export function publishBlockReason(info: ModelChannelInfo, channelsReady: boolean): string | null {
  if (!channelsReady) return null;
  if (!info.channelKey) return "还没有选择渠道";
  if (!info.channel) return `渠道 ${info.channelKey} 不存在`;
  if (!info.channel.enabled) return `渠道 ${info.channel.name} 已停用`;
  if (info.keyMissing)
    return `渠道 ${info.channel.name} 还没有设置 Key，模型无法上线。请联系运维在“渠道”页设置 Key。`;
  return null;
}
