import { useEffect, useRef, useState, type ComponentProps, type ReactNode } from "react";
import {
  Check,
  CheckCircle2,
  ChevronDown,
  ChevronUp,
  CircleAlert,
  Copy,
  Info,
  Loader2,
  ShieldAlert,
  TriangleAlert,
} from "lucide-react";
import { Link } from "react-router";

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";
import { foldText } from "@/utils/admin/trace";

/**
 * 没有管理权限时的整页提示。角色变更后端最多延迟一个缓存周期，所以给“重试”。
 * @param onRetry 点重试时重新取角色；不传则不显示按钮
 */
export function ForbiddenView({ onRetry }: { onRetry?: () => void }) {
  return (
    <main className="grid min-h-svh place-items-center p-6">
      <div className="flex max-w-sm flex-col items-center gap-3 text-center">
        <ShieldAlert className="text-destructive size-10" />
        <h1 className="text-xl font-semibold">403 没有管理权限</h1>
        <p className="text-muted-foreground text-sm">
          AI 配置管理只对管理员开放。如果你需要访问，请联系运维为你的账号开通权限。
        </p>
        <p className="text-muted-foreground text-xs">
          角色刚变更的话，权限最多要过一小段时间才生效，可以稍后重试。
        </p>
        <div className="flex items-center gap-3">
          {onRetry && (
            <Button variant="outline" size="sm" onClick={onRetry}>
              重试
            </Button>
          )}
          <Link to="/" className="text-primary text-sm hover:underline">
            返回我的画布
          </Link>
        </div>
      </div>
    </main>
  );
}

/** 与 Input 同一套外观的原生下拉：键盘、读屏都由浏览器负责 */
export function NativeSelect({ className, ...props }: ComponentProps<"select">) {
  return (
    <select
      className={cn(
        "border-input h-9 w-full min-w-0 rounded-lg border bg-transparent px-2.5 text-sm outline-none transition-colors focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50 aria-invalid:border-destructive dark:bg-input/30",
        className,
      )}
      {...props}
    />
  );
}

/** 表单字段：标题、控件、说明与错误 */
export function FormField({
  label,
  htmlFor,
  hint,
  error,
  required,
  children,
  className,
}: {
  label: ReactNode;
  htmlFor?: string;
  hint?: ReactNode;
  error?: string;
  required?: boolean;
  children: ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("flex min-w-0 flex-col gap-1.5", className)}>
      <Label htmlFor={htmlFor} className="text-muted-foreground text-xs">
        {label}
        {required && <span className="text-destructive">*</span>}
      </Label>
      {children}
      {error ? (
        <p className="text-destructive text-xs" role="alert">
          {error}
        </p>
      ) : (
        hint && <p className="text-muted-foreground text-xs">{hint}</p>
      )}
    </div>
  );
}

const TAG_TONE = {
  neutral: "bg-muted text-muted-foreground",
  success: "bg-emerald-500/10 text-emerald-700 dark:text-emerald-400",
  warning: "bg-amber-500/10 text-amber-700 dark:text-amber-400",
  danger: "bg-destructive/10 text-destructive",
  info: "bg-primary/10 text-primary",
} as const;

/** 小标签的语气 */
export type TagTone = keyof typeof TAG_TONE;

/** 小标签：来源、状态、kind 等（状态色没有语义令牌，所以没直接用 Badge 的变体） */
export function Tag({
  tone = "neutral",
  mono,
  className,
  children,
  title,
}: {
  tone?: TagTone;
  mono?: boolean;
  className?: string;
  children: ReactNode;
  title?: string;
}) {
  return (
    <span
      title={title}
      className={cn(
        "inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[11px] leading-4 whitespace-nowrap",
        TAG_TONE[tone],
        mono && "font-mono",
        className,
      )}
    >
      {children}
    </span>
  );
}

const NOTICE_TONE = {
  info: {
    icon: Info,
    className: "border-transparent bg-primary/10 text-foreground [&>svg]:text-primary",
  },
  warning: {
    icon: TriangleAlert,
    className:
      "border-transparent bg-amber-500/10 text-amber-900 dark:text-amber-200 [&>svg]:text-amber-600 dark:[&>svg]:text-amber-400",
  },
  danger: {
    icon: CircleAlert,
    className: "border-transparent bg-destructive/10 text-destructive [&>svg]:text-destructive",
  },
  success: {
    icon: CheckCircle2,
    className:
      "border-transparent bg-emerald-500/10 text-emerald-900 dark:text-emerald-200 [&>svg]:text-emerald-600 dark:[&>svg]:text-emerald-400",
  },
} as const;

/** 提示条的语气 */
export type NoticeTone = keyof typeof NOTICE_TONE;

/**
 * 提示条：只读说明、风险提示、错误原因都用它。
 * 图标加文字表达语气，不只靠颜色；danger 用 alert 角色，其余用 status，避免读屏乱播报。
 */
