import type { LibraryItemDto } from "@/api/asset-library/type";
import type { CanvasNodeData } from "@/types";

import type { LibraryApi } from "../assets/library-actions";

/**
 * 节点功能区要不要出「加入资产库」：图片、视频、音频节点，有服务端素材 ID 且 status 为 done。
 * 不看来源：生成的、上传的、扩图 / 修剪 / 截帧合成后上传的产物都可以手动存进库。
 * @param data 节点数据
 */
export function canAddToLibrary(data: CanvasNodeData): boolean {
  return (
    (data.kind === "image" || data.kind === "video" || data.kind === "audio") &&
    !!data.assetId &&
    data.status === "done"
  );
}

/**
 * 把节点的素材加入资产库；已在库里时后端幂等返回现有归属。
 * 失败由请求层弹提示，这里抛出让文件夹选择器保持打开。
 * @param data 节点数据
 * @param folderId 目标文件夹，null 是根目录
 * @param api 接口
 */
export async function addNodeToLibrary(
  data: CanvasNodeData,
  folderId: string | null,
  api: Pick<LibraryApi, "addToLibrary">,
): Promise<LibraryItemDto> {
  if (!canAddToLibrary(data) || !data.assetId) throw new Error("node has no library-ready asset");
  return api.addToLibrary(data.assetId, folderId);
}
