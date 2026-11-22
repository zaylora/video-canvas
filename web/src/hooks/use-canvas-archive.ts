import { useCallback, useState } from "react";
import { toast } from "sonner";

import { requestCanvasArchive } from "@/api/canvas";
import { runCanvasArchive } from "@/utils/canvas/archive-flow";
import { useCurrentCanvasId } from "@/utils/canvas/canvas-id";

/** 票据地址是后端相对路径，下载前补上 API 前缀 */
function downloadUrl(url: string): string {
  const base = (import.meta.env.VITE_API_BASE_URL ?? "/api/v1").replace(/\/+$/, "");
  return `${base}${url}`;
}

/**
 * 用浏览器原生下载打开带票据的地址。
 * 不用 window.open：同源 + <a download> 才会直接存盘，否则可能新开一个空白页再被拦截。
 */
function openNative(url: string): void {
  const link = document.createElement("a");
  link.href = url;
  link.rel = "noopener";
  document.body.append(link);
  link.click();
  link.remove();
}

/**
 * 打包下载当前画布的全部素材（剧本 / 分镜表 / 角色 / 图片 / 视频 / 音频 / 成片）。
 * 只负责拿票据和触发下载，统计文案与提示在 archive-flow 里，便于单测。
 */
export function useCanvasArchive() {
  const canvasId = useCurrentCanvasId();
  const [archiving, setArchiving] = useState(false);

  const archive = useCallback(async () => {
    if (!canvasId || archiving) return;
    setArchiving(true);
    try {
      await runCanvasArchive({
        canvasId,
        request: requestCanvasArchive,
        open: (url) => openNative(downloadUrl(url)),
        notify: {
          success: (message, description) => toast.success(message, { description }),
          warning: (message) => toast.warning(message),
        },
      });
    } finally {
      setArchiving(false);
    }
  }, [archiving, canvasId]);

  return { archive, archiving };
}
