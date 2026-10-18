import type { AuditItem, AuditModule, ListAuditParams } from "@/api/admin/logs/type";
import type {
  ListUsageParams,
  UsageQuery,
  UsageEvent,
  UsageKind,
  UsageLedger,
  UsageOutput,
  UsageStatus,
  UsageStatusFilter,
  UsageSummary,
} from "@/api/admin/usage/type";
import { AUDIT_ACTION_LABEL, formatAbsolute, formatDurationMs } from "@/utils/admin/user-rules";

/**
 * 使用记录与操作审计的纯逻辑：筛选条件 → 请求参数、状态与动作文案、审计摘要提炼、时间线整理。
 * 规则出处是 docs/design/日志管理/日志管理.md §6、§8.3；后端字段保持 snake_case，这里不改名。
 */

// ---------------------------------------------------------------- 时间范围

/** 时间范围档位 */
export type RangeKey = "today" | "24h" | "7d" | "30d" | "90d";

const HOUR = 3_600_000;
const SPAN: Record<Exclude<RangeKey, "today">, number> = {
  "24h": 24 * HOUR,
  "7d": 7 * 24 * HOUR,
  "30d": 30 * 24 * HOUR,
  "90d": 90 * 24 * HOUR,
};

/** 使用记录的时间范围下拉（默认近 7 天；后端最大跨度 90 天） */
export const USAGE_RANGES: ReadonlyArray<readonly [RangeKey, string]> = [
  ["today", "今天"],
  ["24h", "近 24 小时"],
  ["7d", "近 7 天"],
  ["30d", "近 30 天"],
];

/** 操作审计的时间范围下拉（默认近 30 天） */
export const AUDIT_RANGES: ReadonlyArray<readonly [RangeKey, string]> = [
  ...USAGE_RANGES,
  ["90d", "近 90 天"],
];

/**
 * 把档位换成请求用的起止时间
 * @param range 档位；「今天」从本地零点算起
 * @param now 当前时间（测试注入）
 * @returns RFC3339 的 from / to，to 是当前时刻
 */
export function rangeWindow(range: RangeKey, now: Date = new Date()): { from: string; to: string } {
  const from =
    range === "today"
      ? new Date(now.getFullYear(), now.getMonth(), now.getDate())
      : new Date(now.getTime() - SPAN[range]);
  return { from: from.toISOString(), to: now.toISOString() };
}

// ---------------------------------------------------------------- 使用记录：筛选与请求参数

/** 使用记录页的筛选状态 */
export type UsageFilters = {
  /** 搜索框：用户名 / 邮箱 / 32 位十六进制任务编号 */
  q: string;
  /** 用户筛选标签；null 表示不限 */
  userId: number | null;
  status: UsageStatusFilter;
  /** 空串表示全部类型 */
  kind: UsageKind | "";
  /** 模型 key，空串表示全部模型 */
  model: string;
  range: RangeKey;
  /** 含试跑任务 */
  includeTest: boolean;
};

export const DEFAULT_USAGE_FILTERS: UsageFilters = {
  q: "",
  userId: null,
  status: "all",
  kind: "",
  model: "",
  range: "7d",
  includeTest: false,
};

/**
 * 有没有生效的筛选（决定空结果时说“没有任务”还是“没有符合条件”）；时间范围不算，它有自己的“放宽”按钮
 * @param filters 筛选状态
 */
export function hasUsageFilters(filters: UsageFilters): boolean {
  return (
    filters.q.trim() !== "" ||
    filters.userId !== null ||
    filters.status !== "all" ||
    filters.kind !== "" ||
    filters.model !== "" ||
    filters.includeTest
  );
}

/** 状态筛选下拉 */
export const USAGE_STATUS_OPTIONS: ReadonlyArray<readonly [UsageStatusFilter, string]> = [
  ["all", "全部状态"],
  ["active", "进行中"],
  ["succeeded", "成功"],
  ["failed", "失败"],
  ["canceled", "已取消"],
  ["expired", "已超时"],
];

/** 类型筛选下拉 */
export const USAGE_KIND_OPTIONS: ReadonlyArray<readonly [UsageKind | "", string]> = [
  ["", "全部类型"],
  ["video", "视频"],
  ["image", "图片"],
  ["audio", "音频"],
  ["text", "文本"],
];

/** 32 位十六进制：任务编号 */
export const isTaskRef = (text: string) => /^[0-9a-f]{32}$/i.test(text.trim());

/** 使用记录页放进 URL 的筛选参数名（搜索词 q 与页码 page 由列表 hook 管） */
export const USAGE_FILTER_KEYS = [
  "user_id",
  "status",
  "kind",
  "model",
  "range",
  "include_test",
] as const;

const isRange = (value: string | undefined): value is RangeKey =>
  value !== undefined && value in SPAN_OR_TODAY;
const SPAN_OR_TODAY: Record<RangeKey, true> = {
  today: true,
  "24h": true,
  "7d": true,
  "30d": true,
  "90d": true,
};

/** 读 URL 里的 user_id：只认正整数 */
const parseUserId = (value: string | undefined): number | null => {
  const id = Number(value);
  return Number.isInteger(id) && id > 0 ? id : null;
};

/**
 * URL 查询参数 → 使用记录的筛选状态；缺省或非法的值回退默认（近 7 天、全部）
 * @param list 列表 hook 解析出的 q 与筛选参数
 */
