import type { CanvasNodeData } from "@/types";

/**
 * 节点是不是正在把本地文件传上去：生成中（status 为 running）并且带着上传进度。
 * 普通的生成中没有 uploadProgress，所以靠它区分「在传文件」和「在跑生成任务」。
 * 上传中的节点还没有正式素材，不能被引用，也不存进画布。
 * @param data 节点数据
 */
export function isUploading(data: Pick<CanvasNodeData, "status" | "uploadProgress">): boolean {
  return data.status === "running" && data.uploadProgress !== undefined;
}

/**
 * 节点上失败的是不是「上传」：上传来的素材、没有入过库（没有素材 id）、状态为失败。
 * 上传成功后（有素材 id）再出的错是生成失败。
 * @param data 节点数据
 */
export function isUploadFailed(
  data: Pick<CanvasNodeData, "status" | "uploaded" | "assetId">,
): boolean {
  return data.status === "error" && !!data.uploaded && !data.assetId;
}
