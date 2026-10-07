import type { DropEvent } from "react-dropzone";

import {
  normalizeEntryPath,
  readDroppedEntries,
  type FsEntryLike,
} from "@/utils/admin/agent-skill-import";

import type { PickedItem } from "./use-skill-import";

/** 给 File 挂上相对路径（和 file-selector 的 path 约定一致），react-dropzone 原样把 File 交回 onDrop */
function withPath(file: File, path: string): File {
  Object.defineProperty(file, "path", { value: path, configurable: true });
  return file;
}

/** 取 File 上的相对路径：拖入时挂的 path，其次 <input webkitdirectory> 的 webkitRelativePath，最后是文件名 */
export function pathOf(file: File): string {
  const attached = (file as File & { path?: unknown }).path;
  if (typeof attached === "string" && attached) return normalizeEntryPath(attached);
  return normalizeEntryPath(file.webkitRelativePath || file.name);
}

/** File 列表转成导入要的“文件 + 相对路径” */
export const toPicked = (files: readonly File[]): PickedItem[] =>
  files.map((file) => ({ file, path: pathOf(file) }));

/**
 * 给 react-dropzone 的 getFilesFromEvent：
 * 拖入时用 webkitGetAsEntry 递归读目录（分批 readEntries）；选择器选的文件夹靠 webkitRelativePath；
 * 拖动经过（dragenter / dragover）时浏览器不给文件内容，返回空列表，只用来让 isDragActive 亮起。
 * 注意 dataTransfer.items 只在事件同步阶段可读，所以条目必须在第一个 await 之前取出来。
 */
export async function getFilesFromEvent(
  event: DropEvent | FileSystemFileHandle[],
): Promise<File[]> {
  if (Array.isArray(event)) return [];
  const dataTransfer = (event as DragEvent).dataTransfer;
  if (dataTransfer) {
    if (event.type !== "drop") return [];
    const entries = Array.from(dataTransfer.items ?? [])
      .filter((item) => item.kind === "file")
      .map((item) => item.webkitGetAsEntry?.() ?? null)
      .filter((entry): entry is FileSystemEntry => entry !== null);
    if (entries.length === 0) {
      return Array.from(dataTransfer.files).map((file) => withPath(file, pathOf(file)));
    }
    const read = await readDroppedEntries(entries as unknown as FsEntryLike[]);
    return read.map(({ file, path }) => withPath(file, path));
  }
  const input = (event as Event).target as HTMLInputElement | null;
  return Array.from(input?.files ?? []).map((file) => withPath(file, pathOf(file)));
}