export function usageFiltersOf(list: { q: string; filters: Record<string, string> }): UsageFilters {
  const f = list.filters;
  return {
    q: list.q,
    userId: parseUserId(f.user_id),
    status: USAGE_STATUS_OPTIONS.some(([v]) => v === f.status)
      ? (f.status as UsageStatusFilter)
      : "all",
    kind: USAGE_KIND_OPTIONS.some(([v]) => v === f.kind) ? (f.kind as UsageKind) : "",
    model: f.model ?? "",
    range: isRange(f.range) && f.range !== "90d" ? f.range : DEFAULT_USAGE_FILTERS.range,
    includeTest: f.include_test === "true",
  };
}

/**
 * 改一项筛选 → 写进 URL 的 [参数名, 值]；等于默认的写空串（从 URL 里删掉，链接保持简短）
 * @param patch 只含一项的改动
 */
export function usageFilterEntry(patch: Partial<UsageFilters>): [string, string] {
  if ("q" in patch) return ["q", patch.q ?? ""];
  if ("userId" in patch) return ["user_id", patch.userId ? String(patch.userId) : ""];
  if ("status" in patch) return ["status", patch.status === "all" ? "" : (patch.status ?? "")];
  if ("kind" in patch) return ["kind", patch.kind ?? ""];
  if ("model" in patch) return ["model", patch.model ?? ""];
  if ("range" in patch)
    return ["range", patch.range === DEFAULT_USAGE_FILTERS.range ? "" : (patch.range ?? "")];
  return ["include_test", patch.includeTest ? "true" : ""];
}

/** 列表 hook 交给 fetcher 的查询：搜索词、筛选与分页 */
type ListQuery = Record<string, string | number>;

/**
 * 把使用记录的筛选换成接口参数（列表与摘要共用）：range 换成 from/to，user_id 转数字，
 * include_test 只在开启时传。
 */
function usageFilterQuery(query: ListQuery, now: Date): UsageQuery {
  const filters = usageFiltersOf({
    q: String(query.q ?? ""),
    filters: Object.fromEntries(
      USAGE_FILTER_KEYS.filter((k) => query[k] !== undefined).map((k) => [k, String(query[k])]),
    ),
  });
  const params: UsageQuery = {};
  const q = filters.q.trim();
  if (q) params.q = q;
  if (filters.userId !== null) params.user_id = filters.userId;
  if (filters.status !== "all") params.status = filters.status;
  if (filters.kind) params.kind = filters.kind;
  if (filters.model) params.model = filters.model;
  if (filters.includeTest) params.include_test = "true";
  return { ...params, ...rangeWindow(filters.range, now) };
}

/**
 * 使用记录列表请求：列表 hook 的查询（q、筛选、page、page_size）→ GET /admin/usage 的参数
 * @param query 列表 hook 给出的查询参数
 * @param now 当前时间（测试注入），决定近 N 天的起点
 */
export function usageRequest(query: ListQuery, now: Date = new Date()): ListUsageParams {
  return {
    ...usageFilterQuery(query, now),
    page: Number(query.page) || 1,
    page_size: Number(query.page_size) || 20,
  };
}

/**
 * 摘要条请求：与列表同一组筛选，不带分页
 * @param list 列表 hook 解析出的 q 与筛选参数
 * @param now 当前时间（测试注入）
 */
export function usageSummaryRequest(
  list: { q: string; filters: Record<string, string> },
  now: Date = new Date(),
): UsageQuery {
  return usageFilterQuery({ q: list.q, ...list.filters }, now);
}

// ---------------------------------------------------------------- 使用记录：状态与展示

/** 任务状态的展示 */
export type UsageStatusView = {
  label: string;
  tone: "success" | "danger" | "info" | "neutral" | "warning";
  /** 进行中：状态点脉动、耗时实时增长、详情自动刷新 */
  active: boolean;
};

const STATUS_VIEW: Record<UsageStatus, UsageStatusView> = {
  pending: { label: "排队中", tone: "info", active: true },
  queued: { label: "平台排队", tone: "info", active: true },
  running: { label: "生成中", tone: "info", active: true },
  finalizing: { label: "转存中", tone: "info", active: true },
  succeeded: { label: "成功", tone: "success", active: false },
  failed: { label: "失败", tone: "danger", active: false },
  canceled: { label: "已取消", tone: "neutral", active: false },
  expired: { label: "已超时", tone: "warning", active: false },
};

/** 任务状态文案与语气；未知状态原样显示 */
export const usageStatusView = (status: string): UsageStatusView =>
  STATUS_VIEW[status as UsageStatus] ?? { label: status, tone: "neutral", active: false };

/** 是不是进行中的状态 */
export const isActiveStatus = (status: string) => usageStatusView(status).active;

/** 进行中任务详情的刷新间隔 */
export const TASK_POLL_MS = 5000;

/**
 * 详情要不要继续刷新：进行中，且页面可见（切到后台就暂停）
 * @param status 任务状态
 * @param visible 页面是否可见
 */
export const shouldPoll = (status: string, visible: boolean) => visible && isActiveStatus(status);

/**
 * 两个时间点相隔的毫秒数；任一缺失或解析失败为 null
 * @param from 起点（RFC3339）
 * @param to 终点（RFC3339）
 */
export function spanMs(from: string | null | undefined, to: string | null | undefined) {
  if (!from || !to) return null;
  const a = Date.parse(from);
  const b = Date.parse(to);
  return Number.isNaN(a) || Number.isNaN(b) ? null : Math.max(0, b - a);
}

