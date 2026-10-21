import {
  Activity,
  ArrowUpCircle,
  Film,
  HardDrive,
  History,
  ImageDown,
  KeyRound,
  Lightbulb,
  Mail,
  LayoutDashboard,
  ListOrdered,
  Puzzle,
  RadioTower,
  ScrollText,
  UserPlus,
  Users,
  Wallet,
  type LucideIcon,
} from "lucide-react";

export type AdminNavEntry = {
  /** 相对 /admin 的路径 */
  to: string;
  /** 侧栏条目右侧的小标，如新功能的「新」；不写则显示条目计数（有的话） */
  badge?: string;
  label: string;
  icon: LucideIcon;
};

export type AdminNavSection = { label: string; items: AdminNavEntry[] };

export const ADMIN_NAV: AdminNavSection[] = [
  {
    label: "AI 配置",
    items: [
      { to: "ai/overview", label: "总览", icon: LayoutDashboard },
      { to: "ai/queue", label: "任务队列", icon: ListOrdered },
      { to: "ai/channels", label: "模型渠道", icon: RadioTower },
      { to: "ai/plugins", label: "插件", icon: Puzzle },
      { to: "agent/skills", label: "技能", icon: Lightbulb },
      { to: "agent/runs", label: "运行记录", icon: History },
      { to: "agent/tokens", label: "分析令牌", icon: KeyRound },
    ],
  },
  {
    label: "用户",
    items: [
      { to: "users", label: "用户管理", icon: Users },
      { to: "payment", label: "支付管理", icon: Wallet },
      { to: "usage", label: "使用记录", icon: Activity },
    ],
  },
  {
    label: "日志管理",
    items: [{ to: "logs/audits", label: "操作审计", icon: ScrollText }],
  },
  {
    label: "系统设置",
    items: [
      { to: "settings/storage", label: "存储配置", icon: HardDrive },
      { to: "settings/image-processor", label: "图片服务", icon: ImageDown },
      { to: "settings/register", label: "注册设置", icon: UserPlus },
      { to: "settings/email", label: "邮件服务", icon: Mail },
      { to: "settings/showcase", label: "登录页展示", icon: Film },
      { to: "settings/version", label: "版本与升级", icon: ArrowUpCircle },
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
