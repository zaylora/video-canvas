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
  /** 账号角色：user / admin / super_admin */
  role: string;
}

/** 登录页注册相关的公开配置 */
export interface AuthConfig {
  /** 是否开放注册；为 false 时登录页不显示注册入口 */
  register_enabled: boolean;
  /** 注册是否需要邮箱验证码；false 表示全新环境的首个账号，免验证 */
  email_verify_required: boolean;
}

/** 注册请求参数 */
export interface RegisterRequest {
  /** 用户名 */
  username: string;
  /** 邮箱 */
  email: string;
  /** 密码 */
  password: string;
  /** 邮箱验证码；email_verify_required 为 false 时可省略 */
  code?: string;
}

/** 注册响应：与登录一致，注册即登录 */
export type RegisterResponse = LoginResponse;

/**
 * 用户登录
 * @param data 登录账号和密码
 * @returns 登录令牌及过期时间
 */
export const login = (data: LoginRequest) => service.post<LoginResponse>("/auth/login", data);

/**
 * 注册页配置：决定是否显示注册入口、是否需要验证码
 * @returns 注册开关与是否需要邮箱验证码
 */
export const getAuthConfig = () => service.get<AuthConfig>("/auth/config");

/**
 * 发送注册验证码（同邮箱 60 秒内限一次）
 * @param data 接收验证码的邮箱
 */
export const sendRegisterCode = (data: { email: string }) =>
  service.post<null>("/auth/register/code", data);

/**
 * 注册新账号；成功后直接返回登录令牌
 * @param data 用户名、邮箱、密码与验证码
 * @returns 登录令牌、过期时间与角色
 */
export const register = (data: RegisterRequest) =>
  service.post<RegisterResponse>("/auth/register", data);