/**
 * 失败 / 超时 / 取消的任务没有上游错误原文时，抽屉里给什么提示。
 * 老任务（没有状态事件）是日志功能上线前创建的；新任务按状态说明原因。
 * @returns 提示文案；不需要提示（成功、进行中、已有原文）为 null
 */
export function failureHint(input: {
  status: string;
  upstream: string | null;
  hasEvents: boolean;
}): string | null {
  if (!["failed", "expired", "canceled"].includes(input.status)) return null;
  if (input.upstream) return null;
  if (!input.hasEvents) return "此任务创建于日志功能上线前，没有上游错误记录。";
  if (input.status === "expired") return "这类失败（排队超时）没有上游错误。";
  if (input.status === "canceled") return "这类失败（用户取消）没有上游错误。";
  return "这次失败没有记录到上游错误原文。";
}

/** 后端 ErrTaskNotFound 的业务码 */
const TASK_NOT_FOUND_CODE = 40004;

/**
 * 是不是「找不到这个任务」：HTTP 404 或业务码 40004。抽屉据此显示就地提示，而不是“加载失败”
 * @param error 请求抛出的错误
 */
export function isTaskNotFound(error: unknown): boolean {
  if (typeof error !== "object" || error === null) return false;
  const e = error as { code?: unknown; status?: unknown };
  return e.status === 404 || e.code === TASK_NOT_FOUND_CODE;
}

/**
 * 秒级时间「10-18 14:05:09」，抽屉里的时间线和基础信息用
 * @param value RFC3339；空返回「—」，解析失败原样返回
 */
export function formatDateTimeSec(value: string | null | undefined): string {
  if (!value) return "—";
  const time = Date.parse(value);
  if (Number.isNaN(time)) return value;
  const d = new Date(time);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
}

/**
 * 产物的一句话说明：尺寸、时长、文本字数；都没有为空串
 * @param output 详情里的一个产物
 */
export function outputCaption(output: UsageOutput): string {
  const parts: string[] = [];
  if (output.width && output.height) parts.push(`${output.width}×${output.height}`);
  if (output.duration_ms) parts.push(formatDurationMs(output.duration_ms));
  if (output.text) parts.push(`${[...output.text].length} 字`);
  return parts.join(" · ");
}

/** 产物的渲染方式：图片 / 视频 / 音频 / 文本 / 其它文件，以及有没有可访问地址 */
export type OutputRender = {
  kind: "image" | "video" | "audio" | "text" | "file";
  hasUrl: boolean;
};

/**
 * 判断一个产物该怎么渲染。media_type 不分大小写，带 mime 前缀（video/mp4）只看斜杠前；
 * 类型认不出但带了正文的当文本；其余都当普通文件。没有 url（素材已被清理）照样识别类型，
 * 只是 hasUrl 为 false，界面显示「没有可访问地址」。
 * @param output 详情里的一个产物
 */
export function outputRender(output: UsageOutput): OutputRender {
  const head = (output.media_type ?? "").trim().toLowerCase().split("/")[0];
  const hasUrl = !!output.url;
  if (head === "image" || head === "video" || head === "audio" || head === "text") {
    return { kind: head, hasUrl };
  }
  return { kind: output.text ? "text" : "file", hasUrl };
}

/** 老任务（没有事件）的时间线提示 */
export const NO_TIMELINE_TEXT = "无记录：此任务创建于日志功能上线前，没有状态变化记录。";

// ---------------------------------------------------------------- 使用记录：时间线与流水

/** 时间线事件的人话 */
export const EVENT_TEXT: Record<string, string> = {
  created: "提交了任务",
  submitted: "平台开始处理",
  finalizing: "生成完成，正在保存结果",
  succeeded: "完成",
  failed: "失败了",
  canceled: "被取消",
  expired: "等太久，已超时",
};

const EVENT_VIEW: Record<string, { label: string; tone: UsageStatusView["tone"] }> = {
  created: { label: EVENT_TEXT.created, tone: "neutral" },
  submitted: { label: EVENT_TEXT.submitted, tone: "info" },
  finalizing: { label: EVENT_TEXT.finalizing, tone: "info" },
  succeeded: { label: EVENT_TEXT.succeeded, tone: "success" },
  failed: { label: EVENT_TEXT.failed, tone: "danger" },
  canceled: { label: EVENT_TEXT.canceled, tone: "neutral" },
  expired: { label: EVENT_TEXT.expired, tone: "warning" },
};

const REASON_LABEL: Record<string, string> = {
  user_cancel: "用户取消",
  admin_cancel: "管理员取消",
  deadline: "超过截止时间",
};

/** 事件 detail 里不在备注里展示的键：upstream 太长，单独在失败原因里看 */
const NOTE_SKIP = new Set(["upstream"]);

/**
 * 事件备注：已知键提炼成短句，未知键以 key=value 原样显示（后端以后加键也不丢信息）
 * @param detail 事件的 detail，可能为 null
 */
export function eventNote(detail: Record<string, unknown> | null | undefined): string {
  if (!detail) return "";
  const parts: string[] = [];
  for (const [key, value] of Object.entries(detail)) {
    if (NOTE_SKIP.has(key)) continue;
    if (key === "provider_task_id_set") {
      if (value === true) parts.push("已记录上游任务号");
    } else if (key === "reason" && typeof value === "string") {
      parts.push(REASON_LABEL[value] ?? `reason=${value}`);
    } else {
      parts.push(`${key}=${showValue(value)}`);
    }
  }
  return parts.join(" · ");
}

