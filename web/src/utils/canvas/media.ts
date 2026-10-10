import { UPLOAD_SIZE_LIMIT } from "@/constants/canvas";
import type { MediaType, UploadTaken } from "@/types";

/**
 * blob: 开头的地址是本地文件临时挂出来的，
 * 删节点时得还回去，不然那份文件一直占着内存直到刷新。
 */
export function releaseObjectUrl(src?: string | null) {
  if (src?.startsWith("blob:")) URL.revokeObjectURL(src);
}

/** 按 MIME 前缀认种类，三类之外的文件一概不收 */
function getMediaType(file: File): MediaType | null {
  if (file.type.startsWith("image/")) return "image";
  if (file.type.startsWith("video/")) return "video";
  if (file.type.startsWith("audio/")) return "audio";
  return null;
}

/**
 * 收下一个选中的文件：认种类、卡大小。
 * 不合规时给一句能直接摆给用户看的话。
 */
export function takeUploadFile(file: File): UploadTaken {
  const mediaType = getMediaType(file);
  if (!mediaType) return { error: "只收图片、视频和音频，换个文件试试" };

  const limit = UPLOAD_SIZE_LIMIT[mediaType];
  if (file.size > limit) {
    return { error: `文件超过 ${Math.round(limit / 1024 / 1024)}MB，换个小点的` };
  }

  return { mediaType };
}
