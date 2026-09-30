import type { TraceStep } from "@/api/admin-ai/type";

/**
 * 试跑追踪（GET /test-runs/:id/trace）→ 面板上的展示模型。
 * 后端按执行顺序追加步骤，这里保持原顺序；内容已由后端脱敏，前端原样展示，只做格式化与长文本折叠。
 */

export type TraceBlock = {
  label: string;
  text: string;
};

export type TraceStepView = {
  /** 列表 key */
  id: string;
  /** 从 1 开始的序号 */
  index: number;
  kind: "hook" | "http" | "other";
  /** 主标题：钩子名，或“方法 URL” */
  title: string;
  /** 步骤名（submit / query / prepare:0 …） */
  stepName: string;
  durationMs: number | null;
  /** HTTP 状态码 */
  status?: number;
  /** 出错信息 */
  error?: string;
  /** 出错（有 error，或 HTTP 状态 ≥ 400）的步骤要高亮 */
  failed: boolean;
  /** 输入 / 输出 / 请求头 / 请求体 / 响应体 */
  blocks: TraceBlock[];
  /** utils.log 的输出 */
  logs: string[];
  /** 响应体被后端截断 */
  truncated: boolean;
};

export type TraceView = {
  steps: TraceStepView[];
  totalMs: number;
  failedCount: number;
};

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

/** 任意 JSON 值 → 缩进文本；字符串如果本身是 JSON 就展开，否则原样 */
export function prettyValue(value: unknown): string {
  if (value === undefined || value === null) return "";
  if (typeof value === "string") {
    const trimmed = value.trim();
    if (/^[[{]/.test(trimmed)) {
      try {
        return JSON.stringify(JSON.parse(trimmed), null, 2);
      } catch {
        // 截断或不是 JSON：原样展示
      }
    }
    return value;
  }
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

const headerText = (headers: unknown) =>
  isRecord(headers)
    ? Object.entries(headers)
        .map(([key, value]) => `${key}: ${String(value)}`)
        .join("\n")
    : "";

const str = (value: unknown) => (typeof value === "string" ? value : "");

function toStepView(raw: TraceStep, index: number): TraceStepView {
  const kind = raw.kind === "hook" || raw.kind === "http" ? raw.kind : "other";
  const blocks: TraceBlock[] = [];
  const push = (label: string, text: string) => {
    if (text) blocks.push({ label, text });
  };
  let title = str(raw.name) || `步骤 ${index + 1}`;
  let status: number | undefined;
  let logs: string[] = [];
  let truncated = false;

  if (kind === "hook" && isRecord(raw.hook)) {
    const hook = raw.hook;
    title = str(hook.name) || title;
    push("输入", prettyValue(hook.input));
    push("输出", prettyValue(hook.output));
    logs = Array.isArray(hook.logs)
      ? hook.logs.map((line) => String(line))
      : [];
  } else if (kind === "http") {
    const request = isRecord(raw.request) ? raw.request : null;
    const response = isRecord(raw.response) ? raw.response : null;
    if (request) {
      title = `${str(request.method) || "GET"} ${str(request.url)}`.trim();
      push("请求头", headerText(request.headers));
      push("请求体", prettyValue(request.body));
    }
    if (response) {
      status =
        typeof response.status === "number" ? response.status : undefined;
      push("响应体", prettyValue(response.body));
      truncated = response.truncated === true;
    }
  }

  const error = str(raw.error) || undefined;
  const duration =
    typeof raw.duration_ms === "number" && Number.isFinite(raw.duration_ms)
      ? Math.max(0, raw.duration_ms)
      : null;
  return {
    id: `${index}-${str(raw.name)}`,
    index: index + 1,
    kind,
    title,
    stepName: str(raw.name),
    durationMs: duration,
    status,
    error,
    failed: !!error || (status !== undefined && status >= 400),
    blocks,
    logs,
    truncated,
  };
}

/** 后端步骤数组 → 展示模型；null / 坏条目都不崩 */
export function toTraceView(steps: unknown): TraceView {
  const list = Array.isArray(steps) ? steps.filter(isRecord) : [];
  const views = list.map((step, index) =>
    toStepView(step as unknown as TraceStep, index),
  );
  return {
    steps: views,
    totalMs: views.reduce((sum, step) => sum + (step.durationMs ?? 0), 0),
    failedCount: views.filter((step) => step.failed).length,
  };
}

/** 长文本折叠：超过行数或字数就只给前一段 */
export function foldText(
  text: string,
  maxLines = 12,
  maxChars = 1500,
): { folded: boolean; preview: string } {
  const lines = text.split("\n");
  if (lines.length <= maxLines && text.length <= maxChars)
    return { folded: false, preview: text };
  let preview = lines.slice(0, maxLines).join("\n");
  if (preview.length > maxChars) preview = preview.slice(0, maxChars);
  return { folded: true, preview };
}

/** 12 -> 「12ms」，1500 -> 「1.5s」 */
export function formatDuration(ms: number | null | undefined): string {
  if (ms == null || !Number.isFinite(ms)) return "-";
  return ms < 1000
    ? `${Math.round(ms)}ms`
    : `${(ms / 1000).toFixed(ms < 10_000 ? 2 : 1)}s`;
}
