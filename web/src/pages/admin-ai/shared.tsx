import { useState, type ComponentProps, type ReactNode } from "react";
import { ChevronDown, ChevronUp, ShieldAlert } from "lucide-react";
import { Link } from "react-router";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";
import { foldText } from "@/utils/admin/trace";

/** 没有管理权限时的整页提示 */
export function ForbiddenView() {
  return (
    <main className="grid min-h-svh place-items-center p-6">
      <div className="flex max-w-sm flex-col items-center gap-3 text-center">
        <ShieldAlert className="text-destructive size-10" />
        <h1 className="text-xl font-semibold">403 没有管理权限</h1>
        <p className="text-muted-foreground text-sm">
          AI
          配置管理只对管理员开放。如果你需要访问，请联系运维为你的账号开通权限。
        </p>
        <Link to="/" className="text-primary text-sm hover:underline">
          返回我的画布
        </Link>
      </div>
    </main>
  );
}

/** 与 Input 同一套外观的原生下拉：键盘、读屏都由浏览器负责 */
export function NativeSelect({
  className,
  ...props
}: ComponentProps<"select">) {
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

/** 小标签：来源、状态、kind 等 */
export function Tag({
  tone = "neutral",
  mono,
  className,
  children,
  title,
}: {
  tone?: keyof typeof TAG_TONE;
  mono?: boolean;
  className?: string;
  children: ReactNode;
  title?: string;
}) {
  return (
    <span
      title={title}
      className={cn(
        "inline-flex items-center rounded-md px-1.5 py-0.5 text-[11px] leading-4 whitespace-nowrap",
        TAG_TONE[tone],
        mono && "font-mono",
        className,
      )}
    >
      {children}
    </span>
  );
}

/** 二次确认框：危险操作（删除、开启风险开关）用它，比 window.confirm 更醒目 */
export function ConfirmDialog({
  open,
  title,
  description,
  confirmLabel = "确认",
  destructive,
  onConfirm,
  onCancel,
}: {
  open: boolean;
  title: string;
  description: ReactNode;
  confirmLabel?: string;
  destructive?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  return (
    <Dialog open={open} onOpenChange={(next) => !next && onCancel()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription render={<div />}>{description}</DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={onCancel}>
            取消
          </Button>
          <Button
            variant={destructive ? "destructive" : "default"}
            onClick={onConfirm}
          >
            {confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** 等宽文本块，过长时折叠，点开看全文 */
export function FoldableCode({
  text,
  className,
}: {
  text: string;
  className?: string;
}) {
  const [open, setOpen] = useState(false);
  const { folded, preview } = foldText(text);
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
          {open ? (
            <ChevronUp className="size-3" />
          ) : (
            <ChevronDown className="size-3" />
          )}
          {open ? "收起" : `展开全部（${text.length} 字符）`}
        </button>
      )}
    </div>
  );
}

/** 时间戳 → 本地时间；解析失败原样返回 */
export const formatTime = (value: string | null | undefined) => {
  if (!value) return "-";
  const time = Date.parse(value);
  return Number.isNaN(time) ? value : new Date(time).toLocaleString();
};
