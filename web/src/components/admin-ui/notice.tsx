import type { ReactNode } from "react";
import { CheckCircle2, CircleAlert, Info, TriangleAlert } from "lucide-react";

import { toneClasses, type TagTone } from "@/components/admin-ui/tag";
import { Alert } from "@/components/ui/alert";
import { cn } from "@/lib/utils";

const NOTICE_TONE = {
  info: { icon: Info, tone: "info" },
  warning: { icon: TriangleAlert, tone: "warning" },
  danger: { icon: CircleAlert, tone: "danger" },
  success: { icon: CheckCircle2, tone: "success" },
  neutral: { icon: Info, tone: "neutral" },
} as const satisfies Record<string, { icon: typeof Info; tone: TagTone }>;

/** 提示条的语气 */
type NoticeTone = keyof typeof NOTICE_TONE;

/**
 * 提示条：基于 ui/alert，一行里是图标、加粗标题、说明，右侧放操作；内容长了自动换行。
 * 配色和 Tag 共用 toneClasses。图标加文字表达语气，不只靠颜色；
 * danger 用 alert 角色，其余用 status，避免读屏乱播报。
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
  const { icon: Icon, tone: toneName } = NOTICE_TONE[tone];
  return (
    <Alert
      data-slot="notice"
      data-tone={tone}
      role={tone === "danger" ? "alert" : "status"}
      className={cn("flex items-start gap-2 px-3 py-2", toneClasses[toneName], className)}
    >
      <Icon className="mt-0.5 size-4 shrink-0" />
      <div className="flex min-w-0 flex-1 flex-wrap items-baseline gap-x-2 gap-y-0.5">
        {title && <span className="font-medium">{title}</span>}
        {children && <div className="min-w-0 opacity-80">{children}</div>}
      </div>
      {action && <div className="ml-auto shrink-0 self-center">{action}</div>}
    </Alert>
  );
}

export { Notice, type NoticeTone };
