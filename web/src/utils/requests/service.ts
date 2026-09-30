import type { AxiosRequestConfig } from "axios";
import instance from "./request";

export interface ServiceRequestConfig extends Omit<
  AxiosRequestConfig,
  "url" | "method" | "data" | "params"
> {
  url: string;
  method?: "get" | "post" | "put" | "patch" | "delete";
  data?: unknown;
  params?: Record<string, unknown>;
}

/** 快捷方法里透传的额外配置 */
export type RequestExtraConfig = Omit<ServiceRequestConfig, "url" | "method" | "data" | "params">;

/** 发请求，成功拿到的就是后端返回的业务数据本体，失败统一抛 ApiError */
const request = async <T>(config: ServiceRequestConfig): Promise<T> => {
  const { url, method = "get", data, params, ...rest } = config;

  // 响应拦截器已经把 AxiosResponse 拆成了 data，这里的运行时结果就是 T；
  // 全局错误提示由 request.ts 的拦截器统一处理
  return (await instance.request({ url, method, data, params, ...rest })) as T;
};

const service = {
  request,
  get: <T>(url: string, params?: Record<string, unknown>, config?: RequestExtraConfig) =>
    request<T>({ ...config, url, method: "get", params }),
  post: <T>(url: string, data?: unknown, config?: RequestExtraConfig) =>
    request<T>({ ...config, url, method: "post", data }),
  put: <T>(url: string, data?: unknown, config?: RequestExtraConfig) =>
    request<T>({ ...config, url, method: "put", data }),
  patch: <T>(url: string, data?: unknown, config?: RequestExtraConfig) =>
    request<T>({ ...config, url, method: "patch", data }),
  delete: <T>(url: string, config?: RequestExtraConfig) =>
    request<T>({ ...config, url, method: "delete" }),
  /** 上传文件，交给浏览器自己带 multipart 边界，不要手写 Content-Type */
  upload: <T>(url: string, formData: FormData, config?: RequestExtraConfig) =>
    request<T>({
      ...config,
      url,
      method: "post",
      data: formData,
      headers: { ...config?.headers, "Content-Type": undefined },
    }),
};

export default service;
