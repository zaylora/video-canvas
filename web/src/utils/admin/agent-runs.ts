import type {
  AgentRunItem,
  AgentRunSource,
  AgentRunStatus,
  ListAgentRunsParams,
} from "@/api/admin/agent-runs/type";
import { AGENT_MODES } from "@/constants/agent";
import { USAGE_RANGES, rangeWindow, type RangeKey } from "@/utils/admin/log-rules";
import { formatDurationMs } from "@/utils/admin/user-rules";
import { ApiError } from "@/utils/requests/request";

/**
 * 「日志管理 → Agent 运行」的纯逻辑：筛选 → 请求参数、状态与下载状态判定、提示文案、zip 响应校验。
 * 规则出处是设计文档 docs/design/Agent调优Harness/Agent调优Harness.md 的 6.B.2「后台页面」、第 7 节、8.4 接口契约。
 */

// ---------------------------------------------------------------- 筛选

/** 时间范围下拉：与使用记录一致；默认最近 7 天（轨迹只保留 14 天，更早的行会显示「已过期」） */
export const AGENT_RUN_RANGES = USAGE_RANGES;
export const DEFAULT_AGENT_RUN_RANGE: RangeKey = "7d";

/** 放进 URL 的筛选参数名（用户搜索词 q 与页码 page 由列表 hook 管） */
export const AGENT_RUN_FILTER_KEYS = ["canvas_id", "run_id", "status", "source", "range"] as const;

/** 运行状态下拉，值以后端 model/agent.go 的 Run 状态常量为准；第一项是不限 */
export const AGENT_RUN_STATUS_OPTIONS: ReadonlyArray<readonly [AgentRunStatus | "", string]> = [
  ["", "全部状态"],
  ["queued", "排队中"],
  ["running", "运行中"],
  ["waiting_approval", "等待批准"],
  ["waiting_input", "等待回答"],
  ["succeeded", "完成"],
  ["failed", "失败"],
  ["canceled", "已停止"],
  ["budget_exhausted", "预算用尽"],
  ["step_limit", "步数上限"],
  ["timeout", "超时"],
  ["interrupted", "中断"],
  ["expired", "等待超时"],
];

/** 来源下拉：真实用户 / harness（调优命令行跑的用例） */
export const AGENT_RUN_SOURCE_OPTIONS: ReadonlyArray<readonly [AgentRunSource | "", string]> = [
  ["", "全部来源"],
  ["user", "真实用户"],
  ["harness", "harness"],
];

/** 空结果的提示（标题 + 说明），整句出自设计文档第 7 节 */
export const AGENT_RUN_EMPTY_TITLE = "没有符合条件的运行";
export const AGENT_RUN_EMPTY_HINT = "换个筛选条件，或者看看是不是已超过 14 天";
export const AGENT_RUN_EMPTY_TEXT = `${AGENT_RUN_EMPTY_TITLE}，${AGENT_RUN_EMPTY_HINT}`;

/** Agent 运行页的筛选状态 */
export type AgentRunFilters = {
  /** 用户邮箱（包含匹配）或用户 id */
  user: string;
  canvasId: string;
  runId: string;
  status: AgentRunStatus | "";
  source: AgentRunSource | "";
  range: RangeKey;
};

const inOptions = <T extends string>(
  options: ReadonlyArray<readonly [T, string]>,
  value: string | undefined,
): value is T => options.some(([v]) => v === value);

/**
 * URL 查询参数 → 筛选状态；缺省或非法的值回退默认（不限、最近 7 天）
 * @param list 列表 hook 解析出的 q 与筛选参数
 */
export function agentRunFiltersOf(list: {
  q: string;
  filters: Record<string, string>;
}): AgentRunFilters {
  const f = list.filters;
  return {
    user: list.q,
    canvasId: f.canvas_id ?? "",
    runId: f.run_id ?? "",
    status: inOptions(AGENT_RUN_STATUS_OPTIONS, f.status) ? f.status : "",
    source: inOptions(AGENT_RUN_SOURCE_OPTIONS, f.source) ? f.source : "",
    range: AGENT_RUN_RANGES.some(([v]) => v === f.range)
      ? (f.range as RangeKey)
      : DEFAULT_AGENT_RUN_RANGE,
  };
}

