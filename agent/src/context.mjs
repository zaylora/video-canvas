/** 较早的工具结果被省略后留下的占位文字 */
export const ELIDED = "[较早的工具结果已省略；需要时请重新读取]";

const IMAGE_CHARS = 1000; // 一张图按这么多字估

/** 一条消息按「1 字 1 个 Token」估的字数（偏保守，中文接近实际）；摘要压缩的触发和切点也用它 */
export function charsOf(m) {
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

/** 旧的画布目录被省略后留下的占位文字（连同标签一起，模型一眼能看出这里原来有一块目录） */
export const CATALOG_ELIDED = "<画布目录>（旧目录已省略，需要最新内容请调用 canvas_get_state）</画布目录>";

// Go 每轮把画布目录（最多 200 个节点的 JSON）拼进用户消息：<画布目录>…</画布目录>，见 backend 的 prompts.User()
const CATALOG_BLOCK = /<画布目录>[\s\S]*?<\/画布目录>/;
const CATALOG_BLOCKS = new RegExp(CATALOG_BLOCK, "g");

/**
 * 发给模型前，只保留最新一条带目录的用户消息里的画布目录，更早的用户消息里的同名块换成占位文字。
 * 目录是「当时的快照」，旧的早已过时，却会随历史一轮轮累积，白白抬高 token 成本和压缩频率。
 * 用户原话和 <本轮参数> 块不动；只处理用户消息；不修改入参，没有变化时返回同一个数组（和 trimContext 一样）。
 * 「最新」指最近一条带目录的用户消息，而不是最后一条用户消息：后者可能是没带目录的插话。
 */
export function elideOldCatalogs(messages) {
  const hasBlock = (m) => m.role === "user" && userTexts(m).some((t) => CATALOG_BLOCK.test(t));
  const latest = messages.findLastIndex(hasBlock);
  if (latest <= 0) return messages;
  let changed = false;
  const out = messages.map((m, i) => {
    if (i >= latest || m.role !== "user") return m;
    const next = mapUserTexts(m, (t) => t.replaceAll(CATALOG_BLOCKS, () => CATALOG_ELIDED));
    if (next !== m) changed = true;
    return next;
  });
  return changed ? out : messages;
}

/** 用户消息里的所有文字（content 可能是字符串，也可能是分段数组） */
function userTexts(m) {
  if (typeof m.content === "string") return [m.content];
  return Array.isArray(m.content) ? m.content.filter((p) => p.type === "text" && typeof p.text === "string").map((p) => p.text) : [];
}

/** 对用户消息里的每段文字做替换；一处都没变就返回原消息 */
function mapUserTexts(m, fn) {
  if (typeof m.content === "string") {
    const t = fn(m.content);
    return t === m.content ? m : { ...m, content: t };
  }
  if (!Array.isArray(m.content)) return m;
  let changed = false;
  const content = m.content.map((p) => {
    if (p.type !== "text" || typeof p.text !== "string") return p;
    const t = fn(p.text);
    if (t === p.text) return p;
    changed = true;
    return { ...p, text: t };
  });
  return changed ? { ...m, content } : m;
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
