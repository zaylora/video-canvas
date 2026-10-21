import type {
  AgentRunDetail,
  AgentRunEvent,
  AgentRunMutation,
  AgentRunStep,
  AgentRunStepTool,
  AgentRunToolCall,
} from "@/api/admin/agent-run/type.d";
import type { TagTone } from "@/components/admin-ui/tag";
import { feedbackReasonLabel, runStatusView } from "@/utils/admin/agent-run";
import { formatFullTime } from "@/utils/admin/log-rules";
import { formatDuration } from "@/utils/admin/trace";

/**
 * 运行详情抽屉的纯逻辑：请求原文、时间线步骤的显示信息、工具步骤的提示、用户信号、画布改动、上下文、
 * 复制 Markdown 与下载 JSON，以及抽屉自己的加载状态机。
 * 时间线以后端给的 timeline 为准（它和 Markdown 导出由同一份步骤列表生成），这里不用 events / tool_calls 重新拼。
 */

const pad = (n: number) => String(n).padStart(2, "0");

// ---------------------------------------------------------------- 头部

/**
 * 用户请求原文：时间线里第一个 user 步骤的正文（插话不算）；没有就用摘要里的请求首句
 * @param detail 运行详情
 * @returns 请求文本；都没有返回空串
 */
export function requestText(detail: AgentRunDetail): string {
  return detail.timeline.find((s) => s.kind === "user")?.text ?? detail.digest?.request_head ?? "";
}

/** 头部的基础信息 */
export type RunHeadInfo = {
  /** 运行状态 */
  status: string;
  /** 创建时间（RFC3339） */
  createdAt: string;
  /** 结束时间（RFC3339），没结束为 null */
  endedAt: string | null;
  /** 耗时（毫秒）；没结束为 null */
  durationMs: number | null;
  /** 步数 */
  steps: number;
  /** 花掉的积分 */
  credits: number;
  /** 输入 Token */
  inputTokens: number;
  /** 输出 Token */
  outputTokens: number;
};

/**
 * 头部信息：优先用摘要；运行刚创建、摘要还没生成时退回运行本身（耗时用结束减创建，Token 把模型调用加起来）
 * @param detail 运行详情
 * @returns 摘要和运行都没有时返回 null
 */
export function runHeadInfo(detail: AgentRunDetail): RunHeadInfo | null {
  const { digest, run } = detail;
  if (digest)
    return {
      status: digest.status,
      createdAt: digest.created_at,
      endedAt: digest.ended_at,
      durationMs: digest.duration_ms,
      steps: digest.steps,
      credits: digest.spent_credits,
      inputTokens: digest.input_tokens,
      outputTokens: digest.output_tokens,
    };
  if (!run) return null;
  const span = run.ended_at ? Date.parse(run.ended_at) - Date.parse(run.created_at) : NaN;
  return {
    status: run.status,
    createdAt: run.created_at,
    endedAt: run.ended_at,
    durationMs: Number.isNaN(span) ? null : span,
    steps: run.steps,
    credits: run.spent_credits,
    inputTokens: detail.model_calls.reduce((sum, m) => sum + m.input_tokens, 0),
    outputTokens: detail.model_calls.reduce((sum, m) => sum + m.output_tokens, 0),
  };
}

// ---------------------------------------------------------------- 时间线

/**
 * 步骤时间：相对运行开始的偏移（+3.5s、+1m05s），方便数「第几秒发生了什么」；
 * 起点未知或事件早于起点时退回时分秒
 * @param at 事件时间（RFC3339）
 * @param startMs 运行开始时间（毫秒）；未知为 null
 * @returns 没有 at 时为空串
 */
export function stepOffsetText(at: string | undefined, startMs: number | null): string {
  if (!at) return "";
  const time = Date.parse(at);
  if (Number.isNaN(time)) return "";
  if (startMs === null || time < startMs) {
    const d = new Date(time);
    return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
  }
  const seconds = (time - startMs) / 1000;
  if (seconds < 60) return `+${seconds.toFixed(1)}s`;
  const whole = Math.floor(seconds);
  return `+${Math.floor(whole / 60)}m${pad(whole % 60)}s`;
}

