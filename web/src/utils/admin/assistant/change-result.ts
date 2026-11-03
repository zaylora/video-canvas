import type {
  AdminChangeItem,
  AdminChangeItemResult,
  AdminChangePayload,
  AdminChangeResult,
} from "@/api/agent/type";
import { channelsPagePath } from "@/utils/admin/channel-route";

/**
 * 变更单执行后的结果卡：把已应用的条目按「页面 › 对象」分组，失败 / 跳过 / 未应用的条目另列原因。
 * 都是纯函数，页面跳转只给后台已有路由和现有的定位参数。
 */

/** 变更条目对应的后台页面名 */
const PAGE_LABEL: Record<AdminChangeItem["target"], string> = {
  model: "模型配置",
  model_create: "模型配置",
  pricing_settings: "定价设置",
  user: "用户管理",
  register_settings: "注册设置",
  smtp_settings: "邮件服务",
  storage: "存储配置",
  image_processor: "图片服务",
  plugin: "插件",
  channel: "渠道",
};

/**
 * 条目所在的后台页面名，没见过的 target 回落成「其他」
 * @param target 条目的改动对象
 */
export function targetPageLabel(target: string): string {
  return PAGE_LABEL[target as AdminChangeItem["target"]] ?? "其他";
}

/**
 * 后台对象对应的页面路径：模型 / 渠道带上现有的选中参数，插件和定价设置只跳页面（页面没有定位能力）。
 * 变更单结果卡和助手展示卡片（show_card）的 ref 共用这一份映射。
 * @param target 对象种类：model / channel / plugin / pricing_settings
 * @param key 对象标识（模型 key、渠道 key）；插件和定价设置用不到
 * @returns 没见过的种类返回 null
 */
export function adminTargetPath(target: string, key: string): string | null {
  switch (target) {
    case "model":
      return channelsPagePath({ model: key });
    case "channel":
      return channelsPagePath({ channel: key });
    case "plugin":
      return "/admin/ai/plugins";
    case "user":
      return "/admin/users";
    case "register_settings":
      return "/admin/settings/register";
    case "smtp_settings":
      return "/admin/settings/email";
    case "storage":
      return "/admin/settings/storage";
    case "image_processor":
      return "/admin/settings/image-processor";
    case "pricing_settings":
      return "/admin/settings/pricing";
    default:
      return null;
  }
}

/**
 * 条目对应的后台页面路径：新建出来的模型 / 渠道 key 以执行结果为准，其余同 adminTargetPath
 * @param item 变更条目
 * @param outcome 这一项的执行结果；新建出来的 key 以它为准
 * @returns 没见过的 target 返回 null
 */
export function targetPath(item: AdminChangeItem, outcome?: AdminChangeItemResult | null) {
  switch (item.target) {
    case "model_create":
      return adminTargetPath("model", outcome?.created?.model_key || item.key);
    case "channel":
      return adminTargetPath("channel", outcome?.created?.channel_key || item.key);
    default:
      return adminTargetPath(item.target, item.key);
  }
}

/** 结果卡里一个已应用的对象 */
export type AppliedEntry = {
  item: AdminChangeItem;
  outcome: AdminChangeItemResult;
  /** 点标题跳转的后台页面，没有为 null */
  path: string | null;
};

/** 结果卡里的一组：同一个后台页面下的已应用对象 */
export type AppliedGroup = { page: string; entries: AppliedEntry[] };

/** 没有应用成功的条目：失败 / 跳过 / 你没勾选 */
export type UnappliedEntry = {
  item: AdminChangeItem;
  status: "failed" | "skipped" | "rejected";
  /** 原因，没有时为空串 */
  message: string;
};

/**
 * 已应用的条目，按结果里的顺序平铺
 * @param payload 变更单内容
 * @param result 执行结果
 */
export function appliedEntries(
  payload: AdminChangePayload,
  result: AdminChangeResult,
): AppliedEntry[] {
  return result.items.flatMap((outcome) => {
    const item = payload.items[outcome.index];
    if (!item || outcome.status !== "applied") return [];
    return [{ item, outcome, path: targetPath(item, outcome) }];
  });
}

/**
 * 把已应用的条目按页面分组，组按第一次出现的顺序排
 * @param entries 已应用的条目
 */
export function groupByPage(entries: AppliedEntry[]): AppliedGroup[] {
  const groups: AppliedGroup[] = [];
  for (const entry of entries) {
    const page = targetPageLabel(entry.item.target);
    const group = groups.find((g) => g.page === page);
    if (group) group.entries.push(entry);
    else groups.push({ page, entries: [entry] });
  }
  return groups;
}

/**
 * 没应用成功的条目，失败在前，其次跳过，最后是你没勾选的
 * @param payload 变更单内容
 * @param result 执行结果
 */
export function unappliedEntries(
  payload: AdminChangePayload,
  result: AdminChangeResult,
): UnappliedEntry[] {
  const rank = { failed: 0, skipped: 1, rejected: 2 } as const;
  return result.items
    .flatMap((outcome) => {
      const item = payload.items[outcome.index];
      if (!item || outcome.status === "applied") return [];
      return [{ item, status: outcome.status, message: outcome.message ?? "" }];
    })
    .sort((a, b) => rank[a.status] - rank[b.status]);
}

/**
 * 一个字段改前改后的显示方式：原来没有的是新增（只显示绿色的新值），改后没有的是删除（红色删除线的旧值），
 * 其余是「旧值 → 新值」
 * @param before 改前的值，null 表示原来没有
 * @param after 改后的值，null 表示删除
 */
export function diffKind(before: unknown, after: unknown): "added" | "removed" | "changed" {
  const none = (v: unknown) => v === null || v === undefined;
  if (none(before)) return "added";
  if (none(after)) return "removed";
  return "changed";
}
