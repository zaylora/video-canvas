import axios from "axios";

import type { importCanvas } from "@/api/canvas";
import type { CanvasImportResultDto } from "@/api/canvas/type";

import { IMPORT_TOO_LARGE_MESSAGE, MAX_IMPORT_BYTES, precheckImportText } from "./transfer-file";

/** 读取所选文件失败时给用户的提示 */
const READ_FAILED_MESSAGE = "读取文件失败";

/** 导入报告，列表页弹窗和流程共用 */
export type ImportReport = CanvasImportResultDto["report"];

/** 导入流程需要的外部能力，由调用方注入，流程本身不依赖 React 和浏览器 */
export type CanvasImportDeps = {
  /** 用户选的文件；text() 由平台读取并去掉 UTF-8 BOM */
  file: { size: number; text(): Promise<string> };
  /** 向后端提交导出文档；signal 用来中止请求 */
  importCanvas: typeof importCanvas;
  /** 取消信号；不传表示不可取消 */
  signal?: AbortSignal;
};

/** 一次导入的结果 */
export type ImportOutcome =
  /** 没通过本地预检，没有发请求；message 可直接展示给用户 */
  | { kind: "invalid"; message: string }
  /** 请求失败；原因已由请求层弹出全局提示，调用方不要再提示 */
  | { kind: "error" }
  /** 用户取消了；请求层不会为取消弹提示，调用方告诉用户「已取消」，并刷新列表（取消可能恰好发生在提交之后） */
  | { kind: "cancelled" }
  /** 导入成功且没有需要注意的项，可直接打开新画布 */
  | { kind: "opened"; canvasId: string; title: string }
  /** 导入成功但有需要注意的项，先给用户看报告 */
  | { kind: "report"; canvasId: string; title: string; report: ImportReport };

/** 导入报告里各类需要注意的项的数量 */
export type ImportReportSummary = {
  /** 找不到的素材数 */
  missing: number;
  /** 被丢弃的连线数 */
  edgesDropped: number;
  /** 需要注意的项数；被丢弃的连线不论多少根都只算 1 项 */
  total: number;
};

/**
 * 统计导入报告里需要注意的项。
 * @param report 导入报告
 * @returns 各类数量和弹窗标题用的项数
 */
export function summarizeImportReport(report: ImportReport): ImportReportSummary {
  const missing = report.assets.missing.length;
  const { edgesDropped } = report;
  return { missing, edgesDropped, total: missing + (edgesDropped > 0 ? 1 : 0) };
}

/**
 * 导入一个画布文件：先在本地预检，再提交，最后按报告决定直接打开还是先展示报告。
 * 为什么先比大小再读内容：超大文件没必要读进内存。
 * 读文件失败返回 invalid，请求失败只返回 error，用户取消返回 cancelled，都不向外抛错。
 * 取消时响应恰好已经到了（画布已建好）按成功处理，不返回 cancelled。
 * @param deps 文件和注入的外部能力
 * @returns 导入结果，由调用方决定提示和跳转
 */
export async function runCanvasImport(deps: CanvasImportDeps): Promise<ImportOutcome> {
  const { file, signal } = deps;
  if (file.size > MAX_IMPORT_BYTES) return { kind: "invalid", message: IMPORT_TOO_LARGE_MESSAGE };

  let text: string;
  try {
    text = await file.text();
  } catch {
    /**
     * 读文件不经过请求层，没有全局提示；文件被移走、权限变化等都会读失败，要就地告诉用户。
     */
    return { kind: "invalid", message: READ_FAILED_MESSAGE };
  }
  // 读文件期间用户点了取消：不必再发请求
  if (signal?.aborted) return { kind: "cancelled" };

  const checked = precheckImportText(file.size, text);
  if (!checked.ok) return { kind: "invalid", message: checked.message };

  let result: CanvasImportResultDto;
  try {
    result = await deps.importCanvas(checked.document, signal);
  } catch (error) {
    // 主动取消不是失败：请求层不会为它弹提示
    if (axios.isCancel(error)) return { kind: "cancelled" };
    /**
     * 请求失败时请求层已弹出全局错误提示，这里不再重复提示（见 coding-standards §3），只终止流程。
     */
    return { kind: "error" };
  }

  const { canvas, report } = result;
  if (summarizeImportReport(report).total === 0) {
    return { kind: "opened", canvasId: canvas.id, title: canvas.title };
  }
  return { kind: "report", canvasId: canvas.id, title: canvas.title, report };
}