/** 时间线的一行 */
export type TimelineRow = {
  id: number;
  label: string;
  tone: UsageStatusView["tone"];
  /** 「排队中 → 生成中」；创建事件只有目标状态 */
  transition: string;
  note: string;
  createdAt: string;
  /** 时:分:秒 */
  clock: string;
  /** 距上一步的毫秒数；第一行为 null */
  deltaMs: number | null;
};

/**
 * 状态事件 → 时间线行：按事件 id 升序（即发生顺序），带距上一步的耗时
 * @param events 详情里的 events，老任务为空数组
 */
export function timelineRows(events: UsageEvent[]): TimelineRow[] {
  const sorted = [...events].sort((a, b) => a.id - b.id);
  return sorted.map((event, index) => {
    const view = EVENT_VIEW[event.event] ?? { label: event.event, tone: "neutral" as const };
    const to = usageStatusView(event.to_status).label;
    return {
      id: event.id,
      label: view.label,
      tone: view.tone,
      transition: event.from_status ? `${usageStatusView(event.from_status).label} → ${to}` : to,
      note: eventNote(event.detail),
      createdAt: event.created_at,
      clock: formatClock(event.created_at),
      deltaMs: index === 0 ? null : spanMs(sorted[index - 1].created_at, event.created_at),
    };
  });
}

const LEDGER_LABEL: Record<string, string> = { freeze: "冻结", settle: "结算", refund: "退款" };

/** 积分流水行：类型中文名，金额带正负号 */
export function ledgerRows(ledger: UsageLedger[]) {
  return ledger.map((row) => ({
    id: row.id,
    label: LEDGER_LABEL[row.type] ?? row.type,
    amount: row.amount,
    amountText: row.amount > 0 ? `+${row.amount}` : String(row.amount),
    createdAt: row.created_at,
  }));
}

// ---------------------------------------------------------------- 从用户管理进入

/**
 * 读路由 state 里的用户筛选：用户管理的「查看全部使用记录」带 `{ userId }` 过来
 * @param state react-router 的 location.state
 * @returns 用户 id；没有或不合法为 null
 */
export function readUserFilterState(state: unknown): number | null {
  if (typeof state !== "object" || state === null) return null;
  const id = (state as { userId?: unknown }).userId;
  return typeof id === "number" && Number.isInteger(id) && id > 0 ? id : null;
}

// ---------------------------------------------------------------- 操作审计

/** 操作审计页的筛选状态 */
export type AuditFilters = {
  /** 操作人：用户名或数字 id */
  actor: string;
  /** 空串表示全部模块 */
  module: AuditModule | "";
  range: RangeKey;
};

export const DEFAULT_AUDIT_FILTERS: AuditFilters = { actor: "", module: "", range: "30d" };

/** 模块下拉：取值是动作名的前缀，与后端 module 参数一致 */
export const AUDIT_MODULES: ReadonlyArray<readonly [AuditModule | "", string]> = [
  ["", "全部模块"],
  ["user", "用户"],
  ["settings", "系统设置"],
  ["showcase", "登录页展示"],
  ["system", "系统升级"],
  ["payment", "支付"],
  ["channel", "渠道"],
  ["plugin", "插件"],
  ["model", "模型"],
  ["storage", "存储"],
];

/** 操作审计页放进 URL 的筛选参数名（操作人用搜索词 q；action 暂无界面，保留给链接直达） */
export const AUDIT_FILTER_KEYS = ["module", "action", "range"] as const;

/**
 * URL 查询参数 → 操作审计的筛选状态；缺省或非法的值回退默认（近 30 天、全部模块）
 * @param list 列表 hook 解析出的 q（操作人）与筛选参数
 */
export function auditFiltersOf(list: { q: string; filters: Record<string, string> }): AuditFilters {
  const f = list.filters;
  return {
    actor: list.q,
    module: AUDIT_MODULES.some(([v]) => v === f.module) ? (f.module as AuditModule) : "",
    range: isRange(f.range) ? f.range : DEFAULT_AUDIT_FILTERS.range,
  };
}

/**
 * 操作审计列表请求：列表 hook 的查询 → GET /admin/logs/audits 的参数；q 是操作人，range 换成 from/to
 * @param query 列表 hook 给出的查询参数
 * @param now 当前时间（测试注入）
 */
export function auditRequest(query: ListQuery, now: Date = new Date()): ListAuditParams {
  const filters = auditFiltersOf({
    q: String(query.q ?? ""),
    filters: Object.fromEntries(
      AUDIT_FILTER_KEYS.filter((k) => query[k] !== undefined).map((k) => [k, String(query[k])]),
    ),
  });
  const params: ListAuditParams = {};
  const actor = filters.actor.trim();
  if (actor) params.actor = actor;
  if (filters.module) params.module = filters.module;
  if (typeof query.action === "string" && query.action) params.action = query.action;
  return {
    ...params,
    ...rangeWindow(filters.range, now),
    page: Number(query.page) || 1,
    page_size: Number(query.page_size) || 20,
  };
}

