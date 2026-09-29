import axios from "axios";
import { toast } from "sonner";
import type { AxiosInstance, AxiosResponse } from "axios";
import { getToken, removeToken } from "../storage/token";

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

  constructor(
    message: string,
    code: ApiErrorCode,
    status: number,
    body?: ApiErrorBody,
  ) {
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
 */
const reject = (error: ApiError) => {
  console.error(`[api] ${error.code}: ${error.message}`);
  toast.error(error.message, { id: `api:${error.code}` });
  return Promise.reject(error);
};

const instance: AxiosInstance = axios.create({
  baseURL: import.meta.env.VITE_API_BASE_URL,
  timeout: 2 * 60 * 1000,
  headers: {
    "Content-Type": "application/json",
  },
});

instance.interceptors.request.use(
  (config) => {
    const token = getToken();
    if (token) config.headers.Authorization = `Bearer ${token}`;
    return config;
  },
  (error) => Promise.reject(error),
);

instance.interceptors.response.use(
  (response: AxiosResponse) => {
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
      );
    }

    return response.data.data;
  },
  (error: unknown) => {
    if (!axios.isAxiosError(error)) {
      return reject(new ApiError("请求失败", "UNKNOWN_ERROR", 0));
    }

    if (error.code === "ECONNABORTED" || error.code === "ETIMEDOUT") {
      return reject(new ApiError("请求超时", "TIMEOUT", 0));
    }

    const response = error.response;
    if (!response) {
      return reject(
        new ApiError("网络异常，请检查后端服务", "NETWORK_ERROR", 0),
      );
    }

    if (response.status === 401) removeToken();

    const body = isApiResponse(response.data) ? response.data : undefined;
    const code: ApiErrorCode =
      body && body.code !== 0 ? body.code : `HTTP_${response.status}`;

    return reject(
      new ApiError(
        body?.msg || "请求失败",
        code,
        response.status,
        body,
      ),
    );
  },
);

export default instance;
