import type { JSONContent } from "@tiptap/core";

import type { PresetKind } from "@/constants/presets";

/**
 * 提示词里 @ 进来的素材（设计稿 6.7）：节点里仍存纯文本，引用写成 `@[素材名](节点 id)`，
 * 编辑器负责把它渲染成 chip。存名字是为了源节点被删后 chip 还能说清原来引用的是谁。
 * 节点预设（设计稿 6.13）同理，写成 `#[名称](种类/id)`，发送时才展开成完整提示词。
 * 两种 token 放在一条正则里，按出现顺序一次切开。
 */
const TOKEN_RE = /@\[([^\]\n]*)\]\(([^)\s]+)\)|#\[([^\]\n]*)\]\((style|motion|tpl)\/([\w-]+)\)/g;

/** 提示词按引用切开后的一段 */
export type PromptSegment =
  /** 普通文字 */
  | { type: "text"; text: string }
  /** 一个素材引用：节点 id + 写入时的素材名 */
  | { type: "ref"; id: string; label: string }
  /** 一个预设：种类 + 预设 id + 写入时的名称（预设下架后 chip 还能说清是哪个） */
  | { type: "preset"; kind: PresetKind; id: string; label: string };

/** 把一个素材引用写成 token；名字里的 `]` 和换行会弄坏格式，换成空格 */
export const formatPromptRef = (id: string, label: string) =>
  `@[${label.replace(/[\]\n]/g, " ")}](${id})`;

/** 把一个预设写成 token；名字里的 `]` 和换行会弄坏格式，换成空格 */
export const formatPromptPreset = (kind: PresetKind, id: string, label: string) =>
  `#[${label.replace(/[\]\n]/g, " ")}](${kind}/${id})`;

/** 把提示词切成文字段、引用段和预设段，相邻的文字不会被拆开 */
export function parsePrompt(prompt: string): PromptSegment[] {
  const segments: PromptSegment[] = [];
  let last = 0;
  for (const match of prompt.matchAll(TOKEN_RE)) {
    if (match.index > last) segments.push({ type: "text", text: prompt.slice(last, match.index) });
    segments.push(
      match[2] !== undefined
        ? { type: "ref", label: match[1], id: match[2] }
        : { type: "preset", kind: match[4] as PresetKind, id: match[5], label: match[3] },
    );
    last = match.index + match[0].length;
  }
  if (last < prompt.length) segments.push({ type: "text", text: prompt.slice(last) });
  return segments;
}

/** 提示词里引用了哪些节点，按出现顺序去重 */
export const promptRefIds = (prompt: string) => [
  ...new Set(parsePrompt(prompt).flatMap((seg) => (seg.type === "ref" ? [seg.id] : []))),
];

/** 提示词里的预设，按出现顺序，同一个预设出现几次就给几次 */
export const promptPresets = (prompt: string) =>
  parsePrompt(prompt).flatMap((seg) => (seg.type === "preset" ? [seg] : []));

/** 用户眼里的字数：引用和预设按名字计，不把 token 的括号和 id 算进去 */
export const promptTextLength = (prompt: string) =>
  parsePrompt(prompt).reduce(
    (sum, seg) => sum + (seg.type === "text" ? seg.text.length : seg.label.length),
    0,
  );

/** 句末或句中的标点、空白：预设展开后，贴着这些的一侧不用再补逗号 */
const BREAK_RE = /[\s，。！？；：、,.!?;:）)」』】”"'…—]/;

/**
 * 提交前把引用和预设换成模型看得懂的文字，其余原样：
 * - 素材引用换成 resolve 给的文字（素材编号、上游正文等）。
 * - 风格、运镜原位换成预设提示词，前后相邻字符不是标点时补「，」，并吃掉紧挨着的空格。
 * - 模板是整条任务的说明，永远排在最前，用户写的文字接在「补充说明：」后面。
 * - resolvePreset 返回 undefined（预设已下架）、或根本没传：按名称当普通文字。
 */