export function Notice({
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
    <Alert
      role={tone === "danger" ? "alert" : "status"}
      className={cn("text-xs", toneClass, className)}
    >
      <Icon />
      <div className="flex min-w-0 items-start gap-2">
        <div className="min-w-0 flex-1 leading-5">
          {title && <p className="font-medium">{title}</p>}
          {children && <div className={cn(title && "opacity-90")}>{children}</div>}
        </div>
        {action}
      </div>
    </Alert>
  );
}

/** 只读提示：admin 看插件页 / 渠道页时显示，说明为什么没有写操作按钮 */
export function ReadOnlyNotice({ what, className }: { what: string; className?: string }) {
  return (
    <Notice tone="info" title="只读" className={className}>
      {what}需要运维权限（super_admin）。你可以查看，不能修改。
    </Notice>
  );
}

/**
 * 二次确认框：删除、覆盖 Key、发布、回滚等不可逆动作用它，替代 window.confirm。
 * 点确认不会自动关闭，是否关闭由调用方决定（例如后端 409 时把原因留在框里）。
 */
export function ConfirmDialog({
  open,
  title,
  description,
  confirmLabel = "确认",
  destructive,
  busy,
  error,
  confirmDisabled,
  children,
  onConfirm,
  onCancel,
}: {
  open: boolean;
  title: string;
  description?: ReactNode;
  confirmLabel?: string;
  destructive?: boolean;
  /** 请求进行中：确认按钮显示转圈并禁用 */
  busy?: boolean;
  /** 就地显示的失败原因（全局 toast 之外的补充） */
  error?: string | null;
  /** 额外的禁用条件，例如“未勾选确认” */
  confirmDisabled?: boolean;
  /** 描述下方的自定义内容（配置摘要、勾选确认等） */
  children?: ReactNode;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  return (
    <AlertDialog open={open} onOpenChange={(next) => !next && !busy && onCancel()}>
      <AlertDialogContent className="data-[size=default]:sm:max-w-md">
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          {description && (
            <AlertDialogDescription render={<div />}>{description}</AlertDialogDescription>
          )}
        </AlertDialogHeader>
        {children}
        {error && <Notice tone="danger">{error}</Notice>}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={busy}>取消</AlertDialogCancel>
          <AlertDialogAction
            variant={destructive ? "destructive" : "default"}
            disabled={busy || confirmDisabled}
            onClick={onConfirm}
          >
            {busy && <Loader2 className="animate-spin" />}
            {confirmLabel}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

/** 等宽文本块，过长时折叠，点开看全文 */
export function FoldableCode({
  text,
  className,
  defaultOpen = false,
  maxChars,
}: {
  text: string;
  className?: string;
  /** 默认展开全文 */
  defaultOpen?: boolean;
  /** 超过多少字符就折叠（默认 1500）；追踪面板窄，用更小的值 */
  maxChars?: number;
}) {
  const [open, setOpen] = useState(defaultOpen);
  const { folded, preview } = foldText(text, undefined, maxChars);
  return (
    <div className={cn("flex flex-col gap-1", className)}>
      <pre className="bg-muted max-h-[32rem] overflow-auto rounded-md p-2 font-mono text-[11px] leading-5 break-all whitespace-pre-wrap">
        {open || !folded ? text : `${preview}\n…`}
      </pre>
      {folded && (
        <button
          type="button"
          className="text-primary flex items-center gap-1 self-start text-[11px] hover:underline"
          aria-expanded={open}
          onClick={() => setOpen((value) => !value)}
        >
          {open ? <ChevronUp className="size-3" /> : <ChevronDown className="size-3" />}
          {open ? "收起" : `展开全部（${text.length} 字符）`}
        </button>
      )}
    </div>
  );
}

/**
 * 复制按钮：点击复制文本，短暂显示“已复制”。剪贴板不可用（非 https、被拒绝）时静默不复制，
 * 按钮文案不变，用户可以自己选中文字。这里不弹 toast：复制不是请求。
 */
export function CopyButton({
  text,
  label = "复制",
  className,
  iconOnly,
}: {
  text: string;
  label?: string;
  className?: string;
  /** 只显示图标（用 aria-label 与 title 说明） */
  iconOnly?: boolean;
}) {
  const [copied, setCopied] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current);
    },
    [],
  );
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text);
    } catch {
      return;
    }
    setCopied(true);
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => setCopied(false), 1500);
  };
  return (
    <Button
      type="button"
      variant="ghost"
      size={iconOnly ? "icon-xs" : "xs"}
      className={className}
      aria-label={iconOnly ? label : undefined}
      title={iconOnly ? label : undefined}
      onClick={() => void copy()}
    >
      {copied ? <Check /> : <Copy />}
      {!iconOnly && (copied ? "已复制" : label)}
    </Button>
  );
}

/** 时间戳 → 本地时间；解析失败原样返回 */
export const formatTime = (value: string | null | undefined) => {
  if (!value) return "-";
  const time = Date.parse(value);
  return Number.isNaN(time) ? value : new Date(time).toLocaleString();
};
