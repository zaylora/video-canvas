import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";

import { exportCanvas } from "@/api/canvas";
import { runCanvasExport, type CanvasExportDeps } from "@/utils/canvas/export-flow";
import { createTransferCancel, type TransferCancel } from "@/utils/canvas/transfer-cancel";
import { downloadJson } from "@/utils/canvas/transfer-file";

/** 保存并导出当前画布，提供进度弹窗所需的状态与取消操作。 */
export function useCanvasExport(
  input: Pick<CanvasExportDeps, "canvasId" | "title" | "saveNow" | "getVersion">,
) {
  const [exporting, setExporting] = useState(false);
  const [cancelling, setCancelling] = useState(false);
  const transfer = useRef<TransferCancel | null>(null);

  useEffect(() => () => transfer.current?.cancel(), []);

  const startExport = async () => {
    if (transfer.current) return;
    const active = createTransferCancel();
    transfer.current = active;
    setExporting(true);
    try {
      const outcome = await runCanvasExport({
        ...input,
        exportCanvas,
        signal: active.signal,
        download: downloadJson,
        notify: toast,
      });
      if (outcome === "cancelled") toast.info("已取消导出");
    } finally {
      transfer.current = null;
      setCancelling(false);
      setExporting(false);
    }
  };

  const cancelExport = () => {
    if (!transfer.current || transfer.current.signal.aborted) return;
    setCancelling(true);
    transfer.current.cancel();
  };

  return { exporting, cancelling, startExport, cancelExport };
}
