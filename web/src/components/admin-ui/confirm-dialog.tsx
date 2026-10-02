import { Loader2 } from "lucide-react";
import { useState, type ReactNode } from "react";

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
import { openDialog, type DialogControl } from "@/store/dialog";
import { errorMessage } from "@/utils/admin/errors";

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
  onExited,
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
  /** 退出动画播完（store 管理的弹窗用它从 store 移除） */
  onExited?: () => void;
}) {
  return (
    <AlertDialog
      open={open}
      onOpenChange={(next) => !next && !busy && onCancel()}
      onOpenChangeComplete={(next) => !next && onExited?.()}
    >
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

export type ConfirmOptions = {
  title: string;
  description?: ReactNode;
  confirmLabel?: string;
  destructive?: boolean;
  /** 描述下方的自定义内容 */
  children?: ReactNode;
  /** 前端已能判断后端一定会拒绝时的原因：显示在框里并禁用确认 */
  blockReason?: string | null;
  /**
   * 点确认后执行的操作。执行中按钮转圈；抛错时对话框不关，失败原因留在框里（全局 toast 照常弹）；
   * 成功后自动关闭。不传则点确认直接关闭。
   */
  onConfirm?: () => Promise<unknown> | unknown;
};

function StoreConfirm({
  onConfirm,
  blockReason,
  resolve,
  open,
  onClose,
  onExited,
  ...rest
}: ConfirmOptions & DialogControl & { resolve: (ok: boolean) => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const done = (ok: boolean) => {
    resolve(ok);
    onClose();
  };
  const confirmNow = async () => {
    setBusy(true);
    setError(null);
    try {
      await onConfirm?.();
      done(true);
    } catch (cause) {
      setError(errorMessage(cause, "操作失败"));
    } finally {
      setBusy(false);
    }
  };
  return (
    <ConfirmDialog
      {...rest}
      open={open}
      busy={busy}
      error={blockReason ?? error}
      confirmDisabled={!!blockReason}
      onConfirm={() => void confirmNow()}
      onCancel={() => done(false)}
      onExited={onExited}
    />
  );
}

/**
 * 用全局弹窗 store 打开一个确认框，页面不用自己维护“目标 + busy + error”。
 * @returns 用户确认且 onConfirm 成功时为 true；取消为 false
 * @example
 * void confirm({ title: "停用渠道？", destructive: true, onConfirm: () => updateChannel(key, { enabled: false }) });
 */
function confirm(options: ConfirmOptions): Promise<boolean> {
  return new Promise((resolve) => {
    openDialog(StoreConfirm, { ...options, resolve });
  });
}

export { ConfirmDialog, confirm };
