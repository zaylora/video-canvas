import type {
  ChannelCreateRequest,
  ChannelRateLimit,
  ChannelUpdateRequest,
  ChannelView,
} from "@/api/admin/ai/type";

/** 自定义请求头最多几条（与后端一致） */
export const MAX_CHANNEL_HEADERS = 32;

/** 请求头值最长字符数（与后端一致） */
const MAX_HEADER_VALUE = 1024;

/** 宿主独占或属于敏感头、不允许设置的请求头（小写，与后端一致） */
const FORBIDDEN_HEADERS: ReadonlySet<string> = new Set([
  "host",
  "content-length",
  "transfer-encoding",
  "connection",
  "upgrade",
  "authorization",
  "proxy-authorization",
  "cookie",
]);

/** HTTP 头名称允许的字符（RFC 7230 token） */
const HEADER_TOKEN = /^[!#$%&'*+\-.^_`|~0-9A-Za-z]+$/;

/** 文本里有没有控制字符（含换行、制表符、DEL） */
const hasControlChar = (text: string) =>
  [...text].some((char) => {
    const code = char.charCodeAt(0);
    return code < 0x20 || code === 0x7f;
  });

/** 请求头编辑器里的一行 */
export type HeaderRow = {
  /** 请求头名称 */
  name: string;
  /** 请求头值（明文存储，不要放密钥） */
  value: string;
};

/** 渠道编辑表单；数字输入框存原始文本 */
export type ChannelFormState = {
  /** 显示名 */
  name: string;
  /** 上游 Base URL */
  baseUrl: string;
  /** 自定义请求头，逐行 */
  headers: HeaderRow[];
  /** 是否启用 */
  enabled: boolean;
  /** 每秒请求数，空表示不限 */
  rps: string;
  /** 最大同时请求数，空表示不限 */
  maxConcurrency: string;
  /** 最大同时生成数，空表示不限 */
  maxRunning: string;
};

/** 新建渠道的空表单 */
export function emptyChannelForm(): ChannelFormState {
  return {
    name: "",
    baseUrl: "",
    headers: [],
    enabled: true,
    rps: "",
    maxConcurrency: "",
    maxRunning: "",
  };
}

const numText = (value: unknown) =>
  typeof value === "number" && Number.isFinite(value) && value > 0 ? String(value) : "";

/**
 * 渠道视图 → 编辑表单
 * @param view 渠道视图
 */
export function channelFormFromView(view: ChannelView): ChannelFormState {
  return {
    name: view.name ?? "",
    baseUrl: view.base_url ?? "",
    headers: Object.entries(view.headers ?? {}).map(([name, value]) => ({ name, value })),
    enabled: !!view.enabled,
    rps: numText(view.rate_limit?.rps),
    maxConcurrency: numText(view.rate_limit?.max_concurrency),
    maxRunning: numText(view.rate_limit?.max_running),
  };
}

/**
 * Base URL：http/https、有主机、不带用户名密码、不带查询参数与片段（与后端一致）；通过返回 null
 * @param raw 输入框原文
 */
export function checkBaseUrl(raw: string): string | null {
  const text = raw.trim();
  if (!text) return "请填写 Base URL";
  let url: URL;
  try {
    url = new URL(text);
  } catch {
    return "Base URL 不是合法的地址";
  }
  if (url.protocol !== "http:" && url.protocol !== "https:") return "Base URL 只能是 http 或 https";
  if (!url.hostname) return "Base URL 缺少主机名";
  if (url.username || url.password) return "Base URL 不能包含用户名或密码";
  if (url.search || url.hash || text.includes("?") || text.includes("#"))
    return "Base URL 不能带查询参数或片段";
  return null;
}

/**
 * 请求头预校验，规则与后端一致：≤32 条；名称是合法 token、不区分大小写不得重复、
 * 禁用 Host / Authorization / Cookie 等；值 ≤1024 字符且无换行与控制字符。名称和值都为空的行忽略。
 * @param rows 编辑器里的行
 * @returns 错误表：`headers.<行号>` 是该行的问题，`headers` 是整体问题（条数）；没有问题为空对象
 */
export function checkHeaders(rows: readonly HeaderRow[]): Record<string, string> {
  const errors: Record<string, string> = {};
  const seen = new Set<string>();
  let count = 0;
  rows.forEach((row, index) => {
    const name = row.name.trim();
    if (!name && !row.value.trim()) return;
    count += 1;
    const fail = (message: string) => {
      errors[`headers.${index}`] ??= message;
    };
    if (!name) return fail("请填写请求头名称");
    if (!HEADER_TOKEN.test(name)) return fail(`请求头名称「${name}」不合法`);
    const lower = name.toLowerCase();
    if (FORBIDDEN_HEADERS.has(lower)) return fail(`请求头 ${name} 不能设置`);
    if (seen.has(lower)) return fail(`请求头 ${name} 重复（名称不区分大小写）`);
    seen.add(lower);
    if (row.value.length > MAX_HEADER_VALUE)
      return fail(`请求头值不能超过 ${MAX_HEADER_VALUE} 个字符`);
    if (hasControlChar(row.value)) return fail("请求头值不能包含换行或控制字符");
  });
  if (count > MAX_CHANNEL_HEADERS) errors.headers = `请求头最多 ${MAX_CHANNEL_HEADERS} 条`;
  return errors;
}

function parseLimit(
  text: string,
  label: string,
  integer: boolean,
): { value: number } | { error: string } {
  const trimmed = text.trim();
  if (!trimmed) return { value: 0 };
  const value = Number(trimmed);
  if (!Number.isFinite(value) || value < 0) return { error: `${label}必须是非负数字` };
  if (integer && !Number.isInteger(value)) return { error: `${label}必须是整数` };
  return { value };
}

/** 表单 → 请求体的结果 */
export type ChannelRequestResult =
  | { ok: false; errors: Record<string, string> }
  | {
      ok: true;
      /** 新建用的完整请求体（编辑时 key 取原渠道） */
      create: ChannelCreateRequest;
      /** 编辑用的差异请求体，只含改过的字段 */
      update: ChannelUpdateRequest;
      /** 编辑时是否有改动；新建恒为 true */
      changed: boolean;
    };

/** 请求头按名称升序的键值对，比较时不看顺序 */
const headerEntries = (headers: Record<string, string> | null | undefined) =>
  JSON.stringify(Object.entries(headers ?? {}).sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0)));

