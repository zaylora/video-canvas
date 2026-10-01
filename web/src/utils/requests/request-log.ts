/** 一次接口请求的记录，给后台管理的“日志”面板看 */
export type RequestLogEntry = {
  id: number;
  /** 发出时间（毫秒） */
  time: number;
  /** 耗时（毫秒） */
  duration: number;
  method: string;
  /** 不含 baseURL 的路径（带查询参数） */
  url: string;
  /** 请求体；敏感接口与文件上传会被替换成说明文字 */
  request?: unknown;
  /** HTTP 状态码；网络错误为 0 */
  status: number;
  /** 业务码：成功为 0 */
  code?: number | string;
  /** 响应体（统一 Body 原样，不拆 data）或错误说明 */
  response?: unknown;
  ok: boolean;
};

/** 只记录这些前缀的请求，其余（画布、素材等）不进日志 */
const LOGGED_PREFIXES = ["/admin/"];
/** 请求体不落日志的接口：Key 是明文 */
const SECRET_PATTERN = /\/secret$/;
const MAX_ENTRIES = 200;

let entries: RequestLogEntry[] = [];
let nextId = 1;
const listeners = new Set<() => void>();

const emit = () => listeners.forEach((listener) => listener());

export const shouldLogRequest = (url: string | undefined) =>
  !!url && LOGGED_PREFIXES.some((prefix) => url.startsWith(prefix));

/** 请求体脱敏：Key 接口整段替换，文件上传只写文件名 */
export function redactRequestBody(url: string, body: unknown): unknown {
  if (body === undefined || body === null || body === "") return undefined;
  if (SECRET_PATTERN.test(url.split("?")[0])) return "（Key 已隐藏）";
  if (typeof FormData !== "undefined" && body instanceof FormData) {
    const files = [...body.values()]
      .map((value) => (typeof value === "object" && "name" in value ? value.name : null))
      .filter(Boolean);
    return `（文件上传：${files.join("、") || "表单"}）`;
  }
  if (typeof body === "string") {
    try {
      return JSON.parse(body);
    } catch {
      return body;
    }
  }
  return body;
}

export function recordRequest(entry: Omit<RequestLogEntry, "id">) {
  entries = [{ ...entry, id: nextId++ }, ...entries].slice(0, MAX_ENTRIES);
  emit();
}

export const getRequestLog = () => entries;

export function clearRequestLog() {
  entries = [];
  emit();
}

/** 订阅日志变化；返回取消订阅（给 useSyncExternalStore 用） */
export function subscribeRequestLog(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}
