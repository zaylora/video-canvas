import type { PluginMeta, PluginView, SettingSchema } from "@/api/admin/ai/type";

import { MODEL_KIND_LABEL, readModelString, withModelField } from "./model-body";
import { defaultCapabilities, defaultDeadline, defaultPricing } from "./model-template";
import { compareSemver, findPluginVersion, latestVersion, metaSupportsKind } from "./plugin";
import { initialSettingValues, settingFields, validateSettingValues } from "./settings-form";

/**
 * 模型“调用协议”（channels[0].protocol）的纯函数：读写正文、按能力列出可选协议、
 * 搜索、选中 / 切版本 / 切能力时设置项的整理。渠道不绑协议，协议由模型自己选。
 * 界面（pages/admin/ai/models/protocol-picker.tsx）只负责展示，规则都在这里，方便单测。
 */

/** 模型选的调用协议：协议插件、固定的插件版本、协议设置项取值 */
export type ModelProtocol = {
  /** 插件 key */
  plugin: string;
  /** 固定的 semver 版本号；空串表示正文里没写（保存时由后端补成最新版本） */
  version: string;
  /** 协议设置项取值：只存插件声明过的项 */
  settings: Record<string, unknown>;
};

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

/** 正文里的 channels[0].protocol；没选、不是对象或缺插件 key 返回 null */
export function readModelProtocol(body: unknown): ModelProtocol | null {
  const first = isRecord(body) && Array.isArray(body.channels) ? body.channels[0] : undefined;
  const raw = isRecord(first) ? first.protocol : undefined;
  if (!isRecord(raw) || typeof raw.plugin !== "string" || !raw.plugin) return null;
  return {
    plugin: raw.plugin,
    version: typeof raw.version === "string" ? raw.version : "",
    settings: isRecord(raw.settings) ? raw.settings : {},
  };
}

/**
 * 把 channels[0].protocol 改成指定协议，传 null 则删除这一项；其余字段与键顺序不动。
 * 正文没有 channels 时：写入会补一项（渠道与上游模型名留空待填），清除则原样返回。正文不是对象返回 null。
 */
export function withModelProtocol(
  body: unknown,
  protocol: ModelProtocol | null,
): Record<string, unknown> | null {
  if (!isRecord(body)) return null;
  const channels = Array.isArray(body.channels) ? [...body.channels] : [];
  if (protocol === null && !isRecord(channels[0])) return body;
  const first = isRecord(channels[0]) ? channels[0] : {};
  channels[0] =
    protocol === null
      ? Object.fromEntries(Object.entries(first).filter(([name]) => name !== "protocol"))
      : {
          channel: first.channel ?? "",
          upstream_model: first.upstream_model ?? "",
          ...first,
          protocol,
        };
  return withModelField(body, "channels", channels);
}

/** 协议列表里的一行：一个插件在最新版本里对某种能力声明的请求 */
export type ProtocolOption = {
  /** 插件 key */
  key: string;
  /** 插件显示名 */
  name: string;
  /** 厂商 slug；插件没声明为空串 */
  vendor: string;
  /** 插件描述 */
  description: string;
  /** 插件是否启用；停用的插件也列出，但不可选 */
  enabled: boolean;
  /** 不可选的原因；可选为 null */
  disabledReason: string | null;
  /** 最新版本号 */
  latestVersion: string;
  /** 同步 / 异步；插件没声明为空串 */
  mode: "sync" | "async" | "";
  /** 请求方法（展示用），没声明按 POST */
  method: string;
  /** 请求路径（展示用，宿主不据此发请求）；没声明为空串 */
  path: string;
  /** 鉴权类型；meta 没声明为 none */
  authType: string;
};

/** 插件停用时在协议列表里写的原因 */
export const PLUGIN_DISABLED_REASON = "插件已停用，不可选";

/**
 * 按能力列出可选协议：取每个插件最新版本的 endpoints[kind]。
 * 已停用的插件也列出（置灰、写明原因），排在可选的后面；Agent 和没选能力不需要协议，返回空。
 */
