import type { JSONContent } from "@tiptap/core";

/**
 * 提示词里 @ 进来的素材（设计稿 6.7）：节点里仍存纯文本，引用写成 `@[素材名](节点 id)`，
 * 编辑器负责把它渲染成 chip。存名字是为了源节点被删后 chip 还能说清原来引用的是谁。
 */
const REF_RE = /@\[([^\]\n]*)\]\(([^)\s]+)\)/g;

/** 提示词按引用切开后的一段 */
export type PromptSegment =
  /** 普通文字 */
  | { type: "text"; text: string }
  /** 一个素材引用：节点 id + 写入时的素材名 */
  | { type: "ref"; id: string; label: string };

/** 把一个素材引用写成 token；名字里的 `]` 和换行会弄坏格式，换成空格 */
export const formatPromptRef = (id: string, label: string) =>
  `@[${label.replace(/[\]\n]/g, " ")}](${id})`;

/** 把提示词切成文字段和引用段，相邻的文字不会被拆开 */
export function parsePrompt(prompt: string): PromptSegment[] {
  const segments: PromptSegment[] = [];
  let last = 0;
  for (const match of prompt.matchAll(REF_RE)) {
    if (match.index > last) segments.push({ type: "text", text: prompt.slice(last, match.index) });
    segments.push({ type: "ref", label: match[1], id: match[2] });
    last = match.index + match[0].length;
  }
  if (last < prompt.length) segments.push({ type: "text", text: prompt.slice(last) });
  return segments;
}

/** 提示词里引用了哪些节点，按出现顺序去重 */
export const promptRefIds = (prompt: string) => [
  ...new Set(parsePrompt(prompt).flatMap((seg) => (seg.type === "ref" ? [seg.id] : []))),
];

/** 用户眼里的字数：引用按素材名计，不把 token 的括号和 id 算进去 */
export const promptTextLength = (prompt: string) =>
  parsePrompt(prompt).reduce(
    (sum, seg) => sum + (seg.type === "text" ? seg.text.length : seg.label.length),
    0,
  );

/** 提交前把每个引用换成 resolve 给的文字（素材编号、上游正文等），其余原样 */
export const expandPrompt = (prompt: string, resolve: (id: string, label: string) => string) =>
  parsePrompt(prompt)
    .map((seg) => (seg.type === "text" ? seg.text : resolve(seg.id, seg.label)))
    .join("");

/** 编辑器里引用节点的类型名，和 @tiptap/extension-mention 的默认名一致 */
export const MENTION_NODE = "mention";

/** 一行文字转成段落内容：文字节点 + 引用节点 */
function lineToInline(line: string): JSONContent[] {
  return parsePrompt(line).map((seg) =>
    seg.type === "text"
      ? { type: "text", text: seg.text }
      : { type: MENTION_NODE, attrs: { id: seg.id, label: seg.label } },
  );
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
    else out += dropSpace && seg.text.startsWith(" ") ? seg.text.slice(1) : seg.text;
    dropSpace = false;
  }
  return out;
}
