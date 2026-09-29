import service from "@/utils/requests/service";

/** 登录请求参数 */
export interface LoginRequest {
  /** 登录账号 */
  username: string;
  /** 登录密码 */
  password: string;
}

/** 登录响应 */
export interface LoginResponse {
  /** 登录令牌（JWT） */
  token: string;
  /** 令牌过期时间戳 */
  expire_at: number;
}

/**
 * 用户登录
 * @param data 登录账号和密码
 * @returns 登录令牌及过期时间
 */
export const login = (data: LoginRequest) =>
  service.post<LoginResponse>("/auth/login", data);
