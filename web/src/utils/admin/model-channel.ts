import type { ChannelView, PluginView } from "@/api/admin/ai/type";

import { readModelChannel, readModelKind } from "./model-body";
import { readModelProtocol, settingsProblem, type ModelProtocol } from "./model-protocol";
import {
  describeAuth,
  findPluginVersion,
  latestVersion,
  metaSupportsKind,
  shortSha,
} from "./plugin";

/** 模型正文选中的渠道与协议，连同协议插件的信息（模型编辑器的“选中后显示”与启用前提示共用） */
export type ModelChannelInfo = {
  /** channels[0].channel；没选为空串 */
  channelKey: string;
  /** 渠道；没选或清单里找不到为 null */
  channel: ChannelView | null;
  /** 模型能力（kind）；没选为空串 */
  kind: string;
  /** channels[0].protocol；没选（待配置 / Agent）为 null */
  protocol: ModelProtocol | null;
  /** 协议插件显示名；没选协议为空串，找不到插件时用插件 key */
  pluginName: string;
  /** 固定的插件版本号 */
  pluginVersion: string;
  /** 协议版本 sha256 前 8 位；未知为空串 */
  sha8: string;
  /** 协议鉴权方式说明；协议信息未知为空串 */
  authLabel: string;
  /** 协议鉴权类型；Agent 或协议信息未知为 null（这时由 keyMissing 单独判断 Agent） */
  authType: string | null;
  /** 渠道 API Key 是否已设置；没选渠道为 false */
  secretSet: boolean;
  /** 需要 Key 却没设置（Agent 一定需要；其他模型看协议版本的鉴权）：启用会被后端拦（50015），前端提前提示 */
  keyMissing: boolean;
  /** 协议版本的 endpoints 里是否有该模型的 kind；Agent、没选协议或协议信息未知为 null */
  supportsKind: boolean | null;
  /** 协议层面的问题（文案与后端启用检查一致）；没有问题为 null。没选能力时不检查 */
  protocolProblem: string | null;
};

/** 协议没选时的提示（后端启用检查的原文） */
const NO_PROTOCOL = "该模型还没有选择调用协议";

/** 解析模型正文里选中的渠道、协议与协议插件的信息 */
export function resolveModelChannel(
  body: unknown,
  channels: readonly ChannelView[],
  plugins: readonly PluginView[],
): ModelChannelInfo {
  const { channel: channelKey } = readModelChannel(body);
  const kind = readModelKind(body);
  const protocol = readModelProtocol(body);
  const channel = channels.find((item) => item.key === channelKey) ?? null;
  const base: ModelChannelInfo = {
    channelKey,
    channel,
    kind,
    protocol,
    pluginName: "",
    pluginVersion: protocol?.version ?? "",
    sha8: "",
    authLabel: "",
    authType: null,
    secretSet: !!channel?.secret_set,
    keyMissing: false,
    supportsKind: null,
    protocolProblem: null,
  };
  // Agent 由宿主网关直接用渠道 Key 请求，不看协议；带了协议是正文校验问题
  if (kind === "agent") return { ...base, keyMissing: !!channel && !channel.secret_set };
  if (!protocol) return { ...base, protocolProblem: kind ? NO_PROTOCOL : null };

  const plugin = plugins.find((item) => item.key === protocol.plugin);
  const found = protocol.version
    ? findPluginVersion(plugins, protocol.plugin, { version: protocol.version })
    : latestVersion(plugin);
  const meta = found?.meta ?? null;
  const ref = `${protocol.plugin}@${protocol.version}`;
  let problem: string | null = null;
  if (!kind) problem = null;
  else if (!plugin) problem = `模型选择的协议插件 "${protocol.plugin}" 不存在`;
  else if (!plugin.enabled) problem = `模型选择的协议插件 "${protocol.plugin}" 已停用`;
  else if (!found) problem = `模型固定的插件版本 ${ref} 不存在`;
  else if (!meta) problem = `插件 ${ref} 的 meta 无法解析`;
  else if (!metaSupportsKind(meta, kind)) problem = `协议 ${ref} 不支持 ${kind} 类型的模型`;
  else problem = settingsProblem(meta.channelSettings, protocol.settings);

  const authType = meta ? (meta.auth?.type ?? "none") : null;
  return {
    ...base,
    pluginName: plugin?.name ?? protocol.plugin,
    pluginVersion: found?.version ?? protocol.version,
    sha8: shortSha(found?.sha256),
    authLabel: meta ? describeAuth(meta.auth) : "",
    authType,
    keyMissing: !!channel && authType !== null && authType !== "none" && !channel.secret_set,
    supportsKind: meta && kind ? metaSupportsKind(meta, kind) : null,
    protocolProblem: problem,
  };
}

/**
 * 模型启用被拦的原因；可以启用返回 null。
 * 镜像后端的启用检查链（文案一致）：正文校验（能力、渠道）→ 渠道存在且启用 → Agent 只看渠道 Key →
 * 协议已选 → 插件存在且启用 → 固定版本存在 → 版本支持该能力 → 协议设置合法 → 版本鉴权非 none 时渠道 Key 已设置。
 * 其余正文校验（价格、参数等）由后端校验接口给出，这里不重复。
 * 渠道与插件清单还没加载好（ready=false）时不下结论。
 */
export function publishBlockReason(info: ModelChannelInfo, ready: boolean): string | null {
  if (!ready) return null;
  if (!info.kind) return "还没有选择模型能力";
  if (info.kind === "agent" && info.protocol)
    return "Agent 模型由宿主的 LLM 网关直接请求渠道，不需要选择协议";
  if (!info.channelKey) return "还没有选择渠道";
  if (!info.channel) return `渠道 ${info.channelKey} 不存在`;
  if (!info.channel.enabled) return `渠道 ${info.channel.name} 已停用`;
  if (info.protocolProblem) return info.protocolProblem;
  if (info.keyMissing)
    return `渠道 ${info.channel.name} 的 Key 尚未设置，请联系超级管理员设置后再试`;
  return null;
}
