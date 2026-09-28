import service from "@/utils/requests/service";

export interface LoginRequest {
  username: string;
  password: string;
}

export interface LoginResponse {
  token: string;
  expire_at: number;
}

/**
 * 登陆
 * @param data 
 * @returns 
 */
export const login = (data: LoginRequest) =>
  service.post<LoginResponse>("/auth/login", data, { silent: true });
