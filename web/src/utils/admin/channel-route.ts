/** 渠道弹窗的页签 */
export type ChannelTab = "config" | "models";

/** 插件弹窗的页签 */
export type PluginTab = "overview" | "versions";

/** 渠道页 URL 要打开的弹窗：新建（可预选插件），或已有渠道的某个页签 */
export type ChannelRouteTarget =
  | { kind: "new"; pluginKey?: string }
  | { kind: "edit"; key: string; tab: ChannelTab };

const CHANNEL_TABS: readonly string[] = ["config", "models"];
const PLUGIN_TABS: readonly string[] = ["overview", "versions"];

/**
 * 解析渠道页的 URL 参数：
 * - `?key=<key>[&tab=config|models]` 打开渠道弹窗（默认配置）；
 * - `?new=1[&plugin=<插件 key>]` 新建；
 * - 旧链接 `?edit=new` 等同新建，`?edit=<key>` 打开配置页签（别处的跳转链接还在用）。
 * 渠道不存在时不开弹窗，把缺失的 key 报出来（清单没加载完 ready=false 时先不报，避免闪错）。
 * 新建需要写权限。
 */
export function parseChannelRoute(
  params: URLSearchParams,
  channels: ReadonlyArray<{ key: string }>,
  { canWrite, ready }: { canWrite: boolean; ready: boolean },
): { target: ChannelRouteTarget | null; missing: string | null } {
  const edit = params.get("edit");
  if (params.get("new") === "1" || edit === "new") {
    return {
      target: canWrite ? { kind: "new", pluginKey: params.get("plugin") ?? undefined } : null,
      missing: null,
    };
  }
  const key = params.get("key") ?? edit;
  if (!key) return { target: null, missing: null };
  if (!channels.some((channel) => channel.key === key)) {
    return { target: null, missing: ready ? key : null };
  }
  const tab = params.get("tab");
  return {
    target: {
      kind: "edit",
      key,
      tab: CHANNEL_TABS.includes(tab ?? "") ? (tab as ChannelTab) : "config",
    },
    missing: null,
  };
}

/** 解析插件页的 URL 参数：`?key=<key>[&tab=versions]`；插件不存在的处理同渠道 */
export function parsePluginRoute(
  params: URLSearchParams,
  plugins: ReadonlyArray<{ key: string }>,
  ready: boolean,
): { target: { key: string; tab: PluginTab } | null; missing: string | null } {
  const key = params.get("key");
  if (!key) return { target: null, missing: null };
  if (!plugins.some((plugin) => plugin.key === key)) {
    return { target: null, missing: ready ? key : null };
  }
  const tab = params.get("tab");
  return {
    target: { key, tab: PLUGIN_TABS.includes(tab ?? "") ? (tab as PluginTab) : "overview" },
    missing: null,
  };
}
