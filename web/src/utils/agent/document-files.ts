/** 一条消息最多附几个文档（故事、剧本）；图片的 9 张另算 */
export const MAX_ATTACH_DOCUMENTS = 3;
/** 单个文档最大字节数：消息带给模型的正文有上限，文件再大也放不下 */
export const MAX_DOCUMENT_BYTES = 200_000;

/** 能当文档附件的扩展名 */
const DOCUMENT_EXT = /\.(txt|md|markdown)$/i;

/** 文件大小的显示：不足 1KB 用字节，之后 KB，≥1MB 用 MB，KB / MB 保留两位小数 */
export function formatFileSize(bytes: number): string {
  if (bytes < 1024) return `${Math.max(0, Math.round(bytes))}B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(2)}KB`;
  return `${(bytes / (1024 * 1024)).toFixed(2)}MB`;
}

/** 文件卡片上的类型标签：.md / .markdown 是 MD，其余（含没有扩展名）是 TXT */
export function documentLabel(name: string): "TXT" | "MD" {
  return /\.(md|markdown)$/i.test(name) ? "MD" : "TXT";
}

/**
 * 从用户选的文件里挑出能当文档附件的：只收 .txt / .md、不能是空文件、不能超过 20 万字节，
 * 托盘里已有 current 个文档时只收剩下的名额。
 * @returns accepted 要上传的；wrongType / tooLarge / empty / clipped 各自被拒绝的个数，调用方据此提示
 */
export function pickDocumentFiles(
  files: ArrayLike<File> | null | undefined,
  current: number,
): { accepted: File[]; wrongType: number; tooLarge: number; empty: number; clipped: number } {
  const out = { accepted: [] as File[], wrongType: 0, tooLarge: 0, empty: 0, clipped: 0 };
  for (const f of Array.from(files ?? [])) {
    if (!DOCUMENT_EXT.test(f.name)) out.wrongType += 1;
    else if (f.size === 0) out.empty += 1;
    else if (f.size > MAX_DOCUMENT_BYTES) out.tooLarge += 1;
    else if (current + out.accepted.length >= MAX_ATTACH_DOCUMENTS) out.clipped += 1;
    else out.accepted.push(f);
  }
  return out;
}
