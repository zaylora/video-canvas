import { useCallback, useRef, useState } from "react";
import { useNavigate } from "react-router";
import { toast } from "sonner";

import { importCanvas } from "@/api/canvas";
import { runCanvasImport, type ImportReport } from "@/utils/canvas/import-flow";
import { createTransferCancel, type TransferCancel } from "@/utils/canvas/transfer-cancel";

/** 导入成功但有需要注意的项时，等待用户查看的结果 */
export type PendingImportReport = {
  /** 新画布 ID */
  canvasId: string;
  /** 新画布标题 */
  title: string;
  /** 导入报告 */
  report: ImportReport;
};

/**
 * 列表页的导入画布：选文件、导入、按结果提示或跳转。导入中重复选择会被忽略。
 * 导入中可以取消：取消后提示「已取消导入」并刷新列表（可能恰好已经导入成功）。
 * 请求失败的提示由请求层统一弹，这里只处理本地预检失败和成功后的去向。
 * @param onImported 导入成功（画布已创建）后调用，用来刷新列表
 */
export function useCanvasImport(onImported: () => void) {
  const navigate = useNavigate();
  const [importing, setImporting] = useState(false);
  /** 已点取消、正在等请求收尾；弹窗据此把按钮置灰 */
  const [cancelling, setCancelling] = useState(false);
  /** 当前这次导入的取消控制器；每次导入新建一个 */
  const cancelRef = useRef<TransferCancel | null>(null);
  /** 同步的进行中标记；state 要等渲染后才变，挡不住紧挨着的第二次选择 */
  const busy = useRef(false);
  const [pending, setPending] = useState<PendingImportReport | null>(null);

  const pick = useCallback(
    async (file: File) => {
      if (busy.current) return;
      busy.current = true;
      const cancel = createTransferCancel();
      cancelRef.current = cancel;
      setImporting(true);
      try {
        const outcome = await runCanvasImport({ file, importCanvas, signal: cancel.signal });
        if (outcome.kind === "invalid") {
          toast.error(outcome.message);
          return;
        }
        if (outcome.kind === "error") return;
        if (outcome.kind === "cancelled") {
          // 取消只是不再等待：请求可能恰好在提交后才到达，画布已经建好，所以要刷新列表让用户看到
          toast.info("已取消导入");
          onImported();
          return;
        }

        onImported();
        if (outcome.kind === "opened") {
          toast.success(`已导入「${outcome.title}」`);
          navigate(`/canvas/${outcome.canvasId}`);
        } else {
          setPending(outcome);
        }
      } finally {
        busy.current = false;
        cancelRef.current = null;
        setCancelling(false);
        setImporting(false);
      }
    },
    [navigate, onImported],
  );

  /** 点「取消」：中止请求，弹窗继续锁住，等 pick 收尾后才关闭 */
  const cancel = useCallback(() => {
    if (!cancelRef.current) return;
    setCancelling(true);
    cancelRef.current.cancel();
  }, []);

  /** 关闭结果弹窗；画布已经创建、列表已刷新，不需要别的善后 */
  const closeReport = useCallback(() => setPending(null), []);

  /** 打开导入出的新画布并关闭弹窗 */
  const openImported = useCallback(() => {
    if (!pending) return;
    navigate(`/canvas/${pending.canvasId}`);
    setPending(null);
  }, [navigate, pending]);

  return { importing, cancelling, cancel, pick, pending, closeReport, openImported };
}