/**
 * 全部审计动作的中文名：来自 backend/internal/model/admin_audit.go（AdminAudit*）与
 * ai_audit.go（Audit*）。用户类动作沿用用户管理里已有的那份，不重复定义。
 */
const ACTION_LABEL: Record<string, string> = {
  ...AUDIT_ACTION_LABEL,
  "settings.register": "修改了注册设置",
  "settings.smtp": "修改了邮件服务设置",
  "showcase.create": "添加了登录页作品",
  "showcase.update": "修改了登录页作品",
  "showcase.delete": "删除了登录页作品",
  "showcase.order": "调整了登录页作品顺序",
  "showcase.settings": "修改了登录页展示设置",
  "system.upgrade": "升级了系统",
  "system.rollback": "回退了系统版本",
  "payment.settings_update": "修改了支付设置",
  "payment.channel_update": "修改了支付渠道",
  "payment.channel_enable": "启用了支付渠道",
  "payment.channel_disable": "停用了支付渠道",
  "payment.product_create": "新建了充值商品",
  "payment.product_update": "修改了充值商品",
  "payment.product_delete": "删除了充值商品",
  "payment.product_sort": "调整了充值商品顺序",
  "payment.order_query": "查询了订单",
  "payment.order_close": "关闭了订单",
  "plugin.upload": "上传了插件版本",
  "plugin.enable": "启用 / 停用了插件",
  "plugin.delete": "删除了插件",
  "plugin.upgrade_models": "批量升级了模型的插件版本",
  "channel.create": "新建了渠道",
  "channel.update": "修改了渠道",
  "channel.secret": "设置了渠道 Key",
  "channel.secret_key": "设置了渠道 Secret Key",
  "channel.sort": "调整了渠道顺序",
  "channel.enable": "启用 / 停用了渠道",
  "channel.auto_off": "自动停用了渠道",
  "channel.delete": "删除了渠道",
  // 历史动作：对应的开关已取消，不再产生新记录，旧审计里还在
  "channel.trusted": "修改了渠道的内网访问开关",
  "channel.credential": "修改了渠道的凭证开关",
  "model.import": "导入了模型",
  "model.protocol": "修改了模型协议",
  "storage.create": "新建了存储",
  "storage.update": "修改了存储配置",
  "storage.secret": "替换了存储密钥",
  "storage.default": "设置了默认存储",
  "storage.delete": "删除了存储",
};

/** 已取消、不再产生新记录的动作 */
const LEGACY_ACTIONS = new Set(["channel.trusted", "channel.credential"]);

/**
 * 动作的展示：中文名与是否历史动作；不认识的动作原样显示 action
 * @param action 审计的 action
 */
export function actionView(action: string): { label: string; legacy: boolean; known: boolean } {
  const label = ACTION_LABEL[action];
  if (label === undefined) return { label: action, legacy: false, known: false };
  return { label, legacy: LEGACY_ACTIONS.has(action), known: true };
}

/**
 * 操作人的展示：actor_id=0 是系统自动操作；名字缺失时用 #id
 * @param actorId 操作人 id
 * @param actorName 操作人用户名
 */
export function auditActorView(actorId: number, actorName: string) {
  if (actorId === 0) return { name: "系统", auto: true };
  return { name: actorName || `#${actorId}`, auto: false };
}

const TARGET_TYPE_LABEL: Record<string, string> = {
  user: "用户",
  settings: "设置",
  showcase: "登录页展示",
  system: "系统",
  payment: "支付",
  channel: "渠道",
  plugin: "插件",
  plugin_version: "插件版本",
  model: "模型",
  storage: "存储",
};

/**
 * 对象的展示：类型中文名 + 名称（能解析的名称优先，其次编号）
 * @param item 审计里的 target_type / target_ref / target_name
 */
export function auditTargetView(
  item: Pick<AuditItem, "target_type" | "target_ref" | "target_name">,
) {
  return {
    type: TARGET_TYPE_LABEL[item.target_type] ?? item.target_type,
    name: item.target_name || item.target_ref || "—",
  };
}

