import type { exportCanvas } from "@/api/canvas";

import { exportFileName } from "./transfer-file";

/** 导出流程需要的外部能力，由调用方注入，流程本身不依赖 React 和浏览器 */
export type CanvasExportDeps = {
  /** 要导出的画布 ID */
  canvasId: string;
  /** 画布标题，用来生成文件名和提示文案 */
  title: string;
  /** 立即保存，返回保存后是否已经没有未保存的内容 */
  saveNow: () => Promise<boolean>;
  /** 向后端请求导出文档 */
  exportCanvas: typeof exportCanvas;
  /** 把文档存成文件下载到本地 */
  download: (fileName: string, data: unknown) => void;
  /** 给用户的提示 */
  notify: {
    /** 成功提示 */
    success(message: string): void;
    /** 需要留意但不影响结果的提示 */
    warning(message: string): void;
    /** 失败提示 */
    error(message: string): void;
  };
};

/**
 * 导出当前画布为文件：先保存，再请求导出，最后下载。
 * 为什么先保存：后端只导出已保存的内容，不先存的话没保存的改动会悄悄缺失；存不上就不导出。
 * 所有失败都在内部提示，不向外抛错。
 * @param deps 画布信息和注入的外部能力
 */
export async function runCanvasExport(deps: CanvasExportDeps): Promise<void> {
  const { canvasId, title, saveNow, notify } = deps;

  if (!(await saveNow())) {
    notify.error("还有内容没保存上，请保存成功后再导出");
    return;
  }

  let result: Awaited<ReturnType<typeof exportCanvas>>;
  try {
    result = await deps.exportCanvas(canvasId);
  } catch {
    notify.error("导出失败，请稍后重试");
    return;
  }

  deps.download(exportFileName(title), result.document);
  notify.success(`已导出「${title}」`);
  if (result.skippedAssets > 0) {
    notify.warning(
      `有 ${result.skippedAssets} 个素材文件读不到，已跳过，对应节点导入后素材位置为空`,
    );
  }
}
