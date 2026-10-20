/** 后台分页列表的统一返回：当页的行与命中总数 */
export interface AuditPage {
  list: AuditItem[];
  total: number;
  page: number;
  page_size: number;
}

/** 审计模块：与动作名的前缀一致；不传表示全部 */
export type AuditModule =
  | "user"
  | "settings"
  | "showcase"
  | "system"
  | "payment"
  | "channel"
  | "plugin"
  | "model"
  | "storage"
  | "agent";

/** 操作审计列表请求参数 */
export interface ListAuditParams {
  /** 用户名或数字 id */
  actor?: string;
  module?: AuditModule;
  /** 完整动作名 */
  action?: string;
  /** 起止时间（RFC3339）；默认近 30 天 */
  from?: string;
  to?: string;
  /** 页码，从 1 起，缺省 1 */
  page?: number;
  /** 每页条数，缺省 20，最大 100 */
  page_size?: number;
}

/** 合并后的一条审计：admin 来自 admin_audit_logs，ai 来自 ai_audit_logs */
export interface AuditItem {
  source: "admin" | "ai";
  id: number;
  /** 0 表示系统自动操作，此时 actor_name 为空串 */
  actor_id: number;
  actor_name: string;
  action: string;
  target_type: string;
  /** 用户类对象是用户 id 的字符串，渠道、插件等是 key */
  target_ref: string;
  /** 能解析时的展示名（如用户名），否则为空串 */
  target_name: string;
  detail_json: Record<string, unknown> | null;
  created_at: string;
}