/** detail 可能是对象，也可能是 JSON 字符串（历史数据） */
function parseDetail(detail: unknown): Record<string, unknown> | null {
  let value = detail;
  if (typeof detail === "string") {
    try {
      value = JSON.parse(detail);
    } catch {
      return null;
    }
  }
  return typeof value === "object" && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

/** 值转成一行文字：字符串原样，其余 JSON */
function showValue(value: unknown): string {
  return typeof value === "string" ? value : JSON.stringify(value);
}

const signed = (n: number) => (n > 0 ? `+${n}` : String(n));

/**
 * 从动作与 detail 提炼一句话摘要；提炼不出时退回 key=value 拼接，detail 为空显示「—」
 * @param action 审计的 action
 * @param detail 审计的 detail_json
 */
export function auditSummary(action: string, detail: unknown): string {
  const d = parseDetail(detail);
  if (!d || Object.keys(d).length === 0) return "—";
  const from = d.from;
  const to = d.to;
  if (action === "user.credit_adjust" && typeof d.delta === "number") {
    const note = typeof d.note === "string" && d.note ? `，备注：${d.note}` : "";
    return `积分 ${signed(d.delta)}${note}`;
  }
  if (action === "user.limit" && "to" in d) {
    const limit = (value: unknown) =>
      value === null || value === undefined ? "全局默认" : showValue(value);
    return `并发上限 ${limit(from)} → ${limit(to)}`;
  }
  if (action === "user.role" && from !== undefined && to !== undefined) {
    return `角色 ${showValue(from)} → ${showValue(to)}`;
  }
  if ((action === "system.upgrade" || action === "system.rollback") && from !== undefined) {
    return `${showValue(from)} → ${showValue(to)}`;
  }
  if (action.endsWith(".enable") && typeof from === "boolean" && typeof to === "boolean") {
    return `${from ? "已启用" : "已停用"} → ${to ? "已启用" : "已停用"}`;
  }
  if (action === "plugin.upload" && typeof d.version === "string") return `版本 ${d.version}`;
  return Object.entries(d)
    .map(([key, value]) => `${key}=${showValue(value)}`)
    .join("，");
}

/**
 * 抽屉里 detail 的键值展示：对象值展开成「键：值，键：值」，数组按 JSON，空值破折号
 * @param detail 审计的 detail_json（对象或 JSON 字符串）
 */
export function detailEntries(detail: unknown): [string, string][] {
  const d = parseDetail(detail);
  if (!d) return [];
  return Object.entries(d).map(([key, value]) => {
    if (value === null || value === undefined) return [key, "—"];
    if (typeof value === "object" && !Array.isArray(value)) {
      return [
        key,
        Object.entries(value as Record<string, unknown>)
          .map(([k, v]) => `${k}：${showValue(v)}`)
          .join("，"),
      ];
    }
    return [key, showValue(value)];
  });
}

export { parseDetail, showValue };

// ---------------------------------------------------------------- 人话文案（界面重做）

/** 时:分:秒；空值或解析失败返回「—」 */
export function formatClock(value: string | null | undefined): string {
  if (!value) return "—";
  const time = Date.parse(value);
  if (Number.isNaN(time)) return "—";
  const d = new Date(time);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
}

/**
 * 时长人话：不足 1 分钟「42 秒」，不足 1 小时「3 分 20 秒」/「3 分钟」，更长「1 小时 5 分」
 * @param ms 毫秒；null 返回空串
 */
export function humanDuration(ms: number | null | undefined): string {
  if (ms === null || ms === undefined) return "";
  const sec = Math.round(ms / 1000);
  if (sec < 60) return `${Math.max(1, sec)} 秒`;
  if (sec < 3600) {
    const m = Math.floor(sec / 60);
    const s = sec % 60;
    return s ? `${m} 分 ${s} 秒` : `${m} 分钟`;
  }
  const h = Math.floor(sec / 3600);
  const m = Math.round((sec % 3600) / 60);
  return m ? `${h} 小时 ${m} 分` : `${h} 小时`;
}

/** 卡片与结论卡需要的任务字段（列表项与详情里的 task 都能凑出来） */
export type TaskLike = {
  kind: string;
  status: string;
  credits: number;
  charged_credits: number | null;
  is_test: boolean;
  error_message: string;
  model_key: string;
  model_name: string;
  /** 完成时间 - 创建时间；进行中为 null */
  duration_ms: number | null;
  created_at: string;
};

const KIND_NAME: Record<string, string> = {
  video: "视频",
  image: "图片",
  audio: "音频",
  text: "文本",
};

/** 类型中文名；未知类型原样 */
export const kindName = (kind: string) => KIND_NAME[kind] ?? kind;

const FAILED_LIKE = ["failed", "expired", "canceled"];

/**
 * 卡片第一行里用户名后面的那句话：「生成了一个视频」「生成图片失败了」…
 * @param item 只用 kind 与 status
 */
export function taskHeadline(item: Pick<TaskLike, "kind" | "status">): string {
  const k = kindName(item.kind);
  switch (item.status) {
    case "succeeded":
      return `生成了一个${k}`;
    case "failed":
      return `生成${k}失败了`;
    case "expired":
      return `生成${k}超时了`;
    case "canceled":
      return `取消了${k}生成`;
    case "pending":
    case "queued":
      return `正在排队生成${k}`;
    default:
      return `正在生成${k}`;
  }
}

/** 积分一句话：试跑不扣；进行中已冻结；成功用了多少；其余已退回 */
export function creditSentence(
  item: Pick<TaskLike, "status" | "credits" | "charged_credits" | "is_test">,
): string {
  if (item.is_test) return "不扣积分";
  if (isActiveStatus(item.status)) return `已冻结 ${item.credits} 积分`;
  if (item.status === "succeeded") return `用了 ${item.charged_credits ?? item.credits} 积分`;
  return "积分已退回";
}

/** 抽屉里的积分说明：冻结多少、最后扣多少、退回多少 */
export function creditPlain(
  item: Pick<TaskLike, "status" | "credits" | "charged_credits" | "is_test">,
): string {
  if (item.is_test) return "试跑任务不扣积分。";
  if (isActiveStatus(item.status)) return `已冻结 ${item.credits} 积分，完成后按实际扣。`;
  if (item.status === "succeeded") {
    const charged = item.charged_credits ?? item.credits;
    const back = item.credits - charged;
    return `冻结了 ${item.credits}，最后扣了 ${charged}${back > 0 ? `，多的 ${back} 已退回` : ""}。`;
  }
  return `冻结了 ${item.credits}，已全部退回，没有扣费。`;
}

/** 进行中任务已经过去的毫秒数（以传入的当前时间为准） */
const elapsed = (item: Pick<TaskLike, "created_at">, now: number) =>
  Math.max(0, now - Date.parse(item.created_at));

/**
 * 卡片第二行：模型展示名 · 积分一句话 · 用时 / 原因 / 已等多久
 * @param now 当前时间戳（毫秒）；进行中的「已 N」据此算，每秒刷新由界面负责
 */
export function taskSubline(item: TaskLike, now: number): string {
  const parts = [item.model_name || item.model_key];
  if (item.status === "succeeded") {
    parts.push(creditSentence(item));
    if (item.duration_ms !== null) parts.push(`用时 ${humanDuration(item.duration_ms)}`);
  } else if (FAILED_LIKE.includes(item.status)) {
    const credit = creditSentence(item);
    if (item.status === "failed" && item.error_message) {
      parts.push(item.error_message);
      if (!item.error_message.includes(credit)) parts.push(credit);
    } else {
      if (item.status === "expired") parts.push("等太久没出结果");
      parts.push(credit);
    }
  } else {
    const waited = humanDuration(elapsed(item, now));
    parts.push(
      item.status === "pending" || item.status === "queued"
        ? `排队中 · 已等 ${waited}`
        : `生成中 · 已 ${waited}`,
    );
  }
  return parts.join(" · ");
}

/** 抽屉顶部的结论卡 */
export type TaskVerdict = {
  tone: UsageStatusView["tone"];
  title: string;
  sub: string;
  /** 进行中：状态点脉动 */
  pulse: boolean;
};

/**
 * 结论卡：状态点颜色、一句结论、用时 / 原因 / 积分
 * @param now 当前时间戳（毫秒）
 */
export function taskVerdict(item: TaskLike, now: number): TaskVerdict {
  switch (item.status) {
    case "succeeded":
      return {
        tone: "success",
        title: "生成成功",
        sub: [
          item.duration_ms !== null ? `用时 ${humanDuration(item.duration_ms)}` : "",
          creditSentence(item),
        ]
          .filter(Boolean)
          .join(" · "),
        pulse: false,
      };
    case "failed":
      return {
        tone: "danger",
        title: "生成失败",
        sub: item.error_message || "积分已退回",
        pulse: false,
      };
    case "expired":
      return { tone: "warning", title: "等太久，已超时", sub: "积分已退回", pulse: false };
    case "canceled":
      return { tone: "neutral", title: "已取消", sub: "积分已退回", pulse: false };
    default: {
      const queued = item.status === "pending" || item.status === "queued";
      return {
        tone: "info",
        title: queued ? "排队中" : "正在生成",
        sub: `已 ${humanDuration(elapsed(item, now))} · ${creditSentence(item)}`,
        pulse: true,
      };
    }
  }
}

/**
 * 失败 / 超时 / 取消时给运营的「怎么办」；成功和进行中为 null，未知错误码给通用文案
 * @param code 统一错误码
 * @param status 任务状态
 */
export function failureAdvice(
  code: string,
  status: string,
): { title: string; text: string } | null {
  if (status === "canceled") {
    return { title: "已取消", text: "用户或管理员取消了这个任务，积分已退回，无需处理。" };
  }
  if (status === "expired" || (status === "failed" && code === "timeout")) {
    return {
      title: "等太久，已超时",
      text: "任务在规定时间内没出结果，积分已自动退回，可以让用户重试。",
    };
  }
  if (status !== "failed") return null;
  switch (code) {
    case "moderation":
      return { title: "不是系统问题", text: "内容没通过审核，让用户改改提示词再试即可。" };
    case "invalid_param":
      return { title: "不是系统问题", text: "用户填的参数不合适，让用户调整后重试。" };
    case "provider_balance":
      return {
        title: "需要运维处理",
        text: "上游账户余额不足，请联系运维充值；用户的积分已退回。",
      };
    case "submit_unknown":
      return { title: "建议核对", text: "提交结果不确定，上游可能已经扣费，建议让运维核对一下。" };
    default:
      return {
        title: "可以先让用户重试",
        text: "平台暂时出了问题，积分已退回；如果反复出现，请找运维（切到「详细」看原因）。",
      };
  }
}

// ---------------------------------------------------------------- 抽屉：简单 / 详细

export type DrawerMode = "simple" | "detail";

/** 抽屉里各区块显不显示 */
export type DrawerSections = Record<
  | "verdict"
  | "advice"
  | "prompt"
  | "outputs"
  | "creditSentence"
  | "creditLedger"
  | "timeline"
  | "params"
  | "failureDetail"
  | "taskInfo"
  | "trace",
  boolean
>;

/**
 * 使用记录抽屉各区块显不显示。抽屉不再分「简单 / 详细」（用户评审时取消了切换），
 * 区块一律全显示，只有试跑追踪要看这个任务有没有追踪。
 */
export function drawerSections(opts: { hasTrace: boolean }): DrawerSections {
  return {
    verdict: true,
    advice: true,
    prompt: true,
    outputs: true,
    creditSentence: true,
    creditLedger: true,
    timeline: true,
    params: true,
    failureDetail: true,
    taskInfo: true,
    trace: opts.hasTrace,
  };
}

// ---------------------------------------------------------------- 概览卡与筛选条

/**
 * 摘要接口 → 概览卡数据。需要关注 = 失败 + 超时 + 取消；
 * 成功率 = 成功 ÷ (全部 − 进行中)，最多一位小数，分母为 0 时为 0%。
 */
export function overviewCards(summary: UsageSummary) {
  const done = summary.total - summary.active;
  const rate = done > 0 ? Math.round((summary.succeeded / done) * 1000) / 10 : 0;
  return {
    total: summary.total,
    succeeded: summary.succeeded,
    attention: {
      count: summary.failed + summary.expired + summary.canceled,
      failed: summary.failed,
      expired: summary.expired,
      canceled: summary.canceled,
    },
    active: summary.active,
    successRate: `${rate}%`,
  };
}

const SEGMENT_STATUSES: UsageStatusFilter[] = ["all", "succeeded", "failed", "active"];

/** 状态分段控件的选中项；取消、超时只能从概览卡进入，此时没有选中项 */
export const segmentValue = (status: UsageStatusFilter): UsageStatusFilter | null =>
  SEGMENT_STATUSES.includes(status) ? status : null;

/** 「更多筛选」里已选了几项（类型、模型、含试跑），给按钮角标 */
export const moreFilterCount = (filters: UsageFilters): number =>
  (filters.kind ? 1 : 0) + (filters.model ? 1 : 0) + (filters.includeTest ? 1 : 0);

// ---------------------------------------------------------------- 操作审计分类

/** 动作所属分类（动作名前缀）；不认识的归到 other，界面给默认图标 */
export function auditCategory(action: string): AuditModule | "other" {
  const head = action.split(".")[0];
  return AUDIT_MODULES.some(([value]) => value && value === head) ? (head as AuditModule) : "other";
}

/**
 * 卡片右侧的相对时间：刚刚 / N 分钟前 / N 小时前 / N 天前，超过 30 天写绝对时间
 * @param value RFC3339
 * @param now 当前时间戳（毫秒，测试注入）
 */
export function agoText(value: string | null | undefined, now: number = Date.now()): string {
  if (!value) return "—";
  const time = Date.parse(value);
  if (Number.isNaN(time)) return value;
  const minutes = Math.max(0, (now - time) / 60_000);
  if (minutes < 1) return "刚刚";
  if (minutes < 60) return `${Math.floor(minutes)} 分钟前`;
  if (minutes < 1440) return `${Math.floor(minutes / 60)} 小时前`;
  if (minutes < 43_200) return `${Math.floor(minutes / 1440)} 天前`;
  return formatAbsolute(value);
}

// ---------------------------------------------------------------- 操作审计：表格与抽屉

/** 操作者第二行：我们没有角色字段，只区分后台管理员与系统自动 */
export const auditActorRole = (actorId: number) => (actorId === 0 ? "系统自动" : "后台管理员");

/** 自动（系统触发）/ 手动（管理员操作） */
export const auditModeLabel = (actorId: number) => (actorId === 0 ? "自动" : "手动");

/** 审计来源对应的表名 */
export const auditSourceTable = (source: AuditItem["source"]) =>
  source === "ai" ? "ai_audit_logs" : "admin_audit_logs";

/**
 * 完整时间「2026/10/18 14:05:09」
 * @param value RFC3339；空返回「—」，解析失败原样返回
 */
export function formatFullTime(value: string | null | undefined): string {
  if (!value) return "—";
  const time = Date.parse(value);
  if (Number.isNaN(time)) return value;
  const d = new Date(time);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}/${p(d.getMonth() + 1)}/${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
}

// ---------------------------------------------------------------- 使用记录表格单元格

/** 类型小标签：文字、Tag 语气、图标键（组件按键选 lucide 图标） */
export function kindTagView(kind: string): {
  label: string;
  tone: "info" | "violet" | "orange" | "neutral";
  icon: "video" | "image" | "audio" | "text" | "other";
} {
  switch (kind) {
    case "video":
      return { label: "视频", tone: "info", icon: "video" };
    case "image":
      return { label: "图片", tone: "violet", icon: "image" };
    case "audio":
      return { label: "音频", tone: "orange", icon: "audio" };
    case "text":
      return { label: "文本", tone: "neutral", icon: "text" };
    default:
      return { label: kind, tone: "neutral", icon: "other" };
  }
}

/** 状态格第二行：失败写用户文案，超时写原因，其余留空 */
export function statusDetail(item: Pick<TaskLike, "status" | "error_message">): string {
  if (item.status === "failed") return item.error_message;
  if (item.status === "expired") return "等太久没出结果";
  return "";
}

/** 积分格：成功是绿色数字（实扣），失败类「已退回」，进行中「已冻结 N」，试跑「不扣积分」 */
export function creditCell(
  item: Pick<TaskLike, "status" | "credits" | "charged_credits" | "is_test">,
): { text: string; tone: "success" | "muted" | "plain" } {
  if (item.is_test) return { text: "不扣积分", tone: "muted" };
  if (isActiveStatus(item.status)) return { text: `已冻结 ${item.credits}`, tone: "plain" };
  if (item.status === "succeeded") {
    return { text: String(item.charged_credits ?? item.credits), tone: "success" };
  }
  return { text: "已退回", tone: "muted" };
}

/**
 * 耗时格：完成的写时长，进行中写「已 N」（随传入的当前时间增长），没有为「—」
 * @param now 当前时间戳（毫秒）
 */
export function durationCell(
  item: Pick<TaskLike, "status" | "duration_ms" | "created_at">,
  now: number,
): string {
  if (isActiveStatus(item.status)) return `已 ${humanDuration(elapsed(item, now))}`;
  return item.duration_ms === null ? "—" : humanDuration(item.duration_ms);
}