export function expandPrompt(
  prompt: string,
  resolve: (id: string, label: string) => string,
  resolvePreset?: (kind: PresetKind, id: string, label: string) => string | undefined,
) {
  const lead: string[] = [];
  let body = "";
  /** 刚放完一个预设，下一段文字的开头要看要不要补逗号 */
  let afterPreset = false;
  /** 刚拿掉一个模板，它旁边多出来的空格要吃掉 */
  let eatSpace = false;
  for (const seg of parsePrompt(prompt)) {
    if (seg.type === "preset") {
      const text = resolvePreset?.(seg.kind, seg.id, seg.label)?.trim();
      if (text === undefined || text === "") {
        body += seg.label;
        afterPreset = false;
        continue;
      }
      if (seg.kind === "tpl") {
        lead.push(text);
        eatSpace = /(^|\s)$/.test(body);
        continue;
      }
      body = body.replace(/[ \t]+$/, "");
      if (body && !BREAK_RE.test(body.slice(-1))) body += "，";
      body += text;
      afterPreset = true;
      continue;
    }
    let piece = seg.type === "text" ? seg.text : resolve(seg.id, seg.label);
    if (afterPreset || eatSpace) piece = piece.replace(/^[ \t]+/, "");
    if (afterPreset && piece) {
      if (!BREAK_RE.test(piece[0])) body += "，";
      afterPreset = false;
    }
    if (piece) eatSpace = false;
    body += piece;
  }
  if (lead.length === 0) return body;
  const extra = body.trim();
  return extra ? `${lead.join("\n\n")}\n\n补充说明：${extra}` : lead.join("\n\n");
}

/** 编辑器里引用节点的类型名，和 @tiptap/extension-mention 的默认名一致 */
export const MENTION_NODE = "mention";
/** 编辑器里预设 chip 的类型名 */
export const PRESET_NODE = "preset";

/** 一行文字转成段落内容：文字节点 + 引用节点 + 预设节点 */
function lineToInline(line: string): JSONContent[] {
  return parsePrompt(line).map((seg) => {
    if (seg.type === "text") return { type: "text", text: seg.text };
    if (seg.type === "preset")
      return { type: PRESET_NODE, attrs: { kind: seg.kind, id: seg.id, label: seg.label } };
    return { type: MENTION_NODE, attrs: { id: seg.id, label: seg.label } };
  });
}

/** 提示词转成编辑器文档：一行一个段落 */
export function promptToDoc(prompt: string): JSONContent {
  return {
    type: "doc",
    content: prompt.split("\n").map((line) => {
      const content = lineToInline(line);
      return content.length > 0 ? { type: "paragraph", content } : { type: "paragraph" };
    }),
  };
}

/** 编辑器文档转回提示词：段落之间、硬换行都是 \n，引用写回 token */
export function docToPrompt(doc: JSONContent): string {
  const inline = (node: JSONContent): string => {
    if (node.type === "text") return node.text ?? "";
    if (node.type === "hardBreak") return "\n";
    if (node.type === MENTION_NODE)
      return formatPromptRef(String(node.attrs?.id ?? ""), String(node.attrs?.label ?? ""));
    if (node.type === PRESET_NODE)
      return formatPromptPreset(
        node.attrs?.kind as PresetKind,
        String(node.attrs?.id ?? ""),
        String(node.attrs?.label ?? ""),
      );
    return (node.content ?? []).map(inline).join("");
  };
  return (doc.content ?? []).map(inline).join("\n");
}

/**
 * 把某个素材的引用从提示词里全部拿掉（引用条上点 × 断开时用），
 * 连同插 chip 时自动带上的那一个空格；没引用它就原样返回。
 */
export function removePromptRef(prompt: string, id: string): string {
  const segments = parsePrompt(prompt);
  if (!segments.some((seg) => seg.type === "ref" && seg.id === id)) return prompt;
  let out = "";
  let dropSpace = false;
  for (const seg of segments) {
    if (seg.type === "ref" && seg.id === id) {
      dropSpace = true;
      continue;
    }
    if (seg.type === "ref") out += formatPromptRef(seg.id, seg.label);
    else if (seg.type === "preset") out += formatPromptPreset(seg.kind, seg.id, seg.label);
    else out += dropSpace && seg.text.startsWith(" ") ? seg.text.slice(1) : seg.text;
    dropSpace = false;
  }
  return out;
}
