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
  | "channel"
  /** 用户管理 */
  | "user"
  /** 注册设置 */
  | "register_settings"
  /** 邮件服务 */
  | "smtp_settings"
  /** 存储配置 */
  | "storage"
  /** 图片服务 */
  | "image_processor";

/** admin-config-changed 事件的 detail */
export type AdminConfigChangedDetail = {
  /** 这次真正改动了哪些对象 */
  targets: AdminConfigTarget[];
};

/**
 * 变更单完成后要刷新哪些页面；管理操作失败也可能已经记录检查结果或完成部分取消，必须重新读取。
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
    const raw = payload.items[outcome.index]?.target;
    const managed = [
      "user",
      "register_settings",
      "smtp_settings",
      "storage",
      "image_processor",
    ].includes(raw ?? "");
    if (outcome.status !== "applied" && !(managed && outcome.status === "failed")) continue;
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
