import { findNav } from "@/pages/admin/admin-nav";

/** 应用名：所有标签页标题的末尾 */
export const APP_NAME = "连镜";

/**
 * 拼浏览器标签页标题：各段用「 - 」连接，末尾固定是应用名，空段忽略
 * @param parts 从具体到概括的各段，如 ["模型渠道", "后台管理"]
 * @returns 如「模型渠道 - 后台管理 - 连镜」
 */
export function formatPageTitle(...parts: (string | null | undefined)[]): string {
  return [...parts.map((part) => part?.trim()), APP_NAME].filter(Boolean).join(" - ");
}

/**
 * 后台页面的标题：当前页名称 + 后台管理
 * @param pathname 当前路径，如 /admin/ai/channels
 */
export function adminPageTitle(pathname: string): string {
  return formatPageTitle(findNav(pathname)?.item.label, "后台管理");
}

/**
 * 画布页的标题：画布名称；没起名时用「未命名画布」
 * @param title 画布名称
 */
export function canvasPageTitle(title: string): string {
  return formatPageTitle(title.trim() || "未命名画布");
}

/** 首页外壳下各一级页面的名称，首页（创作页）只显示应用名 */
const HOME_SECTIONS: Record<string, string> = {
  canvases: "画布",
  assets: "资产",
  profile: "个人资料",
  conversations: "对话",
};

/**
 * 首页外壳内页面的标题
 * @param pathname 当前路径
 */
export function homePageTitle(pathname: string): string {
  return formatPageTitle(HOME_SECTIONS[pathname.split("/")[1] ?? ""]);
}
