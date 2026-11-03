import type { AgentEventDto } from "@/api/agent/type";
import { adminChangePayload, adminChangeResult } from "@/utils/admin/assistant/change-order";

/**
 * 后台助手的变更单应用后，页面据此重新拉数据的 window 事件名。
 * 纯前端约定，detail 为 { targets }，见 docs/design/后台Agent助手/一期接口契约.md 3.5
 */
export const ADMIN_CONFIG_CHANGED = "admin-config-changed";

/** 配置变更的对象，新建模型和修改模型共用 model */
export type AdminConfigTarget =
  /** 模型 */
  | "model"
  /** 定价设置 */
  | "pricing_settings"
  /** 插件 */
  | "plugin"
  /** 渠道 */
  | "channel";

/** admin-config-changed 事件的 detail */
export type AdminConfigChangedDetail = {
  /** 这次真正改动了哪些对象 */
  targets: AdminConfigTarget[];
};

/**
 * 一条事件造成了哪些配置变更：只有 approval.decided、种类是 admin_change、结果里有已应用条目才算
 * @param event Agent 事件
 * @returns 受影响的对象（去重，按出现顺序）；不算配置变更返回 null
 */
export function adminConfigChangeTargets(event: AgentEventDto): AdminConfigTarget[] | null {
  if (event.type !== "approval.decided") return null;
  const approval = event.data as never;
  const payload = adminChangePayload(approval);
  const result = adminChangeResult(approval);
  if (!payload || !result) return null;
  const targets: AdminConfigTarget[] = [];
  for (const outcome of result.items) {
    if (outcome.status !== "applied") continue;
    const raw = payload.items[outcome.index]?.target;
    const target = raw === "model_create" ? "model" : raw;
    if (target && !targets.includes(target)) targets.push(target);
  }
  return targets.length > 0 ? targets : null;
}

/**
 * 事件若造成了配置变更，就在 window 上派发 admin-config-changed
 * @param event Agent 事件
 */
export function notifyAdminConfigChanged(event: AgentEventDto): void {
  const targets = adminConfigChangeTargets(event);
  if (!targets || typeof window === "undefined") return;
  window.dispatchEvent(
    new CustomEvent<AdminConfigChangedDetail>(ADMIN_CONFIG_CHANGED, { detail: { targets } }),
  );
}