/**
 * 表单 → 请求体。新建用 create；编辑用 update（只含改过的字段，没改动时 changed=false）。
 * 错误键：name / baseUrl / headers / headers.<行号> / rps / maxConcurrency / maxRunning
 * @param form 表单
 * @param original 编辑时的原渠道；新建不传
 */
export function buildChannelRequest(
  form: ChannelFormState,
  original?: ChannelView | null,
): ChannelRequestResult {
  const errors: Record<string, string> = {};
  const name = form.name.trim();
  if (!name) errors.name = "请填写渠道名称";
  else if ([...name].length > 128) errors.name = "渠道名称不能超过 128 个字";
  const baseUrlError = checkBaseUrl(form.baseUrl);
  if (baseUrlError) errors.baseUrl = baseUrlError;
  Object.assign(errors, checkHeaders(form.headers));
  const rps = parseLimit(form.rps, "rps", false);
  if ("error" in rps) errors.rps = rps.error;
  const concurrency = parseLimit(form.maxConcurrency, "最大同时请求数", true);
  if ("error" in concurrency) errors.maxConcurrency = concurrency.error;
  const running = parseLimit(form.maxRunning, "最大同时生成数", true);
  if ("error" in running) errors.maxRunning = running.error;

  if (
    "error" in rps ||
    "error" in concurrency ||
    "error" in running ||
    Object.keys(errors).length > 0
  ) {
    return { ok: false, errors };
  }

  const headers: Record<string, string> = {};
  for (const row of form.headers) {
    const headerName = row.name.trim();
    if (headerName) headers[headerName] = row.value.trim();
  }
  const rateLimit: ChannelRateLimit = {
    rps: rps.value,
    max_concurrency: concurrency.value,
    max_running: running.value,
  };
  // 新建不带 key，由后端生成
  const create: ChannelCreateRequest = {
    name,
    base_url: form.baseUrl.trim(),
    headers,
    rate_limit: rateLimit,
    enabled: form.enabled,
  };
  if (!original) return { ok: true, create, update: {}, changed: true };

  const update: ChannelUpdateRequest = {};
  if (create.name !== original.name) update.name = create.name;
  if (create.base_url !== original.base_url) update.base_url = create.base_url;
  if (headerEntries(headers) !== headerEntries(original.headers)) update.headers = headers;
  const originalLimit = {
    rps: original.rate_limit?.rps ?? 0,
    max_concurrency: original.rate_limit?.max_concurrency ?? 0,
    max_running: original.rate_limit?.max_running ?? 0,
  };
  if (JSON.stringify(rateLimit) !== JSON.stringify(originalLimit)) update.rate_limit = rateLimit;
  if (create.enabled !== !!original.enabled) update.enabled = create.enabled;
  return { ok: true, create, update, changed: Object.keys(update).length > 0 };
}

/**
 * 限流的一句话摘要，渠道头部的标签用。
 * @param limit 渠道限流；null 或全 0 表示不限
 */
export function formatRateLimit(limit: ChannelRateLimit | null | undefined): string {
  const rps = limit?.rps ?? 0;
  const concurrency = limit?.max_concurrency ?? 0;
  const running = limit?.max_running ?? 0;
  if (!rps && !concurrency && !running) return "不限";
  return `rps ${rps || "不限"} · 并发 ${concurrency || "不限"} · 同时生成 ${running || "不限"}`;
}

/**
 * 渠道当前负载的一句话摘要：生成中 / 同时生成上限 + 排队数。
 * @param load 负载记录；没有未完成任务的渠道没有记录，按空闲 0 处理
 * @param maxRunning 渠道的最大同时生成数；0 或缺省表示不限
 */
export function formatLoad(
  load: { running: number; waiting: number } | undefined,
  maxRunning: number | undefined,
): string {
  return `生成中 ${load?.running ?? 0}/${maxRunning || "不限"} · 排队 ${load?.waiting ?? 0}`;
}
