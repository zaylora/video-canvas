/**
 * 消息的图片附件：Go 校验通过后给的 AttachmentCheck，引擎据此记事件、拼交给模型的内容。
 * 都是纯函数，不碰存储和网络。
 */

/** Go 校验通过的一张附件图（平台接口 POST /attachments/check 的出参） */
export interface AttachmentCheck {
  asset_id: string;
  /** 展示名：原文件名，没有就是「图片 N」 */
  name: string;
  mime_type: string;
  width?: number;
  height?: number;
  /** 素材的稳定访问地址（/files/*，对象存储在访问时现签名跳转，不会过期） */
  url: string;
  /** 交给模型的内容：对象存储给地址，本地磁盘给 base64 */
  image: { mime_type: string; url?: string; data?: string };
}

/** 落进 message.user 事件的附件：只有引用和展示信息，不带图片内容（图片很大，也不该进历史） */
export interface AttachmentView {
  asset_id: string;
  name: string;
  mime_type: string;
  width?: number;
  height?: number;
  url: string;
}

export const attachmentViews = (list: readonly AttachmentCheck[]): AttachmentView[] =>
  list.map(({ asset_id, name, mime_type, width, height, url }) => ({
    asset_id, name, mime_type, ...(width && { width }), ...(height && { height }), url,
  }));

/** 追加在用户消息文字后面、只给模型看的一行：让模型知道每张图的素材 id，之后要重新看图、落到画布时用 */
export const attachmentLine = (list: readonly AttachmentCheck[]): string =>
  `附件图片：${list.map((a, i) => `图${i + 1}（asset:${a.asset_id}）`).join("、")}`;

/** Pi 的输入内容：纯文字，或文字加图片块 */
export type InputContent = string | Array<{ type: "text"; text: string } | { type: "image"; data: string; mimeType: string }>;

/**
 * 交给 Pi 的用户消息内容。没有附件就是原来的字符串；有附件是「文字 + 附件说明行」加每张图一个图片块。
 * 图片块只有 data 和 mimeType：对象存储的图把地址放进 data，发请求前由模型提供方还原成真正的地址（见 context.ts restoreImageUrls）。
 */
export function inputContent(text: string, attachments: readonly AttachmentCheck[] = []): InputContent {
  if (attachments.length === 0) return text;
  return [
    { type: "text", text: `${text}\n\n${attachmentLine(attachments)}` },
    ...attachments.map((a) => ({ type: "image" as const, data: (a.image.url ?? a.image.data) as string, mimeType: a.image.mime_type })),
  ];
}
