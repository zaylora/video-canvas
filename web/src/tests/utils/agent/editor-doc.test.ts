import { describe, expect, test } from "bun:test";

import { chipNode, docToMessage, messageToDoc, type DocNode } from "@/utils/agent/editor-doc";

const p = (...content: DocNode[]): DocNode => ({ type: "paragraph", content });
const t = (text: string): DocNode => ({ type: "text", text });

describe("编辑器文档 ↔ 消息文本", () => {
  test("段落之间和软换行都变成换行；chip 写成 @[名字](类型:id)", () => {
    const doc: DocNode = {
      type: "doc",
      content: [
        p(t("把 "), chipNode({ type: "node", id: "n_1", name: "剧本" }), t(" 拆开")),
        p(
          t("角色用 "),
          chipNode({ type: "model", id: "a", name: "Nano Banana" }),
          { type: "hardBreak" },
          t("场景用别的"),
        ),
        { type: "paragraph" },
      ],
    };
    expect(docToMessage(doc)).toBe(
      "把 @[剧本](node:n_1) 拆开\n角色用 @[Nano Banana](model:a)\n场景用别的\n",
    );
  });

  test("空文档是空串", () => {
    expect(docToMessage({ type: "doc", content: [{ type: "paragraph" }] })).toBe("");
    expect(docToMessage({ type: "doc" })).toBe("");
  });

  test("文本还原成文档：再序列化回来与原文一致（含多个 chip、空行）", () => {
    const text =
      "把 @[剧本](node:n_1) 拆成分镜\n\n角色用 @[A](model:a)，场景用 @[B](model:b)；参考 @[拆镜](skill:script-breakdown)";
    const doc = messageToDoc(text);
    expect(doc.content).toHaveLength(3);
    expect(doc.content?.[1]).toEqual({ type: "paragraph" });
    expect(docToMessage(doc)).toBe(text);
  });

  test("chip 种类不认识：按节点引用处理，不丢", () => {
    const msg = docToMessage({
      type: "doc",
      content: [p({ type: "mention", attrs: { id: "x", label: "某物", ctype: "weird" } })],
    });
    expect(msg).toBe("@[某物](node:x)");
  });
});