export function protocolOptions(plugins: readonly PluginView[], kind: string): ProtocolOption[] {
  if (!kind || kind === "agent") return [];
  const options: ProtocolOption[] = [];
  for (const plugin of plugins) {
    const latest = latestVersion(plugin);
    const endpoints = latest?.meta?.endpoints;
    const endpoint = isRecord(endpoints) ? endpoints[kind] : undefined;
    if (!latest || !endpoint) continue;
    options.push({
      key: plugin.key,
      name: plugin.name || plugin.key,
      vendor: latest.meta?.vendor ?? "",
      description: latest.meta?.description ?? "",
      enabled: plugin.enabled,
      disabledReason: plugin.enabled ? null : PLUGIN_DISABLED_REASON,
      latestVersion: latest.version,
      mode: endpoint.mode === "sync" || endpoint.mode === "async" ? endpoint.mode : "",
      method: (endpoint.method || "POST").toUpperCase(),
      path: endpoint.path ?? "",
      authType: latest.meta?.auth?.type ?? "none",
    });
  }
  // Array.prototype.sort 是稳定排序：只把停用的挪到后面，其余保持插件原有顺序
  return options.sort((a, b) => Number(!a.enabled) - Number(!b.enabled));
}

/** 搜索协议：协议名称、插件 key、厂商、请求路径（含“POST /v1/…”写法），忽略大小写；空搜索词返回全部 */
export function filterProtocolOptions(
  options: readonly ProtocolOption[],
  query: string,
): ProtocolOption[] {
  const text = query.trim().toLowerCase();
  if (!text) return [...options];
  return options.filter((item) =>
    [item.name, item.key, item.vendor, item.path, `${item.method} ${item.path}`]
      .join("\n")
      .toLowerCase()
      .includes(text),
  );
}

/**
 * 版本下拉的可选版本（新到旧）：只列声明了该能力的版本；
 * 当前已固定的版本即使不支持也保留，免得下拉里找不到当前值。
 */
export function protocolVersions(
  plugin: PluginView | undefined,
  kind: string,
  pinned?: string,
): string[] {
  const list = (plugin?.versions ?? [])
    .filter((item) => item.version === pinned || (!!item.meta && metaSupportsKind(item.meta, kind)))
    .map((item) => item.version);
  return [...new Set(list)].sort((a, b) => compareSemver(b, a));
}

/**
 * 按声明整理协议设置项：只保留声明过的项，缺的补默认值，取值放不进声明类型的回到默认值。
 * 没有默认值的可选项不出现；必填项没填也不出现（由必填校验提示，不在这里补值）。
 */
export function normalizeProtocolSettings(
  schema: SettingSchema | null | undefined,
  current: Record<string, unknown> | null | undefined,
): Record<string, unknown> {
  const fields = settingFields(schema);
  if (fields.length === 0) return {};
  return validateSettingValues(fields, initialSettingValues(fields, current)).values;
}

/**
 * 协议设置项按版本声明校验（必填、数字格式、枚举取值），不合法时拼成“协议设置不合法：名称：问题”的一句话；合法返回 null。
 * 文案与后端启用检查一致，只是前端提前提示。
 */
export function settingsProblem(
  schema: SettingSchema | null | undefined,
  settings: Record<string, unknown>,
): string | null {
  const fields = settingFields(schema);
  const { ok, errors } = validateSettingValues(fields, initialSettingValues(fields, settings));
  if (ok) return null;
  return `协议设置不合法：${Object.entries(errors)
    .map(([name, message]) => `${name}：${message}`)
    .join("；")}`;
}

/** 在界面里选中一个协议：固定该插件最新版本，设置项填默认值；插件不存在、已停用、最新版本不支持该能力返回 null */
export function selectProtocol(
  plugins: readonly PluginView[],
  pluginKey: string,
  kind: string,
): ModelProtocol | null {
  const plugin = plugins.find((item) => item.key === pluginKey);
  const latest = latestVersion(plugin);
  if (!plugin?.enabled || !latest?.meta || !metaSupportsKind(latest.meta, kind)) return null;
  return {
    plugin: plugin.key,
    version: latest.version,
    settings: normalizeProtocolSettings(latest.meta.channelSettings, {}),
  };
}

/** 切换版本的结果：新协议与被丢弃的设置项名 */
export type ProtocolVersionChange = {
  protocol: ModelProtocol;
  /** 新版本不再声明、已丢弃的设置项名（确认框 / 提示里列出） */
  dropped: string[];
};

/**
 * 切换协议版本：设置项按新版本的声明重新整理（沿用同名项、丢弃不再声明的、补新项的默认值）。
 * 找不到该版本（或 meta 损坏）时原样返回，不改动。
 */
