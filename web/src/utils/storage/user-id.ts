import { getToken } from "./token";

/**
 * 从登录令牌里取 user_id，只用来给浏览器本地数据（草稿、视口）分命名空间，
 * 换账号后看不到上一个人的内容。不校验签名，也不用于任何权限判断，权限以后端为准。
 */
export function userIdFromToken(token: string | null | undefined): string | null {
  if (!token) return null;
  const payload = token.split(".")[1];
  if (!payload) return null;
  try {
    const base64 = payload.replace(/-/g, "+").replace(/_/g, "/");
    const padded = base64 + "=".repeat((4 - (base64.length % 4)) % 4);
    const bytes = Uint8Array.from(atob(padded), (char) => char.charCodeAt(0));
    const claims = JSON.parse(new TextDecoder().decode(bytes)) as { user_id?: unknown };
    const id = claims.user_id;
    return typeof id === "number" && Number.isInteger(id) && id > 0 ? String(id) : null;
  } catch {
    return null;
  }
}

/** 当前登录账号的 user_id；没登录或令牌异常返回 null */
export const getCurrentUserId = () => userIdFromToken(getToken());
