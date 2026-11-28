import { useEffect, useState } from "react";

import { getCollaborationModePreview, setCollaborationMode } from "@/api/canvas/collaboration";
import type { CanvasModePreviewDto } from "@/api/canvas/type";
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
import { ApiError } from "@/utils/requests/request";

export function CanvasModeDialog({
  canvasId,
  active,
  open,
  onOpenChange,
  beforeConfirm,
  onChanged,
}: {
  canvasId: string;
  active: boolean;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  beforeConfirm?: () => Promise<boolean>;
  onChanged: () => void;
}) {
  const [impact, setImpact] = useState<CanvasModePreviewDto | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    if (!open) return;
    let current = true;
    void getCollaborationModePreview(canvasId, active).then(
      (result) => {
        if (current) setImpact(result);
      },
      (failure: unknown) => {
        if (current) setError(failure instanceof Error ? failure.message : "无法读取影响范围。");
      },
    );
    return () => {
      current = false;
    };
  }, [open, canvasId, active, attempt]);

  const close = () => {
    setImpact(null);
    setError(null);
    onOpenChange(false);
  };

  const confirm = async () => {
    if (busy || !impact) return;
    setBusy(true);
    setError(null);
    try {
      if (beforeConfirm && !(await beforeConfirm())) {
        setError("请先同步修改并处理冲突。");
        return;
      }
      await setCollaborationMode(canvasId, impact.revision, active, impact.preview);
      close();
      onChanged();
    } catch (failure) {
      if (failure instanceof ApiError && failure.status === 409 && failure.code === 30002) {
        setImpact(null);
        setAttempt((count) => count + 1);
        setError("画布版本或成员范围已变化，请核对更新后的影响并重新确认。");
      } else {
        setError(failure instanceof Error ? failure.message : "切换协作模式失败。");
      }
    } finally {
      setBusy(false);
    }
  };

  return (
    <AlertDialog open={open} onOpenChange={(next) => !next && !busy && close()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{active ? "开启实时协作？" : "关闭这张画布的协作？"}</AlertDialogTitle>
          <AlertDialogDescription>
            {impact
              ? `「${impact.title}」版本 ${impact.revision}。${impact.editors} 位编辑者、${impact.viewers} 位查看者。${active ? "已有编辑者可重新加入，查看者继续只读。" : "成员仍可查看最后已同步的内容。当前编辑会话和未提交草稿将失效。"}`
              : error
                ? "需要读取当前影响范围才能确认。"
                : "正在读取当前影响范围…"}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {error && (
          <p role="alert" className="text-destructive text-sm">
            {error}
          </p>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={busy}>取消</AlertDialogCancel>
          <AlertDialogAction
            variant={active ? "default" : "destructive"}
            disabled={busy || !impact}
            onClick={() => void confirm()}
          >
            {busy ? "正在处理…" : active ? "确认开启协作" : "确认关闭协作"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
