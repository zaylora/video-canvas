import { readDialogEntry } from "./dialog-entry";

/** 插件弹窗的页签 */
export type PluginTab = "overview" | "versions";

/** 渠道与模型主从页的路径 */
export const CHANNELS_PAGE = "/admin/ai/channels";

/** 左栏渠道列表的状态筛选：全部 / 已启用 / 已停用 */
export type ChannelStatusFilter = "all" | "on" | "off";

/** 主从页深链的参数 */
export type ChannelsPageQuery = {
  /** 选中的渠道 key */
  channel?: string;
  /** 打开这个模型的编辑弹窗 */
  model?: string;
  /** 与 model 同用：打开后直接弹测试弹窗 */
  test?: boolean;
  /** 左栏状态筛选；旧的健康筛选值（ok / warn / bad）按“已启用”处理 */
  status?: string;
};

/**
 * 渠道与模型主从页的路径（别的页面跳转统一用它）。
 * @param opts 要选中的渠道、要打开的模型、是否直接试跑、左栏筛选
 * @returns 例如 `/admin/ai/channels?channel=yswg&model=gpt-image-2&test=1`
 */
export function channelsPagePath(opts: ChannelsPageQuery = {}): string {
  const params = new URLSearchParams();
  if (opts.channel) params.set("channel", opts.channel);
  if (opts.model) params.set("model", opts.model);
  if (opts.test) params.set("test", "1");
  if (opts.status) params.set("status", opts.status);
  const query = params.toString();
  return query ? `${CHANNELS_PAGE}?${query}` : CHANNELS_PAGE;
}

/** 把任意状态值收窄成左栏筛选：off 为已停用，ok / warn / bad / on 为已启用，其余为全部 */
function normalizeStatus(raw: string | null): ChannelStatusFilter {
  if (raw === "off") return "off";
  if (raw === "on" || raw === "ok" || raw === "warn" || raw === "bad") return "on";
  return "all";
}

/**
 * 解析主从页的查询串。
 * @param search location.search（带不带 ? 都行）
 */
export function parseChannelsQuery(search: string): {
  channel: string | null;
  model: string | null;
  test: boolean;
  status: ChannelStatusFilter;
} {
  const params = new URLSearchParams(search);
  const test = params.get("test");
  return {
    channel: params.get("channel") || null,
    model: params.get("model") || null,
    test: test === "1" || test === "true",
    status: normalizeStatus(params.get("status")),
  };
}

/**
 * 决定左栏选中哪个渠道：想选的存在就选它，否则（没指定、已被删）选第一个；一个渠道也没有为 null。
 * @param channels 渠道列表（渠道顺序）
 * @param wanted 想选的渠道 key
 */
export function pickSelectedChannel(
  channels: ReadonlyArray<{ key: string }>,
  wanted: string | null | undefined,
): string | null {
  if (wanted && channels.some((channel) => channel.key === wanted)) return wanted;
  return channels[0]?.key ?? null;
}

/**
 * 旧的 `/admin/ai/models` 入口重定向到主从页：保留 channel / model / test 查询参数；
 * 旧代码经路由 state（dialogEntry）带来的模型 key 与 test 也转成查询参数，查询串优先。
 * @param search 旧页面的 location.search
 * @param state 旧页面的 location.state
 * @returns 主从页路径
 */
export function legacyModelsRedirect(search: string, state: unknown): string {
  const query = parseChannelsQuery(search);
  const entry = readDialogEntry<{ key?: string; test?: boolean }>(state);
  const model = query.model ?? entry?.key ?? null;
  return channelsPagePath({
    channel: query.channel ?? undefined,
    model: model ?? undefined,
    test: model ? query.test || (query.model === null && !!entry?.test) : false,
  });
}
