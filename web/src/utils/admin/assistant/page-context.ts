import type { AdminPageContext } from "@/api/agent/type";
import { findNav } from "@/pages/admin/admin-nav";

/** 页面上下文的引用标签：页面、渠道、模型、插件 */
export type ContextChipId = "page" | "channel" | "model" | "plugin";

/** 页面里选中的东西（由各后台页面登记） */
export type PageHints = {
  /** 当前选中的模型 */
  modelKey?: string;
  /** 当前选中的渠道 */
  channelKey?: string;
  /** 当前选中的插件 */
  pluginKey?: string;
};

/** 一个引用标签（形状同画布 Agent 的 ContextRef，可直接传给 AgentPanel） */
export type ContextChip = {
  /** 标签种类，移除时按它记 */
  id: ContextChipId;
  /** 显示的文字：页面是页面名，其余是「渠道：x」「模型：x」「插件：x」 */
  label: string;
  /** 悬停说明：它代表的值（路由或 key） */
  title: string;
  /** 它代表的值：页面是路由，其余是 key */
  value: string;
};

/**
 * 当前页面的名字：取自后台导航，认不出来的路径叫「后台」
 * @param pathname 当前路由
 */
const pageName = (pathname: string) => findNav(pathname)?.item.label ?? "后台";

/**
 * 输入框里的引用标签：页面名在前，然后是选中的渠道、模型、插件
 * @param pathname 当前路由
 * @param hints 页面登记的选中项
 */
export function pageContextChips(pathname: string, hints: PageHints): ContextChip[] {
  const chips: ContextChip[] = [
    { id: "page", label: pageName(pathname), title: pathname, value: pathname },
  ];
  const add = (id: ContextChipId, name: string, key: string | undefined) => {
    if (key) chips.push({ id, label: `${name}：${key}`, title: key, value: key });
  };
  add("channel", "渠道", hints.channelKey);
  add("model", "模型", hints.modelKey);
  add("plugin", "插件", hints.pluginKey);
  return chips;
}

/**
 * 发送时带上的 page_context：用户移除的标签不带；标签全被移除就不带上下文。
 * 接口要求 route 必填，所以只要还剩任何一个标签，route 都会带上
 * @param pathname 当前路由
 * @param hints 页面登记的选中项
 * @param dismissed 用户移除的标签
 * @returns 没有上下文时为 undefined
 */
export function buildPageContext(
  pathname: string,
  hints: PageHints,
  dismissed: ReadonlySet<ContextChipId>,
): AdminPageContext | undefined {
  const kept = pageContextChips(pathname, hints).filter((c) => !dismissed.has(c.id));
  if (kept.length === 0) return undefined;
  const context: AdminPageContext = { route: pathname };
  for (const chip of kept) {
    if (chip.id === "channel") context.channel_key = chip.value;
    if (chip.id === "model") context.model_key = chip.value;
    if (chip.id === "plugin") context.plugin_key = chip.value;
  }
  return context;
}

/**
 * 已发出的消息下方的引用：由当时的 page_context 还原标签
 * @param context 消息的 page_context
 */
export function describePageContext(context: AdminPageContext | undefined): ContextChip[] {
  if (!context) return [];
  return pageContextChips(context.route, {
    channelKey: context.channel_key,
    modelKey: context.model_key,
    pluginKey: context.plugin_key,
  });
}