/**
 * 有没有生效的筛选（决定空结果时给不给「清除筛选」）；时间范围不算，它一直生效
 * @param filters 筛选状态
 */
export function hasAgentRunFilters(filters: AgentRunFilters): boolean {
  return (
    filters.user.trim() !== "" ||
    filters.canvasId.trim() !== "" ||
    filters.runId.trim() !== "" ||
    filters.status !== "" ||
    filters.source !== ""
  );
}

/**
 * 列表请求：列表 hook 的查询（q、筛选、page、page_size）→ GET /admin/agent/runs 的参数。
 * 空值不带；时间范围换成 from/to（缺省最近 7 天）。契约见设计文档 8.4
 * @param query 列表 hook 给出的查询参数
 * @param now 当前时间（测试注入），决定「近 N 天」的起点
 */
export function agentRunRequest(
  query: Record<string, string | number>,
  now: Date = new Date(),
): ListAgentRunsParams {
  const filters = agentRunFiltersOf({
    q: String(query.q ?? ""),
    filters: Object.fromEntries(
      AGENT_RUN_FILTER_KEYS.filter((k) => query[k] !== undefined).map((k) => [k, String(query[k])]),
    ),
  });
  const params: ListAgentRunsParams = {};
  const user = filters.user.trim();
  if (user) params.user = user;
  const canvasId = filters.canvasId.trim();
  if (canvasId) params.canvas_id = canvasId;
  const runId = filters.runId.trim();
  if (runId) params.run_id = runId;
  if (filters.status) params.status = filters.status;
  if (filters.source) params.source = filters.source;
  return {
    ...params,
    ...rangeWindow(filters.range, now),
    page: Number(query.page) || 1,
    page_size: Number(query.page_size) || 20,
  };
}

// ---------------------------------------------------------------- 展示

/** 运行状态的展示：文案与语气 */
export type AgentRunStatusView = {
  label: string;
  tone: "success" | "danger" | "info" | "neutral" | "warning";
};

const STATUS_VIEW: Record<AgentRunStatus, AgentRunStatusView> = {
  queued: { label: "排队中", tone: "info" },
  running: { label: "运行中", tone: "info" },
  waiting_approval: { label: "等待批准", tone: "warning" },
  waiting_input: { label: "等待回答", tone: "warning" },
  succeeded: { label: "完成", tone: "success" },
  failed: { label: "失败", tone: "danger" },
  canceled: { label: "已停止", tone: "neutral" },
  budget_exhausted: { label: "预算用尽", tone: "warning" },
  step_limit: { label: "步数上限", tone: "warning" },
  timeout: { label: "超时", tone: "danger" },
  interrupted: { label: "中断", tone: "warning" },
  expired: { label: "等待超时", tone: "neutral" },
};

/** 运行状态文案与语气；未知状态原样显示 */
export const agentRunStatusView = (status: string): AgentRunStatusView =>
  STATUS_VIEW[status as AgentRunStatus] ?? { label: status, tone: "neutral" };

/** 运行还没结束：排队、运行中、等批准、等回答（与后端 ActiveRunStatuses 一致） */
export const isRunUnfinished = (status: string): boolean =>
  status === "queued" ||
  status === "running" ||
  status === "waiting_approval" ||
  status === "waiting_input";

/** 模式的中文名，不认识的原样显示 */
export const agentModeLabel = (mode: string): string =>
  AGENT_MODES.find((item) => item.value === mode)?.label ?? mode;

/** 耗时格：没结束为「—」，其余按秒 / 分秒 */
export const durationText = (ms: number | null): string => formatDurationMs(ms);

/** 积分格：0 也写 0 */
export const creditsText = (credits: number): string => String(credits);

// ---------------------------------------------------------------- 下载

/** 轨迹包保存的文件名，与后端 Content-Disposition 一致 */
export const traceFileName = (rid: string): string => `agent-trace-${rid}.zip`;

