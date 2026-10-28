import { MAX_ATTACH_IMAGES } from "./image-files";

/** 附件托盘里一张图的状态：上传中 / 已上传 / 上传失败 */
export type TrayStatus = "uploading" | "done" | "error";

/** 上传完成后拿到的素材信息（缩略图和发送都用它） */
export type TrayAsset = {
  id: string;
  /** 素材的稳定访问地址（/files/*，对象存储在访问时现签名跳转，不会过期） */
  url: string;
  width: number | null;
  height: number | null;
  mimeType: string;
};

/** 托盘里一项的种类：图片（缩略图）或文档（文件卡片，.txt / .md） */
export type TrayKind = "image" | "document";

/** 输入框上方托盘里的一项：一张图或一个文档 */
export type TrayItem = {
  /** 托盘内的本地 id（不是素材 id） */
  id: string;
  /** 展示名：原文件名，没有就是「图片 N」 */
  name: string;
  /** 种类，缺省是图片（旧代码和旧数据都没有这个字段） */
  kind?: TrayKind;
  /** 文件大小（字节）：文档卡片上显示 */
  size?: number;
  /** 本地预览地址（URL.createObjectURL），上传前后都能立刻显示；文档没有，是空串 */
  previewUrl: string;
  status: TrayStatus;
  /** 上传进度 0–100 */
  progress: number;
  /** 上传完成后才有 */
  asset?: TrayAsset;
  /** 失败原因，给悬停提示用 */
  error?: string;
};

/** 是不是文档附件 */
export const isDocument = (item: Pick<TrayItem, "kind">): boolean => item.kind === "document";

/** 托盘里图片的个数：图片名额（9 张）只数图片，文档另算 */
export const imageCount = (items: readonly TrayItem[]): number =>
  items.filter((i) => !isDocument(i)).length;

/** 托盘里文档的个数 */
export const documentCount = (items: readonly TrayItem[]): number =>
  items.filter(isDocument).length;

/**
 * 托盘里的内容让「发送」不能点的原因，没有返回 null。
 * 失败排在上传中前面：失败需要用户动手（重试或移除），上传中只要等。
 * 「看不到图片」只因为图片拦：只有文档时，不能看图的模型照样能发。
 */
export function attachmentBlockReason(items: readonly TrayItem[], vision: boolean): string | null {
  if (items.some((i) => i.status === "error")) return "有附件上传失败，重试或移除后再发送";
  if (items.some((i) => i.status === "uploading")) return "附件上传中…";
  if (!vision && imageCount(items) > 0) return "当前模型看不到图片，换一个能看图的模型或移除图片";
  return null;
}

/** 发给后端的附件：只带已上传完成的，按托盘顺序，同一个素材只发一次 */
export function toAttachmentsPayload(items: readonly TrayItem[]): { asset_id: string }[] {
  const seen = new Set<string>();
  const out: { asset_id: string }[] = [];
  for (const { asset } of items) {
    if (!asset || seen.has(asset.id)) continue;
    seen.add(asset.id);
    out.push({ asset_id: asset.id });
  }
  return out;
}

/** 托盘已有 current 张图、又来了 incoming 张：能收几张，多出几张没收（只数图片） */
export function clipToLimit(
  current: number,
  incoming: number,
): { accept: number; clipped: number } {
  const accept = Math.max(0, Math.min(incoming, MAX_ATTACH_IMAGES - current));
  return { accept, clipped: incoming - accept };
}
