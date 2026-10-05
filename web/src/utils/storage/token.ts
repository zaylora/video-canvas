const tokenKey = "video_design_token";
const expireAtKey = "video_design_token_expire_at";
const roleKey = "video_design_role";

/** 读取登录 token，未登录时返回 null */
export const getToken = () => {
  const expireAt = Number(localStorage.getItem(expireAtKey));
  if (expireAt && Date.now() >= expireAt * 1000) {
    removeToken();
    return null;
  }
  return localStorage.getItem(tokenKey);
};

/**
 * 读取登录时保存的账号角色（user / admin / super_admin）。
 * 令牌里没有角色，所以登录响应里的 role 单独存；老会话没存过时返回 null。
 * 只用来决定菜单入口显不显示，权限以后端为准。token 过期时一并失效。
 */
export const getRole = () => (getToken() ? localStorage.getItem(roleKey) : null);

/** 补存角色：老会话登录时没存过，进入后台确认身份后补上；没登录时不写 */
export const saveRole = (role: string) => {
  if (getToken()) localStorage.setItem(roleKey, role);
};

/** 写入登录 token 与角色；不传角色会清掉上一个账号留下的角色 */
export const setToken = (token: string, expireAt: number, role?: string) => {
  localStorage.setItem(tokenKey, token);
  localStorage.setItem(expireAtKey, String(expireAt));
  if (role) localStorage.setItem(roleKey, role);
  else localStorage.removeItem(roleKey);
};

/** 清除登录 token */
export const removeToken = () => {
  localStorage.removeItem(tokenKey);
  localStorage.removeItem(expireAtKey);
  localStorage.removeItem(roleKey);
};
