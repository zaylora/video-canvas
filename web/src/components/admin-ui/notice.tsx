import type { ReactNode } from "react";
import { CheckCircle2, CircleAlert, Info, TriangleAlert } from "lucide-react";

import { cn } from "@/lib/utils";

const NOTICE_TONE = {
  info: { icon: Info, className: "border-sky-500/25 bg-sky-500/10 text-sky-700 dark:text-sky-400" },
  warning: {
    icon: TriangleAlert,
    className: "border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-400",
  },
  danger: {
    icon: CircleAlert,
    className: "border-red-500/25 bg-red-500/10 text-red-700 dark:text-red-400",
  },
  success: {
    icon: CheckCircle2,
    className: "border-emerald-500/25 bg-emerald-500/10 text-emerald-700 dark:text-emerald-400",
  },
  neutral: { icon: Info, className: "border-border bg-muted text-muted-foreground" },
} as const;

/** 提示条的语气 */
type NoticeTone = keyof typeof NOTICE_TONE;

/**
 * 提示条（设计稿样式）：一行里是图标、加粗标题、说明，右侧放操作；内容长了自动换行。
 * 图标加文字表达语气，不只靠颜色；danger 用 alert 角色，其余用 status，避免读屏乱播报。
 */
function Notice({
  tone = "info",
  title,
  children,
  className,
  action,
}: {
  tone?: NoticeTone;
  title?: ReactNode;
  children?: ReactNode;
  className?: string;
  /** 右侧的操作（按钮 / 链接） */
  action?: ReactNode;
}) {
  const { icon: Icon, className: toneClass } = NOTICE_TONE[tone];
  return (
    <div
      data-slot="notice"
      data-tone={tone}
      role={tone === "danger" ? "alert" : "status"}
      className={cn(
        "flex items-start gap-2 rounded-lg border px-3 py-2 text-sm",
        toneClass,
        className,
      )}
    >
      <Icon className="mt-0.5 size-4 shrink-0" />
      <div className="flex min-w-0 flex-1 flex-wrap items-baseline gap-x-2 gap-y-0.5">
        {title && <span className="font-medium">{title}</span>}
        {children && <div className="min-w-0 opacity-80">{children}</div>}
      </div>
      {action && <div className="ml-auto shrink-0 self-center">{action}</div>}
    </div>
  );
}

export { Notice, type NoticeTone };
