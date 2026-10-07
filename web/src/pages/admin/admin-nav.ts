import {
  Boxes,
  HardDrive,
  ImageDown,
  Mail,
  LayoutDashboard,
  Puzzle,
  RadioTower,
  Sparkles,
  UserPlus,
  Users,
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
    label: "Agent",
    items: [{ to: "agent/skills", label: "技能", icon: Sparkles }],
  },
  {
    label: "用户",
    items: [{ to: "users", label: "用户管理", icon: Users }],
  },
  {
    label: "系统设置",
    items: [
      { to: "settings/storage", label: "存储配置", icon: HardDrive },
      { to: "settings/image-processor", label: "图片服务", icon: ImageDown },
      { to: "settings/register", label: "注册设置", icon: UserPlus },
      { to: "settings/email", label: "邮件服务", icon: Mail },
    ],
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
