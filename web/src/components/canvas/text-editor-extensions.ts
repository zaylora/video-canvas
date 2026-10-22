import { Markdown } from "@tiptap/markdown";
import StarterKit from "@tiptap/starter-kit";

/**
 * 文本节点弹窗编辑器的扩展：StarterKit 带标题、加粗、斜体、删除线、列表、引用、行内代码、分隔线，
 * Markdown 扩展负责把节点里存的 Markdown 读进编辑器、再写回去。
 * 节点数据始终是 Markdown 字符串，下游引用和后端都不用改；
 * 链接和下划线 Markdown 里没有对应写法，关掉，免得写出去的文本带 HTML。
 */
export const TEXT_EDITOR_EXTENSIONS = [
  StarterKit.configure({ link: false, underline: false }),
  Markdown,
];
