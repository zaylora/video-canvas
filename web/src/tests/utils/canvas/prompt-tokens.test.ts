import { describe, expect, test } from "bun:test";

import {
  docToPrompt,
  expandPrompt,
  formatPromptPreset,
  formatPromptRef,
  parsePrompt,
  promptPresets,
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

describe("提示词里的预设", () => {
  const style = formatPromptPreset("style", "wuxia", "武侠江湖");
  const motion = formatPromptPreset("motion", "dolly_in", "镜头前推");
  const tpl = formatPromptPreset("tpl", "multi_camera_nine_grid", "多机位九宫格");
  const text = (kind: string, id: string, label: string) =>
    kind === "tpl" ? `模板:${id}` : `${label}的提示词`;

  test("写成 #[名称](种类/id)，和 @ 素材引用互不干扰", () => {
    expect(style).toBe("#[武侠江湖](style/wuxia)");
    const prompt = `参考 ${formatPromptRef("n1", "U03-1")} ${style}`;
    expect(parsePrompt(prompt)).toEqual([
      { type: "text", text: "参考 " },
      { type: "ref", id: "n1", label: "U03-1" },
      { type: "text", text: " " },
      { type: "preset", kind: "style", id: "wuxia", label: "武侠江湖" },
    ]);
    expect(promptRefIds(prompt)).toEqual(["n1"]);
    expect(promptPresets(`${motion}${tpl}`)).toEqual([
      { type: "preset", kind: "motion", id: "dolly_in", label: "镜头前推" },
      { type: "preset", kind: "tpl", id: "multi_camera_nine_grid", label: "多机位九宫格" },
    ]);
  });

  test("名字里的 ] 和换行不会弄坏格式；种类不认识的当普通文字", () => {
    expect(parsePrompt(formatPromptPreset("style", "a", "x]y\nz"))).toEqual([
      { type: "preset", kind: "style", id: "a", label: "x y z" },
    ]);
    expect(parsePrompt("#[乱写](other/x)")).toEqual([{ type: "text", text: "#[乱写](other/x)" }]);
  });

  test("字数按预设名计", () => {
    expect(promptTextLength(`夜景${style}`)).toBe("夜景武侠江湖".length);
  });

  test("和编辑器文档来回转换不丢预设", () => {
    const prompt = `${tpl} 一个人${motion}\n第二行${style}`;
    expect(docToPrompt(promptToDoc(prompt))).toBe(prompt);
  });

  test("断开引用时留着预设", () => {
    const img = formatPromptRef("img", "图");
    expect(removePromptRef(`${style} 看${img} 拍`, "img")).toBe(`${style} 看拍`);
  });

  test("提交前展开：风格和运镜原位换成提示词，前后补逗号并吃掉多余空格", () => {
    const prompt = `古风少女 ${style} 细雨`;
    expect(expandPrompt(prompt, (_, l) => l, text)).toBe("古风少女，武侠江湖的提示词，细雨");
  });

  test("已经以标点相接的地方不再补逗号，开头和结尾也不补", () => {
    expect(expandPrompt(`${motion}，夜晚`, (_, l) => l, text)).toBe("镜头前推的提示词，夜晚");
    expect(expandPrompt(`夜晚。${motion}`, (_, l) => l, text)).toBe("夜晚。镜头前推的提示词");
    expect(expandPrompt(`夜晚\n${motion}`, (_, l) => l, text)).toBe("夜晚\n镜头前推的提示词");
  });

  test("两个运镜挨着：中间补一个逗号", () => {
    const orbit = formatPromptPreset("motion", "orbit_180", "环绕拍摄");
    expect(expandPrompt(`${motion} ${orbit}`, (_, l) => l, text)).toBe(
      "镜头前推的提示词，环绕拍摄的提示词",
    );
  });

  test("模板永远排在最前面，用户写的文字接在「补充说明：」后面", () => {
    expect(expandPrompt(`一个人 ${tpl} 站在桥上`, (_, l) => l, text)).toBe(
      "模板:multi_camera_nine_grid\n\n补充说明：一个人 站在桥上",
    );
    expect(expandPrompt(`${tpl} `, (_, l) => l, text)).toBe("模板:multi_camera_nine_grid");
  });

  test("模板和风格同时在：模板在最前，风格留在原位", () => {
    expect(expandPrompt(`夜景 ${style} ${tpl}`, (_, l) => l, text)).toBe(
      "模板:multi_camera_nine_grid\n\n补充说明：夜景，武侠江湖的提示词",
    );
  });

  test("预设已下架（找不到文字）：按名称当普通文字", () => {
    expect(
      expandPrompt(
        `夜景 ${style}`,
        (_, l) => l,
        () => undefined,
      ),
    ).toBe("夜景 武侠江湖");
  });

  test("不传预设解析函数时，预设退回名称，素材引用照旧展开", () => {
    const prompt = `${formatPromptRef("n1", "U03")} ${style}`;
    expect(expandPrompt(prompt, () => "图片1")).toBe("图片1 武侠江湖");
  });
});