/** 时间线一步的显示信息；图标按 kind 在视图里选 */
export type StepView = {
  /** 编号，如「#12」，等宽小字，分析时用它引用 */
  id: string;
  /** 种类 */
  kind: AgentRunStep["kind"];
  /** 类型名，原样用后端的 label */
  label: string;
  /** 正文；状态步骤把状态值译成中文，其余原样 */
  text: string;
  /** 时间文字，没有为空串 */
  offset: string;
  /** 时间的完整悬浮提示 */
  title: string;
  /** codemode 子调用，缩进显示并加「↳」 */
  indent: boolean;
  /** 色调：撤销警告、反馈按 👍/👎、状态按运行状态、工具报错红，其余中性 */
  tone: TagTone;
};

/**
 * 状态步骤的色调：沿用运行状态的色调，其中等待、运行中是 info
 */
function statusTone(text: string): TagTone {
  return runStatusView(text).tone;
}

/** 步骤色调，见 StepView.tone */
function stepTone(step: AgentRunStep): TagTone {
  switch (step.kind) {
    case "undo":
      return "warning";
    case "feedback":
      return step.text.startsWith("👎")
        ? "danger"
        : step.text.startsWith("👍")
          ? "success"
          : "neutral";
    case "status":
      return statusTone(step.text);
    case "tool":
      return step.tool?.is_error ? "danger" : "neutral";
    default:
      return "neutral";
  }
}

/**
 * 时间线一步 → 显示信息
 * @param step 后端给的步骤
 * @param startMs 运行开始时间（毫秒）；未知为 null
 */
export function stepView(step: AgentRunStep, startMs: number | null): StepView {
  return {
    id: `#${step.n}`,
    kind: step.kind,
    label: step.label,
    text: step.kind === "status" ? runStatusView(step.text).label : step.text,
    offset: stepOffsetText(step.at, startMs),
    title: step.at ? formatFullTime(step.at) : "",
    indent: !!step.nested,
    tone: stepTone(step),
  };
}

/** 工具步骤的标题：工具名 · 耗时 · 成败 */
export type ToolHeadline = {
  /** 工具名 */
  name: string;
  /** 耗时文字；没有记录为 null */
  duration: string | null;
  /** 报错 */
  failed: boolean;
  /** 成败文字：成功 / 报错 */
  outcome: "成功" | "报错";
};

/**
 * 工具步骤标题
 * @param tool 步骤里的工具细节
 */
export function toolHeadline(tool: AgentRunStepTool): ToolHeadline {
  return {
    name: tool.name,
    duration: tool.duration_ms ? formatDuration(tool.duration_ms) : null,
    failed: tool.is_error,
    outcome: tool.is_error ? "报错" : "成功",
  };
}

/** 工具步骤展开后的一条提示 */
export type ToolNote = {
  /** 种类，同时用作 key */
  key: "purged" | "unrecorded" | "truncated";
  /** 色调：过保留期和未记录是中性说明，截断是警告 */
  tone: TagTone;
  /** 文案 */
  text: string;
};

