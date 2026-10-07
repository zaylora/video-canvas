/** 消息里行内 chip 的种类：节点、生成模型、技能、上传的附件 */
export type ChipType = "node" | "model" | "skill" | "asset";

/** 一个行内 chip */
export type Chip = {
  /** 种类 */
  type: ChipType;
  /** 引用的对象：节点 id、模型 key、技能名、素材 id */
  id: string;
  /** 显示名 */
  name: string;
};

/** 一段消息拆开后的一块：纯文字，或者一个 chip */
export type MessagePart = { kind: "text"; text: string } | { kind: "chip"; chip: Chip };

const CHIP = /@\[([^\]]*)\]\((node|model|skill|asset):([^)\s]+)\)/g;

/** 名字里的括号会破坏格式，序列化时去掉 */
const cleanName = (name: string) => name.replace(/[[\]()]/g, "").trim() || "未命名";

/**
 * 把 chip 序列化成消息里的写法 @[名字](类型:id)。后端按这个写法校验引用，Agent 也靠它认出用户指定了什么
 * @param chip 要序列化的 chip
 * @returns 消息里的文本
 */
export const serializeChip = (chip: Chip) => `@[${cleanName(chip.name)}](${chip.type}:${chip.id})`;

/**
 * 把消息文本拆成文字和 chip，用来在消息流里把 chip 画成小标签
 * @param text 消息原文
 * @returns 按出现顺序排好的块
 */
export function parseMessage(text: string): MessagePart[] {
  const parts: MessagePart[] = [];
  let last = 0;
  for (const m of text.matchAll(CHIP)) {
    const at = m.index ?? 0;
    if (at > last) parts.push({ kind: "text", text: text.slice(last, at) });
    parts.push({ kind: "chip", chip: { name: m[1], type: m[2] as ChipType, id: m[3] } });
    last = at + m[0].length;
  }
  if (last < text.length) parts.push({ kind: "text", text: text.slice(last) });
  return parts;
}