/** 一行的下载状态 */
export type TraceDownloadView = {
  canDownload: boolean;
  /** 不能下载时写在置灰按钮下面的原因 */
  reason: string | null;
  /** 运行没结束：仍可下载，按钮旁标「未完成」 */
  unfinished: boolean;
  /** 轨迹不完整（trace_complete=false）：行上打「不完整」标签 */
  incomplete: boolean;
};

/**
 * 判定一行能不能下载以及要给什么说明。
 * 过期、关闭的行没有轨迹：置灰并写明原因，不再打「未完成 / 不完整」（没有轨迹谈不上完整）
 * @param run 列表里的一行
 */
export function traceDownloadView(
  run: Pick<AgentRunItem, "trace" | "status" | "trace_complete">,
): TraceDownloadView {
  if (run.trace === "expired") {
    return {
      canDownload: false,
      reason: "已过期（保留 14 天）",
      unfinished: false,
      incomplete: false,
    };
  }
  if (run.trace === "disabled") {
    return {
      canDownload: false,
      reason: "当时轨迹记录已关闭",
      unfinished: false,
      incomplete: false,
    };
  }
  return {
    canDownload: true,
    reason: null,
    unfinished: isRunUnfinished(run.status),
    incomplete: !run.trace_complete,
  };
}

/** 下载成功后的提示：运行未结束时要多说一句轨迹不完整 */
export function traceDownloadSuccess(run: Pick<AgentRunItem, "status">): {
  tone: "success" | "warning";
  message: string;
} {
  return isRunUnfinished(run.status)
    ? { tone: "warning", message: "已下载（运行未结束，轨迹不完整），此次导出已记入操作审计" }
    : { tone: "success", message: "已下载，此次导出已记入操作审计" };
}

/**
 * 下载失败的提示。后端业务错误（轨迹已过期 / 已关闭 / run 不存在）说明原因并要求刷新列表，
 * 因为列表上的下载状态已经过时；其余（网络中断、超时、5xx、响应异常）提示可重试——
 * 审计在服务端开始传输时已写入，所以失败的下载也留了记录。
 */
export function traceDownloadFailure(error: unknown): { message: string; refresh: boolean } {
  if (error instanceof ApiError && typeof error.code === "number" && error.message) {
    return { message: `下载失败：${error.message}`, refresh: true };
  }
  return { message: "下载失败，可以重试", refresh: false };
}

/** zip 文件头：PK */
const isZip = async (blob: Blob): Promise<boolean> => {
  if (blob.size < 2) return false;
  const head = new Uint8Array(await blob.slice(0, 2).arrayBuffer());
  return head[0] === 0x50 && head[1] === 0x4b;
};

/**
 * 校验下载到的 blob 真的是 zip。
 * 后端对「轨迹已过期 / 已关闭 / 不存在」返回 `{code,msg}` 信封而不是文件，而请求按 blob 取，
 * 信封也会变成 blob——看内容（PK 文件头）而不是看 Content-Type，认出信封就抛带后端文案的 ApiError，
 * 免得把一段 JSON 存成 zip。
 * @param blob 下载到的内容
 * @returns 原样返回 zip
 * @throws ApiError 信封（业务码与文案取自后端）或无法识别的内容
 */
export async function ensureTraceZip(blob: Blob): Promise<Blob> {
  if (await isZip(blob)) return blob;
  let body: unknown;
  try {
    body = JSON.parse(await blob.text());
  } catch {
    body = undefined;
  }
  const envelope = body as { code?: unknown; msg?: unknown } | undefined;
  if (typeof envelope?.code === "number" && envelope.code !== 0) {
    throw new ApiError(
      typeof envelope.msg === "string" && envelope.msg ? envelope.msg : "请求失败",
      envelope.code,
      0,
      body as ConstructorParameters<typeof ApiError>[3],
    );
  }
  throw new ApiError("下载到的内容不是有效的轨迹包", "UNKNOWN_ERROR", 0);
}
