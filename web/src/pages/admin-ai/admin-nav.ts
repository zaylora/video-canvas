import {
  Boxes,
  HardDrive,
  LayoutDashboard,
  Puzzle,
  RadioTower,
  type LucideIcon,
} from "lucide-react";

export type AdminNavEntry = {
  /** 相对 /admin 的路径 */
  to: string;
  label: string;
  icon: LucideIcon;
};

export type AdminNavSection = { label: string; items: AdminNavEntry[] };

export const ADMIN_NAV: AdminNavSection[] = [
  {
    label: "AI 配置",
    items: [
      { to: "ai/overview", label: "总览", icon: LayoutDashboard },
      { to: "ai/models", label: "模型", icon: Boxes },
      { to: "ai/channels", label: "渠道", icon: RadioTower },
      { to: "ai/plugins", label: "插件", icon: Puzzle },
    ],
  },
  {
    label: "系统设置",
    items: [{ to: "settings/storage", label: "存储配置", icon: HardDrive }],
  },
];

/** 根据当前路径找出所属分组与条目，给面包屑用 */
export function findNav(pathname: string) {
  for (const group of ADMIN_NAV) {
    for (const item of group.items) {
      if (pathname.startsWith(`/admin/${item.to}`)) return { group, item };
    }
  }
  return null;
}
