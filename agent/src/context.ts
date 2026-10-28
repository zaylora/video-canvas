/** 消息内容块：只列出这里用到的字段，pi 的各种内容块都能赋给它 */
export interface Part {
  type: string;
  id?: string;
  text?: string;
  thinking?: string;
  name?: string;
  arguments?: unknown;
  data?: string;
  mimeType?: string;
}

/** 这个文件处理的消息：pi 的 user / assistant / toolResult 消息都满足这个形状 */
export interface Msg {
  role: string;
  content?: string | Part[];
  toolCallId?: string;
  toolName?: string;
  stopReason?: string;
  isError?: boolean;
  timestamp?: number;
}

const IMAGE_CHARS = 1000; // 一张图按这么多字估

/** 一条消息按「1 字 1 个 Token」估的字数（偏保守，中文接近实际）；摘要压缩的触发和切点也用它 */
export function charsOf(m: Msg): number {
  const c = m.content;
  if (typeof c === "string") return c.length;
  if (!Array.isArray(c)) return 0;
  let n = 0;
  for (const p of c) {
    if (p.type === "text") n += p.text?.length ?? 0;
    else if (p.type === "image") n += IMAGE_CHARS;
    else if (p.type === "toolCall") n += JSON.stringify(p.arguments ?? {}).length + (p.name?.length ?? 0);
    else if (p.type === "thinking") n += p.thinking?.length ?? 0;
  }
  return n;
}

/**
 * 对象存储里的图片交给模型的是地址，不是内容。pi 的图片块只有 data（base64）和 mimeType 两个字段，
 * 发请求时一律拼成 data:<类型>;base64,<data>：所以先把地址放进 data，发出前再还原成真正的地址。
 * base64 的字符里没有冒号，不会和 "https://" 混淆。就地修改并返回请求体。
 */
const URL_AS_DATA = /^data:[^;,]+;base64,(https?:\/\/.+)$/;

/** 发给 OpenAI 兼容接口的请求体里，这里用到的部分 */
interface ChatPayload {
  messages?: { content?: unknown }[];
}

/** 更早的图片被省略后，模型看到的说明：告诉它怎么重新看（附件图片的素材 id 在那条消息的「附件图片」一行里） */
export const IMAGE_OMITTED = "[图片已省略；需要时调用 canvas_inspect_image，传 assetIds（附件图片）或 nodeIds（画布上的图片）重新查看]";

/**
 * 发给模型前，只保留最近 keep 条带图片的消息里的图片，更早的每张换成一段文字说明。
 * 一条消息里的几张图算一条；用户附件和工具结果里的图一起算（Pi 把工具结果的图作为附带的用户消息发出）。
 * 图片很占上下文和带宽，每一轮都原样重发是浪费。就地修改并返回请求体。
 */
export function pruneOldImages<P>(payload: P, keep: number): P {
  const messages = (payload as ChatPayload | undefined)?.messages ?? [];
  const hasImage = (m: { content?: unknown }) => Array.isArray(m.content) && (m.content as { type?: string }[]).some((p) => p?.type === "image_url");
  const withImages = messages.filter(hasImage);
  for (const m of withImages.slice(0, Math.max(0, withImages.length - keep))) {
    m.content = (m.content as { type?: string }[]).map((p) => (p?.type === "image_url" ? { type: "text", text: IMAGE_OMITTED } : p));
  }
  return payload;
}

export function restoreImageUrls<P>(payload: P): P {
  for (const m of (payload as ChatPayload | undefined)?.messages ?? []) {
    if (!Array.isArray(m.content)) continue;
    for (const part of m.content as { type?: string; image_url?: { url?: unknown } }[]) {
      const url = part?.image_url?.url;
      const hit = part?.type === "image_url" && typeof url === "string" ? URL_AS_DATA.exec(url) : null;
      if (hit && part.image_url) part.image_url.url = hit[1];
    }
  }
  return payload;
}
