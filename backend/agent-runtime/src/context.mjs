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
