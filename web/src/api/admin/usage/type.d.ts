/** 后台分页列表的统一返回：当页的行与命中总数 */
export interface UsagePage<T> {
  list: T[];
  total: number;
  page: number;
  page_size: number;
}

/** 任务类型 */
export type UsageKind = "video" | "image" | "audio" | "text";

/** 任务状态：pending / queued / running / finalizing 都算进行中 */
export type UsageStatus =
  | "pending"
  | "queued"
  | "running"
  | "finalizing"
  | "succeeded"
  | "failed"
  | "canceled"
  | "expired";

/** 状态筛选：failed 只含失败，取消与超时各自单列；active 是四种进行中的合集 */
export type UsageStatusFilter = "all" | "active" | "succeeded" | "failed" | "canceled" | "expired";

/** 列表与摘要共用的筛选参数（GET /admin/usage、/admin/usage/summary） */
export interface UsageQuery {
  /** 用户名 / 邮箱模糊匹配；32 位十六进制按任务编号精确匹配 */
  q?: string;
  /** 只看某个用户 */
  user_id?: number;
  status?: UsageStatusFilter;
  kind?: UsageKind;
  /** 模型 key */
  model?: string;
  /** 起止时间（RFC3339）；默认近 7 天，跨度最大 90 天 */
  from?: string;
  to?: string;
  /** 是否含试跑任务，不传按 false */
  include_test?: "true" | "false";
}

/** 列表请求参数：筛选 + 页码分页 */
export interface ListUsageParams extends UsageQuery {
  /** 页码，从 1 起，缺省 1 */
  page?: number;
  /** 每页条数，缺省 20，最大 100 */
  page_size?: number;
}

/** 列表里的一条任务 */
export interface UsageItem {
  id: number;
  /** 32 位十六进制任务编号，界面与日志里的 task_id */
  task_ref: string;
  user: { id: number; username: string };
  kind: UsageKind;
  model_key: string;
  model_name: string;
  status: UsageStatus;
  /** 冻结积分 */
  credits: number;
  /** 实扣积分；还没结算为 null */
  charged_credits: number | null;
  error_code: string;
  /** 给用户看的失败文案 */
  error_message: string;
  /** 完成时间 - 创建时间；进行中为 null */
  duration_ms: number | null;
  is_test: boolean;
  created_at: string;
}

/** 摘要条：与列表同一组筛选 */
export interface UsageSummary {
  total: number;
  succeeded: number;
  failed: number;
  canceled: number;
  expired: number;
  active: number;
  /** 实扣积分合计 */
  charged_credits: number;
}

/** 任务产物 */
export interface UsageOutput {
  asset_id?: number;
  url?: string;
  media_type: string;
  text?: string;
  duration_ms?: number;
  width?: number;
  height?: number;
  cover_asset_id?: number;
}

/** 状态变化事件；老任务没有事件 */
export interface UsageEvent {
  id: number;
  event: "created" | "submitted" | "finalizing" | "succeeded" | "failed" | "canceled" | "expired";
  from_status: string;
  to_status: string;
  /** failed / expired / canceled 可能含 {class, code, upstream, reason}，submitted 可能含 {provider_task_id_set}；未知键原样显示 */
  detail: Record<string, unknown> | null;
  created_at: string;
}

/** 积分流水：amount 已带符号，freeze / settle 为负，refund 为正 */
export interface UsageLedger {
  id: number;
  type: "freeze" | "settle" | "refund";
  amount: number;
  created_at: string;
}

/** 任务详情（GET /admin/usage/:ref） */
export interface UsageDetail {
  task: {
    id: number;
    task_ref: string;
    kind: UsageKind;
    model_key: string;
    model_name: string;
    channel: string;
    plugin: string;
    canvas_id: string;
    node_id: string;
    status: UsageStatus;
    /** 0 到 100；平台没给为 null */
    progress: number | null;
    credits: number;
    charged_credits: number | null;
    error_code: string;
    error_message: string;
    is_test: boolean;
    created_at: string;
    submitted_at: string | null;
    finished_at: string | null;
    deadline_at: string;
  };
  user: { id: number; username: string; email: string };
  /** 规范化后的输入，含 prompt 等 */
  input: Record<string, unknown>;
  outputs: UsageOutput[];
  /** upstream 是上游错误原文（仅渠道 Key 被遮），没有为 null */
  error: { code: string; message: string; upstream: string | null };
  events: UsageEvent[];
  ledger: UsageLedger[];
  /** 仅试跑任务有：trace_json 的步骤数组，和试跑追踪面板吃的结构一致 */
  trace: unknown[] | null;
}
