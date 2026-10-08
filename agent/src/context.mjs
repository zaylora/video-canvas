/** 较早的工具结果被省略后留下的占位文字 */
export const ELIDED = "[较早的工具结果已省略；需要时请重新读取]";

const IMAGE_CHARS = 1000; // 一张图按这么多字估

function charsOf(m) {
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
 * 发给模型前裁剪上下文：估算的字数超过窗口的 ratio 时，从最旧的开始把工具结果换成占位文字，
 * 最近 keepLast 条消息不动。1 字按 1 个 Token 估（偏保守）。不修改入参，也不改 pi 里保存的历史。
 * 摘要式压缩是二期。
 */
export function trimContext(messages, { window, ratio = 0.7, keepLast = 6 }) {
  const budget = window * ratio;
  let total = messages.reduce((n, m) => n + charsOf(m), 0);
  if (total <= budget) return messages;
  const out = messages.slice();
  const limit = Math.max(0, out.length - keepLast);
  for (let i = 0; i < limit && total > budget; i++) {
    const m = out[i];
    if (m.role !== "toolResult") continue;
    const before = charsOf(m);
    if (before <= ELIDED.length) continue;
    out[i] = { ...m, content: [{ type: "text", text: ELIDED }] };
    total -= before - ELIDED.length;
  }
  return out;
}

/** 图片被去掉后留下的占位文字 */
export const IMAGE_ELIDED = "[图片已省略；需要再看请重新调用 canvas_inspect_image]";

/**
 * 把工具结果里的图片换成占位文字，只保留最近 keep 条带图片的工具结果。
 * keep=0 用于保存历史：图片是用户的素材、又很大，不写进会话历史，下次要看重新读。
 * 不修改入参。
 */
export function dropOldImages(messages, keep = 0) {
  const hasImage = (m) => m.role === "toolResult" && Array.isArray(m.content) && m.content.some((p) => p.type === "image");
  const withImages = messages.map((m, i) => (hasImage(m) ? i : -1)).filter((i) => i >= 0);
  const drop = new Set(keep > 0 ? withImages.slice(0, -keep) : withImages);
  if (drop.size === 0) return messages;
  return messages.map((m, i) => {
    if (!drop.has(i)) return m;
    const content = [];
    for (const p of m.content) {
      if (p.type !== "image") content.push(p);
      else if (content.at(-1)?.text !== IMAGE_ELIDED) content.push({ type: "text", text: IMAGE_ELIDED });
    }
    return { ...m, content };
  });
}

/**
 * 对象存储里的图片交给模型的是地址，不是内容。pi 的图片块只有 data（base64）和 mimeType 两个字段，
 * 发请求时一律拼成 data:<类型>;base64,<data>：所以先把地址放进 data，发出前再还原成真正的地址。
 * base64 的字符里没有冒号，不会和 "https://" 混淆。就地修改并返回请求体。
 */
const URL_AS_DATA = /^data:[^;,]+;base64,(https?:\/\/.+)$/;

export function restoreImageUrls(payload) {
  for (const m of payload?.messages ?? []) {
    if (!Array.isArray(m.content)) continue;
    for (const part of m.content) {
      const hit = part?.type === "image_url" && typeof part.image_url?.url === "string" ? URL_AS_DATA.exec(part.image_url.url) : null;
      if (hit) part.image_url.url = hit[1];
    }
  }
  return payload;
}
