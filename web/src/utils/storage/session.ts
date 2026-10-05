import { IMPORT_DRAFT_PREFIX } from "@/utils/admin/import-draft";

import { removeToken } from "./token";

/** 清掉 sessionStorage 里后台“导入模型”留下的草稿；存储不可用就跳过 */
function clearImportDrafts() {
  try {
    const storage = globalThis.sessionStorage;
    if (!storage) return;
    const keys: string[] = [];
    for (let i = 0; i < storage.length; i += 1) {
      const key = storage.key(i);
      if (key?.startsWith(IMPORT_DRAFT_PREFIX)) keys.push(key);
    }
    keys.forEach((key) => storage.removeItem(key));
  } catch {
    // 隐私模式下 sessionStorage 可能抛错，清不掉也不影响退出
  }
}

/**
 * 清掉浏览器里这个账号的登录数据：令牌、过期时间、角色，以及后台导入草稿。
 * 界面偏好（主题、侧边栏、列表密度）不是登录数据，保留。
 */
export function clearSession() {
  removeToken();
  clearImportDrafts();
}

/**
 * 退出登录：清掉登录数据后整页跳到登录页。
 * 用整页跳转而不是路由跳转：内存里的 store（积分、任务、管理端角色、WebSocket）都随页面销毁，
 * 下一个登录的人不会看到上一个账号的残留。
 */
export function logout() {
  clearSession();
  window.location.replace("/login");
}