export function changeProtocolVersion(
  plugins: readonly PluginView[],
  protocol: ModelProtocol,
  version: string,
): ProtocolVersionChange {
  const meta = findPluginVersion(plugins, protocol.plugin, { version })?.meta;
  if (!meta) return { protocol, dropped: [] };
  const declared = isRecord(meta.channelSettings) ? meta.channelSettings : {};
  return {
    protocol: {
      plugin: protocol.plugin,
      version,
      settings: normalizeProtocolSettings(meta.channelSettings, protocol.settings),
    },
    dropped: Object.keys(protocol.settings).filter((name) => !(name in declared)),
  };
}

/** 切换能力后协议该怎么办 */
export type KindSwitch = {
  /** 切换后的协议；null 表示没有（清空或本来就没选） */
  protocol: ModelProtocol | null;
  /** 需要告诉运营的事（协议被清空时）；没有为 null */
  notice: string | null;
};

/**
 * 切换模型能力时的协议规则：
 * - 切到 Agent：协议必须不存在，已选的清空并提示；
 * - 已选协议的固定版本支持新能力：保留；
 * - 不支持（或插件已不存在）：清空，已填的设置项一并丢弃，并提示重新选择。
 */
export function switchModelKind(
  plugins: readonly PluginView[],
  current: ModelProtocol | null,
  nextKind: string,
): KindSwitch {
  if (!current) return { protocol: null, notice: null };
  if (nextKind === "agent")
    return { protocol: null, notice: "Agent 模型不需要选择协议，已清空已选协议" };
  const meta = current.version
    ? findPluginVersion(plugins, current.plugin, { version: current.version })?.meta
    : latestVersion(plugins.find((item) => item.key === current.plugin))?.meta;
  if (meta && metaSupportsKind(meta, nextKind)) return { protocol: current, notice: null };
  return {
    protocol: null,
    notice: `已选协议不支持${MODEL_KIND_LABEL[nextKind] ?? nextKind}，请重新选择`,
  };
}

/**
 * 改正文里的模型能力：能力与定价重置成新种类的默认值（生成方式、素材、参数、计费方式都不通用），
 * 正文没有时限就补该种类的默认时限，协议按 switchModelKind 处理。正文不是对象返回 null。
 */
export function withModelKind(
  body: unknown,
  kind: string,
  plugins: readonly PluginView[],
): { body: Record<string, unknown>; notice: string | null } | null {
  if (typeof body !== "object" || body === null || Array.isArray(body)) return null;
  const { protocol, notice } = switchModelKind(plugins, readModelProtocol(body), kind);
  let next = withModelField(body, "kind", kind);
  next = next && withModelField(next, "capabilities", defaultCapabilities(kind));
  next = next && withModelField(next, "pricing", defaultPricing(kind));
  if (next && !readModelString(next, "deadline"))
    next = withModelField(next, "deadline", defaultDeadline(kind));
  next = next && withModelProtocol(next, protocol);
  return next ? { body: next, notice } : null;
}

/** 协议的简短写法：“插件名 @版本”，表格的“请求协议”列、详情里用 */
export const describeProtocol = (name: string, version: string) =>
  version ? `${name} @${version}` : name;

/** 协议设置项的 meta 声明（版本不存在时为 undefined） */
export const protocolSettingSchema = (
  plugins: readonly PluginView[],
  protocol: ModelProtocol,
): PluginMeta["channelSettings"] =>
  findPluginVersion(plugins, protocol.plugin, { version: protocol.version })?.meta?.channelSettings;

/**
 * 批量设置能力与协议用：只改 kind 与 channels[0].protocol，不改价格。
 * 能力没变就只换协议；能力变了（含待配置的模型第一次选能力）就把能力与参数重置成新种类的默认值，
 * 价格保留，正文里没有价格或时限的才补该种类的默认值。Agent 不带协议。正文不是对象返回 null。
 */
export function withBatchProtocol(
  body: unknown,
  kind: string,
  protocol: ModelProtocol | null,
): Record<string, unknown> | null {
  if (!isRecord(body)) return null;
  let next: Record<string, unknown> | null = body;
  if (readModelString(body, "kind") !== kind) {
    next = withModelField(next, "kind", kind);
    next = next && withModelField(next, "capabilities", defaultCapabilities(kind));
    if (next && !isRecord(body.pricing))
      next = withModelField(next, "pricing", defaultPricing(kind));
    if (next && !readModelString(body, "deadline"))
      next = withModelField(next, "deadline", defaultDeadline(kind));
  }
  return next && withModelProtocol(next, kind === "agent" ? null : protocol);
}
