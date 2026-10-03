import { describe, expect, test } from "bun:test";

import {
  docToPrompt,
  expandPrompt,
  formatPromptRef,
  parsePrompt,
  promptRefIds,
  promptTextLength,
  promptToDoc,
  removePromptRef,
} from "@/utils/canvas/prompt-tokens";

describe("提示词里的素材引用", () => {
  const prompt = `参考 ${formatPromptRef("n1", "U03-1")} 的构图\n按${formatPromptRef("n3", "分镜脚本")}拍`;

  test("切段：文字和引用交替，引用带 id 和名字", () => {
    expect(parsePrompt(prompt)).toEqual([
      { type: "text", text: "参考 " },
      { type: "ref", id: "n1", label: "U03-1" },
      { type: "text", text: " 的构图\n按" },
      { type: "ref", id: "n3", label: "分镜脚本" },
      { type: "text", text: "拍" },
    ]);
    expect(promptRefIds(prompt + formatPromptRef("n1", "U03-1"))).toEqual(["n1", "n3"]);
  });

  test("名字里的 ] 和换行不会弄坏格式", () => {
    const token = formatPromptRef("a", "x]y\nz");
    expect(parsePrompt(token)).toEqual([{ type: "ref", id: "a", label: "x y z" }]);
  });

  test("字数按素材名计", () => {
    expect(promptTextLength(prompt)).toBe("参考 U03-1 的构图\n按分镜脚本拍".length);
  });

  test("提交前展开引用", () => {
    expect(expandPrompt(prompt, (id) => (id === "n1" ? "图片1" : "镜头一"))).toBe(
      "参考 图片1 的构图\n按镜头一拍",
    );
  });

  test("和编辑器文档来回转换不丢东西，空行也保留", () => {
    const withBlank = `${prompt}\n\n结尾`;
    expect(docToPrompt(promptToDoc(withBlank))).toBe(withBlank);
    expect(docToPrompt(promptToDoc(""))).toBe("");
  });

  test("硬换行和段落都算换行", () => {
    const doc = {
      type: "doc",
      content: [
        {
          type: "paragraph",
          content: [
            { type: "text", text: "a" },
            { type: "hardBreak" },
            { type: "text", text: "b" },
          ],
        },
        { type: "paragraph", content: [{ type: "mention", attrs: { id: "n1", label: "图" } }] },
      ],
    };
    expect(docToPrompt(doc)).toBe(`a\nb\n${formatPromptRef("n1", "图")}`);
  });
});

describe("removePromptRef：断开引用时把它的 chip 从提示词里拿掉", () => {
  const img = formatPromptRef("img", "图片");
  const txt = formatPromptRef("txt", "分镜");

  test("删掉这个素材的全部引用，连同插 chip 时带的那个空格；别的引用和文字不动", () => {
    expect(removePromptRef(`参考 ${img} 的构图，按${txt} 拍，再看 ${img} `, "img")).toBe(
      `参考 的构图，按${txt} 拍，再看 `,
    );
  });

  test("没引用这个素材时原样返回", () => {
    const prompt = `按${txt} 拍`;
    expect(removePromptRef(prompt, "img")).toBe(prompt);
  });

  test("换行不会被吃掉", () => {
    expect(removePromptRef(`${img}\n第二行`, "img")).toBe("\n第二行");
  });
});
