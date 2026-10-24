import { useCallback, useRef, useState } from "react";
import { useNavigate } from "react-router";
import { toast } from "sonner";

import { importCanvas } from "@/api/canvas";
import { runCanvasImport, type ImportReport } from "@/utils/canvas/import-flow";

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
 * 请求失败的提示由请求层统一弹，这里只处理本地预检失败和成功后的去向。
 * @param onImported 导入成功（画布已创建）后调用，用来刷新列表
 */
export function useCanvasImport(onImported: () => void) {
  const navigate = useNavigate();
  const [importing, setImporting] = useState(false);
  /** 同步的进行中标记；state 要等渲染后才变，挡不住紧挨着的第二次选择 */
  const busy = useRef(false);
  const [pending, setPending] = useState<PendingImportReport | null>(null);

  const pick = useCallback(
    async (file: File) => {
      if (busy.current) return;
      busy.current = true;
      setImporting(true);
      try {
        const outcome = await runCanvasImport({ file, importCanvas });
        if (outcome.kind === "invalid") {
          toast.error(outcome.message);
          return;
        }
        if (outcome.kind === "error") return;

        onImported();
        if (outcome.kind === "opened") {
          toast.success(`已导入「${outcome.title}」`);
          navigate(`/canvas/${outcome.canvasId}`);
        } else {
          setPending(outcome);
        }
      } finally {
        busy.current = false;
        setImporting(false);
      }
    },
    [navigate, onImported],
  );

  /** 关闭结果弹窗；画布已经创建、列表已刷新，不需要别的善后 */
  const closeReport = useCallback(() => setPending(null), []);

  /** 打开导入出的新画布并关闭弹窗 */
  const openImported = useCallback(() => {
    if (!pending) return;
    navigate(`/canvas/${pending.canvasId}`);
    setPending(null);
  }, [navigate, pending]);

  return { importing, pick, pending, closeReport, openImported };
}
