/** 导入文件大小上限，字节；与后端限制一致，前端先拦一道省得白传 */
export const MAX_IMPORT_BYTES = 10 * 1024 * 1024;

/** 文件超过 MAX_IMPORT_BYTES 时给用户的提示；读文件前也要用，所以单独导出 */
export const IMPORT_TOO_LARGE_MESSAGE = "文件过大，上限 10 MB";

/** 导出文件的格式标识，和后端写进文档的 format 一致 */
const EXPORT_FORMAT = "video-canvas/canvas";

/** 当前前端能读懂的文件格式版本 */
const SUPPORTED_VERSION = 1;

/** 文件名里不允许出现的字符：Windows 保留字符和控制字符 */
// oxlint-disable-next-line no-control-regex
const INVALID_FILE_NAME_CHARS = /[\\/:*?"<>|\u0000-\u001f\u007f]/g;

/**
 * 由画布标题生成导出文件名。
 * @param title 画布标题
 * @returns 带 .canvas.json 后缀、可安全存盘的文件名
 */
export function exportFileName(title: string): string {
  const name = title.trim().replace(INVALID_FILE_NAME_CHARS, "_");
  return `${name || "未命名画布"}.canvas.json`;
}

/**
 * 把数据存成 JSON 文件下载到本地。
 * 为什么延迟回收对象地址：部分浏览器在 click 后异步取数据，立刻回收会得到空文件。
 * @param fileName 保存的文件名
 * @param data 要序列化的数据
 */
export function downloadJson(fileName: string, data: unknown): void {
  const blob = new Blob([JSON.stringify(data, null, 2)], { type: "application/json" });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = fileName;
  link.rel = "noopener";
  document.body.append(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 10_000);
}

/** 导入前预检的结果 */
export type ImportPrecheckResult =
  /** 通过预检，document 是解析后的内容，原样交给后端 */
  | { ok: true; document: unknown }
  /** 未通过，message 是可直接展示给用户的原因 */
  | { ok: false; message: string };

/**
 * 导入前在本地预检文件：大小、是否 JSON、是否本系统的画布文件、版本是否支持。
 * 只做能立刻告诉用户的粗筛，细节校验仍以后端为准。
 * @param size 文件字节数
 * @param text 文件文本内容
 * @returns 通过时带解析后的文档，否则带失败原因
 */
export function precheckImportText(size: number, text: string): ImportPrecheckResult {
  if (size > MAX_IMPORT_BYTES) return { ok: false, message: IMPORT_TOO_LARGE_MESSAGE };

  let document: unknown;
  try {
    document = JSON.parse(text);
  } catch {
    return { ok: false, message: "不是有效的 JSON 文件" };
  }

  if (
    typeof document !== "object" ||
    document === null ||
    Array.isArray(document) ||
    (document as { format?: unknown }).format !== EXPORT_FORMAT
  ) {
    return { ok: false, message: "不是本系统导出的画布文件" };
  }

  const { version } = document as { version?: unknown };
  if (typeof version !== "number" || version > SUPPORTED_VERSION) {
    return { ok: false, message: "文件来自更新版本，请升级后再导入" };
  }

  return { ok: true, document };
}
