/** 渠道弹窗的页签 */
export type ChannelTab = "config" | "models";

/** 插件弹窗的页签 */
export type PluginTab = "overview" | "versions";

/** 渠道页要打开的弹窗：新建（可预选插件），或已有渠道的某个页签（不给页签停在配置） */
export type ChannelDialogOpen =
  | { kind: "new"; pluginKey?: string }
  | { kind: "edit"; key: string; tab?: ChannelTab };

/** 解析后的弹窗目标：页签已补全 */
export type ChannelRouteTarget =
  | { kind: "new"; pluginKey?: string }
  | { kind: "edit"; key: string; tab: ChannelTab };

/**
 * 渠道页弹窗的状态放在页面自己的 state 里，不进 URL（进了 URL 会把列表的类型、搜索、页码筛选冲掉）。
 * 这里把它解析成能直接渲染的目标：
 * 渠道不存在时不开弹窗，把缺失的 key 报出来（清单没加载完 ready=false 时先不报，避免闪错）；新建需要写权限。
 */
export function resolveChannelDialog(
  open: ChannelDialogOpen | null,
  channels: ReadonlyArray<{ key: string }>,
  { canWrite, ready }: { canWrite: boolean; ready: boolean },
): { target: ChannelRouteTarget | null; missing: string | null } {
  if (!open) return { target: null, missing: null };
  if (open.kind === "new") {
    return { target: canWrite ? { kind: "new", pluginKey: open.pluginKey } : null, missing: null };
  }
  if (!channels.some((channel) => channel.key === open.key)) {
    return { target: null, missing: ready ? open.key : null };
  }
  return { target: { kind: "edit", key: open.key, tab: open.tab ?? "config" }, missing: null };
}