/** 本地日期「2026-10-20」 */
function dayText(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

/**
 * 工具步骤的提示：已过保留期（参数和结果已清理，不会再有截断）、未记录参数、已截断
 * @param tool 步骤里的工具细节
 */
export function toolNotes(tool: AgentRunStepTool): ToolNote[] {
  const notes: ToolNote[] = [];
  if (tool.purged_at)
    notes.push({
      key: "purged",
      tone: "neutral",
      text: `已过保留期（${dayText(tool.purged_at)}），参数和结果已清理，摘要和耗时仍在`,
    });
  else if (!tool.recorded) notes.push({ key: "unrecorded", tone: "neutral", text: "未记录参数" });
  else if (tool.truncated)
    notes.push({ key: "truncated", tone: "warning", text: "已截断，完整内容请下载 JSON" });
  return notes;
}

// ---------------------------------------------------------------- 用户信号

/** 用户信号区要显示的内容 */
export type UserSignals = {
  /** 评价：原因已译成中文；没评价为 null */
  feedback: { rating: 1 | -1; reasons: string[]; comment: string } | null;
  /** 改动被撤销过 */
  undone: boolean;
  /** 用户插话次数 */
  steerCount: number;
  /** 被用户拒绝的审批数 */
  approvalsRejected: number;
  /** 工具报错次数 */
  toolErrors: number;
  /** 是否有任何信号；没有时显示「没有用户反馈或撤销」 */
  hasAny: boolean;
};

/**
 * 汇总用户信号：评价、撤销、插话、被拒审批、工具报错。优先用摘要里的计数；没有摘要时从时间线和改动里数
 * @param detail 运行详情
 */
export function userSignals(detail: AgentRunDetail): UserSignals {
  const { digest, feedback, timeline, mutations } = detail;
  let fb: UserSignals["feedback"] = null;
  if (feedback)
    fb = {
      rating: feedback.rating,
      reasons: feedback.reasons.map(feedbackReasonLabel),
      comment: feedback.comment,
    };
  else if (digest && digest.feedback !== 0)
    fb = { rating: digest.feedback, reasons: [], comment: "" };

  const undone = !!digest?.undone || mutations.some((m) => m.undone_at !== null);
  const steerCount = digest?.steer_count ?? timeline.filter((s) => s.kind === "steer").length;
  const approvalsRejected = digest?.approvals_rejected ?? 0;
  const toolErrors = digest?.tool_errors ?? timeline.filter((s) => s.tool?.is_error).length;
  return {
    feedback: fb,
    undone,
    steerCount,
    approvalsRejected,
    toolErrors,
    hasAny: !!fb || undone || steerCount > 0 || approvalsRejected > 0 || toolErrors > 0,
  };
}

// ---------------------------------------------------------------- 画布改动、上下文

/** 画布改动表的一行 */
export type MutationRow = {
  /** 运行内序号 */
  seq: number;
  /** 改动种类 */
  kind: string;
  /** 改动条数（changes 数组长度；不是数组记 0） */
  count: number;
  /** 来自哪个工具；对不上记录时为空串 */
  toolName: string;
  /** 工具调用 ID */
  toolCallId: string;
  /** 已被撤销 */
  undone: boolean;
};

/**
 * 画布改动 → 表格行
 * @param mutations 画布改动
 * @param toolCalls 工具调用，用来把 tool_call_id 对上工具名
 */
export function mutationRows(
  mutations: AgentRunMutation[],
  toolCalls: AgentRunToolCall[],
): MutationRow[] {
  return mutations.map((m) => ({
    seq: m.seq,
    kind: m.kind,
    count: Array.isArray(m.changes) ? m.changes.length : 0,
    toolName: toolCalls.find((c) => c.id === m.tool_call_id)?.name ?? "",
    toolCallId: m.tool_call_id,
    undone: m.undone_at !== null,
  }));
}

/** 上下文里的一行 */
export type ContextLine = {
  /** 谁说的 */
  who: "用户" | "Agent";
  /** 事件 data.text */
  text: string;
};

/**
 * 更早两轮的上下文：用户消息和 Agent 回复各一行；其他事件、没有文字的事件忽略
 * @param events 详情里的 context 事件
 */
export function contextLines(events: AgentRunEvent[]): ContextLine[] {
  const lines: ContextLine[] = [];
  for (const ev of events) {
    const who = ev.type === "message.user" ? "用户" : ev.type === "message.done" ? "Agent" : null;
    const text = ev.data?.text;
    if (who && typeof text === "string" && text) lines.push({ who, text });
  }
  return lines;
}

// ---------------------------------------------------------------- 复制与下载

/**
 * 「下载 JSON」的文件名
 * @param rid 运行 ID（编码串），取前 8 位
 */
export function downloadFileName(rid: string): string {
  return `agent-run-${rid.slice(0, 8)}.json`;
}

/**
 * 「下载 JSON」的内容：把已加载的详情序列化，不再请求接口；工具参数和结果的完整内容都在 tool_calls 里
 * @param detail 运行详情
 */
export function runDetailJson(detail: AgentRunDetail): string {
  return JSON.stringify(detail, null, 2);
}

/**
 * 在浏览器里把文本存成文件（临时 a 标签 + Blob）
 * @param name 文件名
 * @param text 文件内容
 * @param mime 内容类型
 */
export function saveTextFile(name: string, text: string, mime = "application/json"): void {
  const url = URL.createObjectURL(new Blob([text], { type: `${mime};charset=utf-8` }));
  const link = document.createElement("a");
  link.href = url;
  link.download = name;
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  URL.revokeObjectURL(url);
}

/** 复制 Markdown 的依赖，测试里传假的 */
export type CopyMarkdownDeps = {
  /** 调导出接口拿 Markdown；失败时抛出（全局提示由拦截器弹） */
  exportMarkdown: (rid: string) => Promise<string>;
  /** 写剪贴板，返回是否成功 */
  copy: (text: string) => Promise<boolean>;
};

/**
 * 「复制为 Markdown」：先调导出接口，成功后写剪贴板
 * @param rid 运行 ID（编码串）
 * @param deps 导出与写剪贴板
 * @returns copied 已复制；copy-failed 导出成功但剪贴板不可用，页面要提示。导出接口失败直接抛出
 */
export async function copyRunMarkdown(
  rid: string,
  deps: CopyMarkdownDeps,
): Promise<"copied" | "copy-failed"> {
  const markdown = await deps.exportMarkdown(rid);
  return (await deps.copy(markdown)) ? "copied" : "copy-failed";
}

// ---------------------------------------------------------------- 抽屉状态

/** 运行不存在的业务码（ErrAgentRunMissing） */
const RUN_NOT_FOUND_CODE = 60012;

/**
 * 是不是「运行不存在」：HTTP 404 或业务码 60012。抽屉据此显示就地提示，而不是「加载失败」
 * @param error 请求抛出的错误
 */
export function isRunNotFound(error: unknown): boolean {
  if (typeof error !== "object" || error === null) return false;
  const e = error as { code?: unknown; status?: unknown };
  return e.status === 404 || e.code === RUN_NOT_FOUND_CODE;
}

/** 抽屉的状态：idle 没打开，loading 加载中，ready 有详情，error 加载失败，notfound 运行不存在 */
export type RunDrawerState = {
  /** 正在看的运行；null 表示没打开 */
  rid: string | null;
  /** 加载阶段 */
  phase: "idle" | "loading" | "ready" | "error" | "notfound";
  /** 详情，只有 ready 时有 */
  detail: AgentRunDetail | null;
};

/** 抽屉的事件；结果带 rid，是为了丢掉换了一行之后才回来的旧请求 */
export type RunDrawerAction =
  /** 打开一行（也用于重试） */
  | { type: "open"; rid: string }
  /** 详情拿到了 */
  | { type: "loaded"; rid: string; detail: AgentRunDetail }
  /** 详情没拿到 */
  | { type: "failed"; rid: string; notFound: boolean }
  /** 抽屉关闭，清理 */
  | { type: "close" };

/** 抽屉的初始状态 */
export const initialRunDrawer: RunDrawerState = { rid: null, phase: "idle", detail: null };

/**
 * 抽屉的状态转移：打开新的行就重新加载并丢掉旧详情；不属于当前行的结果直接忽略；关闭后清理
 */
export function runDrawerReducer(state: RunDrawerState, action: RunDrawerAction): RunDrawerState {
  switch (action.type) {
    case "open":
      return { rid: action.rid, phase: "loading", detail: null };
    case "loaded":
      return action.rid === state.rid
        ? { rid: state.rid, phase: "ready", detail: action.detail }
        : state;
    case "failed":
      return action.rid === state.rid
        ? { rid: state.rid, phase: action.notFound ? "notfound" : "error", detail: null }
        : state;
    case "close":
      return initialRunDrawer;
  }
}
