import { openAsBlob } from "node:fs";
import { readFile } from "node:fs/promises";
import { basename } from "node:path";

/** 业务错误：code 是后端信封里的 code，status 是 HTTP 状态码，exitCode 可覆盖进程退出码。 */
export class APIError extends Error {
  readonly code: number;
  readonly status: number;
  readonly exitCode?: number;
  /** 重试后可能成功的错误：网络不通、服务端 5xx。4xx 与业务错误重试没有意义。 */
  readonly retryable: boolean;

  constructor(code: number, message: string, status: number, exitCode?: number, retryable = false) {
    super(message);
    this.name = "APIError";
    this.code = code;
    this.status = status;
    this.exitCode = exitCode;
    this.retryable = retryable;
  }
}

/** 把 unknown 类型的抛出值变成一句话；不是 Error 就 String 它。 */
export function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/**
 * 规范化 API 根地址：允许用户只给站点根（https://host），自动补 /api/v1；末尾斜杠一律去掉。
 * 这样 `--base https://host` 和 `--base https://host/api/v1` 行为一致。
 */
export function normalizeBase(raw: unknown): string {
  const trimmed = String(raw || "")
    .trim()
    .replace(/\/+$/, "");
  // 地址写错属于用法错误（退出码 2），不是业务错误，脚本不该重试
  if (!trimmed) throw new APIError(-1, "缺少 API 地址，请用 --base 或环境变量 VC_API_BASE 指定", 2);
  if (!/^https?:\/\//i.test(trimmed))
    throw new APIError(-1, `API 地址必须以 http:// 或 https:// 开头：${trimmed}`, 2);
  return /\/api\/v1$/i.test(trimmed) ? trimmed : `${trimmed}/api/v1`;
}

/** 站点根（授权页地址用它拼），与 normalizeBase 互逆。 */
export function siteRootOf(base: string): string {
  return base.replace(/\/api\/v1$/i, "");
}

export type Query = Record<string, string | number | boolean | undefined | null>;

function buildURL(base: string, path: string, query?: Query): URL {
  const url = new URL(`${base}${path.startsWith("/") ? path : `/${path}`}`);
  for (const [key, value] of Object.entries(query || {})) {
    if (value !== undefined && value !== null && value !== "")
      url.searchParams.set(key, String(value));
  }
  return url;
}

/** 统一信封：code 0 / 缺省都算成功，data 是业务数据。 */
interface Envelope {
  code?: number;
  msg?: string;
  data?: unknown;
}

export interface RequestOptions {
  base: string;
  token?: string;
  method?: string;
  path: string;
  body?: unknown;
  query?: Query;
  headers?: Record<string, string>;
  fetchImpl?: typeof fetch;
  signal?: AbortSignal;
  exactIntegers?: boolean;
  redirect?: RequestInit["redirect"];
}

const exactJSON = JSON as unknown as {
  parse(
    text: string,
    reviver: (key: string, value: unknown, context?: { source?: string }) => unknown,
  ): unknown;
  rawJSON(text: string): object;
};

export function parseExactIntegers(text: string): unknown {
  return exactJSON.parse(text, (_key, value, context) => {
    if (
      typeof value === "number" &&
      !Number.isSafeInteger(value) &&
      /^-?\d+$/.test(context?.source ?? "")
    ) {
      return context?.source;
    }
    return value;
  });
}

export function encodeUnsignedInteger(value: string): object | number {
  if (!/^[1-9]\d{0,19}$/.test(value) || BigInt(value) > 18_446_744_073_709_551_615n)
    throw new APIError(-1, "编号须在无符号 64 位整数范围内", 2);
  const numeric = Number(value);
  return Number.isSafeInteger(numeric) ? numeric : exactJSON.rawJSON(value);
}

/** 拆统一信封：所有接口（JSON 与 multipart）共用同一套 401 / 错误码 / 重试语义。 */
async function parseEnvelope<T>(res: Response, exactIntegers = false): Promise<T> {
  const text = await res.text().catch((err: unknown) => {
    throw new APIError(-1, `未读完服务端响应：${errorMessage(err)}`, 0, undefined, true);
  });
  let envelope: Envelope;
  try {
    envelope = text
      ? ((exactIntegers ? parseExactIntegers(text) : JSON.parse(text)) as Envelope)
      : {};
  } catch {
    throw new APIError(
      -1,
      `服务端返回的不是 JSON（HTTP ${res.status}）`,
      res.status,
      undefined,
      res.status >= 500,
    );
  }
  if (res.status === 401) {
    throw new APIError(
      envelope.code ?? 20003,
      "登录已过期或被撤销，请重新执行 video-canvas-cli login",
      401,
    );
  }
  if (!res.ok || (envelope.code !== undefined && envelope.code !== 0)) {
    throw new APIError(
      envelope.code ?? -1,
      envelope.msg || `请求失败（HTTP ${res.status}）`,
      res.status,
      undefined,
      res.status >= 500,
    );
  }
  return envelope.data as T;
}

/**
 * 调一次后端接口并拆统一信封。
 *
 * 成功返回 data；HTTP 非 2xx 或 code !== 0 抛 APIError。401 单独带提示，
 * 因为 CLI 最常见的失败就是令牌过期，调用方要能一句说清「重新登录」。
 *
 * 泛型参数是调用方声明的返回形状：不写就得到 unknown，逼着调用处想清楚在读什么。
 */
export async function request<T = unknown>({
  base,
  token,
  method = "GET",
  path,
  body,
  query,
  headers: extra = {},
  fetchImpl = fetch,
  signal,
  exactIntegers,
  redirect,
}: RequestOptions): Promise<T> {
  const headers: Record<string, string> = { Accept: "application/json", ...extra };
  if (token) headers.Authorization = `Bearer ${token}`;
  if (body !== undefined) headers["Content-Type"] = "application/json";

  let res: Response;
  try {
    res = await fetchImpl(buildURL(base, path, query), {
      method,
      headers,
      signal,
      redirect,
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch (err) {
    throw new APIError(-1, `无法连接 ${base}：${errorMessage(err)}`, 0, undefined, true);
  }
  return parseEnvelope<T>(res, exactIntegers);
}

export interface UploadOptions {
  base: string;
  token?: string;
  /** 接口路径，如 /assets。 */
  path: string;
  /** 本地文件路径。 */
  file: string;
  /** multipart 的文件字段名，后端固定认 "file"。 */
  field?: string;
  /** 原始文件名；不含路径，避免把本机目录结构带上去。 */
  fileName?: string;
  /** 文件的真实类型，写进 multipart 的 part 头；缺省按二进制流。 */
  contentType?: string;
  query?: Query;
  signal?: AbortSignal;
  fetchImpl?: typeof fetch;
}

/**
 * 后端中转上传：multipart/form-data 发到 POST /api/v1/assets。
 *
 * 文件字段名默认 "file"（后端约定）。Content-Type 不手工设，交给 fetch 带 Boundary；
 * Authorization 带 Bearer，走普通登录态，无额外权限。信封解析与 request 一致。
 */
export async function uploadFile<T = unknown>({
  base,
  token,
  path,
  file,
  field = "file",
  fileName,
  contentType,
  query,
  signal,
  fetchImpl = fetch,
}: UploadOptions): Promise<T> {
  const form = new FormData();
  form.append(
    field,
    await openAsBlob(file, { type: contentType || "application/octet-stream" }),
    fileName || basename(file),
  );

  const headers: Record<string, string> = { Accept: "application/json" };
  if (token) headers.Authorization = `Bearer ${token}`;

  let res: Response;
  try {
    res = await fetchImpl(buildURL(base, path, query), {
      method: "POST",
      headers,
      body: form,
      signal,
    });
  } catch (err) {
    throw new APIError(-1, `无法连接 ${base}：${errorMessage(err)}`, 0, undefined, true);
  }
  return parseEnvelope<T>(res);
}

/**
 * 直传到对象存储：不带登录头（目标是签名地址，不是我们的后端），不解析信封
 * （对象存储返回的是它自己的 2xx / 错误，不是我们的 {code,msg,data}）。
 *
 * 2xx 算成功（S3 POST Policy 成功返回 204）；网络错误与非 2xx 都返回 false，
 * 由调用方决定降级到中转。签名 headers 原样带，但 Content-Length 由 fetch 自己算，不能手工设。
 */
export async function sendDirect(options: {
  url: string;
  method: "POST" | "PUT";
  headers?: Record<string, string>;
  /** POST 时表单里必须排在 fields 之后、file 最后。 */
  fields?: Record<string, string>;
  file: string;
  fileName?: string;
  /**
   * 文件的真实类型。POST 策略若钉了 $Content-Type（后端 s3.go 就是这么签的），
   * file part 的类型必须与申请时的 mime_type 完全一致，否则对象存储直接 403。
   */
  contentType?: string;
  fetchImpl?: typeof fetch;
}): Promise<boolean> {
  const {
    url,
    method,
    headers: signed = {},
    fields = {},
    file,
    fileName,
    contentType,
    fetchImpl = fetch,
  } = options;
  const forbidden = new Set(["content-length"]);
  const headers: Record<string, string> = Object.fromEntries(
    Object.entries(signed).filter(([key]) => !forbidden.has(key.toLowerCase())),
  );
  const blobType = contentType || "application/octet-stream";
  let body: FormData | Blob;
  if (method === "POST") {
    const form = new FormData();
    for (const [key, value] of Object.entries(fields)) {
      if (key.toLowerCase() !== "file") form.append(key, value);
    }
    form.append(
      "file",
      new Blob([await readFile(file)], { type: blobType }),
      fileName || basename(file),
    );
    body = form;
  } else {
    body = new Blob([await readFile(file)], { type: blobType });
  }
  try {
    const res = await fetchImpl(url, { method, headers, body });
    return res.status >= 200 && res.status < 300;
  } catch {
    return false;
  }
}
