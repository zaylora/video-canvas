import axios from "axios";
import { toast } from "sonner";
import type { AxiosInstance, AxiosResponse, InternalAxiosRequestConfig } from "axios";
import { getToken, removeToken } from "../storage/token";
import { recordRequest, redactRequestBody, shouldLogRequest } from "./request-log";

declare module "axios" {
  interface AxiosRequestConfig {
    /** 为 true 时失败不弹全局 toast，由调用方自己决定怎么提示（错误仍会照常抛出） */
    silent?: boolean;
  }
}

/** 后端统一响应结构 */
export interface ApiResponse<T = unknown> {
  code: number;
  msg: string;
  data?: T;
  request_id?: string;
  [key: string]: unknown;
}

/** 后端错误响应体。保留扩展字段，便于处理业务错误附带的数据。 */
export type ApiErrorBody = ApiResponse;

export type ApiErrorCode =
  | number
  | "NETWORK_ERROR"
  | "TIMEOUT"
  | "UNKNOWN_ERROR"
  | `HTTP_${number}`;

/** 请求失败时统一抛出的错误，业务层只需 catch 这一种。 */
export class ApiError extends Error {
  /** 后端业务错误码；请求没发出去时为网络错误标识。 */
  code: ApiErrorCode;
  /** HTTP 状态码；请求没发出去时为 0。 */
  status: number;
  /** 完整错误响应体。 */
  body?: ApiErrorBody;

  constructor(message: string, code: ApiErrorCode, status: number, body?: ApiErrorBody) {
    super(message);
    this.name = "ApiError";
    this.code = code;
    this.status = status;
    this.body = body;
  }
}

const isApiResponse = (value: unknown): value is ApiResponse => {
  if (typeof value !== "object" || value === null) return false;

  const body = value as { code?: unknown; msg?: unknown };
  return typeof body.code === "number" && typeof body.msg === "string";
};

/**
 * 统一拒绝出口：先走全局错误提示，再把 ApiError 抛给调用方。
 * @param error 统一错误
 * @param silent 请求带了 silent 时不弹 toast
 */
const reject = (error: ApiError, silent = false) => {
  console.error(`[api] ${error.code}: ${error.message}`);
  if (!silent) toast.error(error.message, { id: `api:${error.code}` });
  return Promise.reject(error);
};

/** 业务错误码：账号已被停用（契约 53004） */
const ACCOUNT_DISABLED_CODE = 53004;

/**
 * 已登录状态下收到「账号已停用」：清掉 token 并回登录页（登录接口自己的 53004 不处理，交给登录页就地提示）。
 * 提示由随后的 reject 统一弹，这里不重复。
 * @param url 触发错误的请求路径
 */
const handleAccountDisabled = (url?: string) => {
  if (url?.startsWith("/auth/") || !getToken()) return;
  removeToken();
  if (typeof window !== "undefined" && !window.location.pathname.startsWith("/login")) {
    window.location.assign("/login");
  }
};

const instance: AxiosInstance = axios.create({
  baseURL: import.meta.env.VITE_API_BASE_URL,
  timeout: 2 * 60 * 1000,
  headers: {
    "Content-Type": "application/json",
  },
});

/** 请求发出时间：只给要记日志的请求打点 */
const logStart = new WeakMap<object, number>();

function logResponse(
  config: InternalAxiosRequestConfig | undefined,
  status: number,
  response: unknown,
  ok: boolean,
) {
  const start = config ? logStart.get(config) : undefined;
  if (!config || start === undefined) return;
  const url = config.url ?? "";
  const query = config.params ? `?${new URLSearchParams(config.params).toString()}` : "";
  recordRequest({
    time: start,
    duration: Date.now() - start,
    method: (config.method ?? "get").toUpperCase(),
    url: `${url}${query}`,
    request: redactRequestBody(url, config.data),
    status,
    code: isApiResponse(response) ? response.code : undefined,
    response,
    ok,
  });
}

instance.interceptors.request.use(
  (config) => {
    const token = getToken();
    if (token) config.headers.Authorization = `Bearer ${token}`;
    // 后台接口记一笔日志，响应拦截器里补上结果与耗时
    if (shouldLogRequest(config.url)) logStart.set(config, Date.now());
    return config;
  },
  (error) => Promise.reject(error),
);

instance.interceptors.response.use(
  (response: AxiosResponse) => {
    logResponse(
      response.config,
      response.status,
      response.data,
      !isApiResponse(response.data) || response.data.code === 0,
    );
    // 后端成功响应也包在统一 Body 中，业务层直接拿到 data。
    if (!isApiResponse(response.data)) return response.data;

    if (response.data.code !== 0) {
      return reject(
        new ApiError(
          response.data.msg || "请求失败",
          response.data.code,
          response.status,
          response.data,
        ),
        response.config.silent,
      );
    }

    return response.data.data;
  },
  (error: unknown) => {
    if (axios.isAxiosError(error)) {
      logResponse(
        error.config,
        error.response?.status ?? 0,
        error.response?.data ?? error.message,
        false,
      );
    }
    if (!axios.isAxiosError(error)) {
      return reject(new ApiError("请求失败", "UNKNOWN_ERROR", 0));
    }

    const silent = error.config?.silent;
    if (error.code === "ECONNABORTED" || error.code === "ETIMEDOUT") {
      return reject(new ApiError("请求超时", "TIMEOUT", 0), silent);
    }

    const response = error.response;
    if (!response) {
      return reject(new ApiError("网络异常，请检查后端服务", "NETWORK_ERROR", 0), silent);
    }

    if (response.status === 401) removeToken();

    const body = isApiResponse(response.data) ? response.data : undefined;
    if (body?.code === ACCOUNT_DISABLED_CODE) handleAccountDisabled(error.config?.url);
    const code: ApiErrorCode = body && body.code !== 0 ? body.code : `HTTP_${response.status}`;

    return reject(new ApiError(body?.msg || "请求失败", code, response.status, body), silent);
  },
);

export default instance;
