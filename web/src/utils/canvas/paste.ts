/** 文本节点正文的字数上限：粘贴和手动编辑共用，免得一份画布被几十万字撑大 */
export const TEXT_BODY_MAX = 20000;

/** 截图一类没有真名字的图片，起的统一名字 */
const PASTED_IMAGE_NAME = "粘贴图片";

/** 浏览器给截图起的通用名：空的，或者统一叫 image.xxx */
const GENERIC_IMAGE_NAME = /^image\.[a-z0-9]+$/i;

/** 剪贴板里这里用得到的部分，真实的 DataTransfer 和测试里的假对象都满足 */
export type ClipboardLike = {
  /** 剪贴板里的文件 */
  files: ArrayLike<File>;
  /** 剪贴板里有哪些格式，带文件时含 "Files" */
  types: readonly string[];
  /** 按格式取内容 */
  getData: (type: string) => string;
};

/** 读出来的剪贴板内容：文件和文字至多一样有值 */
export type ClipboardContent = {
  /** 文件，没有为空数组 */
  files: File[];
  /** 纯文字，换行统一成 \n；没有或只有空白为空串 */
  text: string;
};

/** 粘贴下来要怎么办：粘回复制的节点、当成新内容落到画布、或不管 */
export type PasteDecision = "nodes" | "content" | "ignore";

/**
 * 读剪贴板里的内容。文件和文字同时有时一般取文件（网页上复制图片、文件管理器里复制文件）；
 * 唯独从 Word / Excel 这类富文本复制时，文字旁边会跟一张渲染出来的图，那是用户不想要的，取文字。
 * @param data 粘贴事件里的 clipboardData
 * @returns 文件或文字，两样都没有时都为空
 */
export function readClipboard(data: ClipboardLike): ClipboardContent {
  const files = Array.from(data.files);
  const raw = data.getData("text/plain").replace(/\r\n?/g, "\n");
  const text = raw.trim() ? raw : "";
  const richText = !!text && data.types.includes("text/html");
  if (files.length > 0 && !richText) return { files, text: "" };
  return { files: [], text };
}

/**
 * 判断一次粘贴是粘回复制的节点还是外部内容。
 * 复制节点时会把节点文字写进系统剪贴板，粘贴时文字对得上才算同一份；
 * 剪贴板是空的也按节点算，那是写剪贴板失败时的兜底。
 * @param content 读出来的剪贴板内容
 * @param copiedText 上次复制节点时写进剪贴板的文字
 * @param hasCopiedNodes 画布里是否留着复制下来的节点
 */
export function decidePaste(
  content: ClipboardContent,
  copiedText: string,
  hasCopiedNodes: boolean,
): PasteDecision {
  if (content.files.length > 0) return "content";
  if (hasCopiedNodes && (content.text === "" || content.text === copiedText)) return "nodes";
  return content.text ? "content" : "ignore";
}

/**
 * 复制节点时写进系统剪贴板的文字：文本节点是正文，其他节点是名字。
 * 这样粘到别的应用里有东西可看，粘回画布时也能认出还是这一份。
 * @param nodes 被复制的节点数据
 */
export function clipboardTextOf(
  nodes: { kind: string; label: string; text?: string | null }[],
): string {
  return nodes
    .map((node) => (node.kind === "script" && node.text ? node.text : node.label))
    .join("\n");
}

/**
 * 粘贴进来的文件起什么名：截图没有真名字，统一叫「粘贴图片」，其余照旧。
 * @param file 粘贴的文件
 * @returns 带扩展名的文件名
 */
export function clipFileName(file: File): string {
  const name = file.name ?? "";
  if (!file.type.startsWith("image/") || (name && !GENERIC_IMAGE_NAME.test(name))) return name;
  const ext = name.split(".")[1] ?? file.type.split("/")[1] ?? "png";
  return `${PASTED_IMAGE_NAME}.${ext}`;
}

/**
 * 把文字卡在正文上限以内，按字符数截（不拆开表情那种两个码元的字）。
 * @param text 原文
 * @returns 截好的文字和有没有被截
 */
export function clipText(text: string): { text: string; clipped: boolean } {
  const chars = [...text];
  if (chars.length <= TEXT_BODY_MAX) return { text, clipped: false };
  return { text: chars.slice(0, TEXT_BODY_MAX).join(""), clipped: true };
}
