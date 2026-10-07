import { parseMessage, serializeChip, type Chip, type ChipType } from "./chips";

/** 编辑器文档里用到的节点形状（tiptap 的 JSON，只取这里认识的部分） */
export type DocNode = {
  type?: string;
  text?: string;
  attrs?: Record<string, unknown>;
  content?: DocNode[];
};

/** 行内 chip 在文档里的节点名（tiptap 的 mention 扩展） */
export const CHIP_NODE = "mention";

const CHIP_TYPES: readonly ChipType[] = ["node", "model", "skill", "asset"];

/** 文档里的 chip 节点 → chip；种类不认识时按节点引用处理 */
export function chipOf(node: DocNode): Chip {
  const raw = node.attrs?.ctype;
  return {
    type: CHIP_TYPES.includes(raw as ChipType) ? (raw as ChipType) : "node",
    id: String(node.attrs?.id ?? ""),
    name: String(node.attrs?.label ?? ""),
  };
}

/** chip → 文档里的节点 */
export const chipNode = (chip: Chip): DocNode => ({
  type: CHIP_NODE,
  attrs: { id: chip.id, label: chip.name, ctype: chip.type },
});

/**
 * 编辑器文档 → 发给后端的消息文本：段落之间和软换行都是换行，chip 写成 @[名字](类型:id)
 * @param doc tiptap 的文档 JSON
 * @returns 消息文本，首尾空白原样保留（由发送方 trim）
 */
export function docToMessage(doc: DocNode): string {
  const inline = (node: DocNode): string => {
    if (node.type === "text") return node.text ?? "";
    if (node.type === "hardBreak") return "\n";
    if (node.type === CHIP_NODE) return serializeChip(chipOf(node));
    return (node.content ?? []).map(inline).join("");
  };
  return (doc.content ?? []).map(inline).join("\n");
}

/**
 * 消息文本 → 编辑器文档：按换行拆成段落，里面的 @[名字](类型:id) 还原成 chip。
 * 引导项预填文字、重试时把原文放回输入框用
 */
export function messageToDoc(text: string): DocNode {
  const paragraphs = text.split("\n").map((line): DocNode => {
    const content = parseMessage(line).map((part): DocNode =>
      part.kind === "text" ? { type: "text", text: part.text } : chipNode(part.chip),
    );
    return content.length ? { type: "paragraph", content } : { type: "paragraph" };
  });
  return { type: "doc", content: paragraphs };
}
