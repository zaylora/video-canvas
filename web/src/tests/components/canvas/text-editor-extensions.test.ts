import { describe, expect, test } from "bun:test";
import { MarkdownManager } from "@tiptap/markdown";

import { TEXT_EDITOR_EXTENSIONS } from "@/components/canvas/text-editor-extensions";

const manager = new MarkdownManager({ extensions: TEXT_EDITOR_EXTENSIONS });
const roundTrip = (md: string) => manager.serialize(manager.parse(md));

describe("文本节点编辑器的 Markdown 往返", () => {
  test("标题、加粗、列表、分隔线读进来再写出去，内容不丢", () => {
    const md =
      "# 《最后一个弯》\n\n## 第一集\n\n**类型：** 都市\n**风格：** 写实\n\n---\n\n- 一\n- 二";
    expect(roundTrip(md)).toBe(md);
  });

  test("有序列表、引用、行内代码、删除线都认识", () => {
    const md = "1. 甲\n2. 乙\n\n> 引用\n\n`code` 和 ~~旧~~";
    expect(roundTrip(md)).toBe(md);
  });

  test("纯文本原样回来，旧节点没有 Markdown 符号时不会被改写", () => {
    expect(roundTrip("一句话\n\n另一段")).toBe("一句话\n\n另一段");
  });

  test("加粗解析成 bold 标记，而不是把星号当文字", () => {
    const doc = manager.parse("**重点**");
    expect(JSON.stringify(doc)).toContain('"type":"bold"');
    expect(JSON.stringify(doc)).not.toContain("**");
  });
});
