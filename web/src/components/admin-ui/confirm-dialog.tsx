import { Loader2 } from "lucide-react";
import type { ReactNode } from "react";

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

import { Notice } from "@/components/admin-ui/notice";

/**
 * 二次确认框：删除、覆盖 Key、发布、回滚等不可逆动作用它，替代 window.confirm。
 * 点确认不会自动关闭，是否关闭由调用方决定（例如后端 409 时把原因留在框里）。
 */
function ConfirmDialog({
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

export { ConfirmDialog };
