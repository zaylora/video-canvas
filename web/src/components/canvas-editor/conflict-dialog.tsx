import { useState } from "react";

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

/**
 * 保存撞上 409（画布在别的标签页或设备上改过）时弹出，必须选一个才能继续，不可点外面或按 Esc 关掉：
 * - 加载最新：丢掉本地没存上的内容
 * - 另存为新画布：本地内容存成一张副本，原画布再加载最新
 * 「以我的为准（追加新版本）」要等后端版本表落地后才开放，见 docs/design/自动保存节奏设计。
 */
export function ConflictDialog({
  open,
  onLoadLatest,
  onSaveAsCopy,
}: {
  open: boolean;
  onLoadLatest: () => void;
  onSaveAsCopy: () => Promise<void>;
}) {
  const [copying, setCopying] = useState(false);

  const saveAsCopy = async () => {
    if (copying) return;
    setCopying(true);
    try {
      await onSaveAsCopy();
    } finally {
      setCopying(false);
    }
  };

  return (
    <AlertDialog open={open}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>画布已在别处修改</AlertDialogTitle>
          <AlertDialogDescription>
            这张画布在另一个标签页或设备上被更新过，你这里还有没保存的改动。可以先把它另存为一张新画布，再加载最新内容。
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={copying} onClick={onLoadLatest}>
            加载最新（放弃本地改动）
          </AlertDialogCancel>
          <AlertDialogAction disabled={copying} onClick={() => void saveAsCopy()}>
            {copying ? "正在另存…" : "另存为新画布"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
