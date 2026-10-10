import { describe, expect, test } from "bun:test";

import {
  TEXT_BODY_MAX,
  clipboardTextOf,
  clipFileName,
  clipText,
  decidePaste,
  readClipboard,
  type ClipboardLike,
} from "@/utils/canvas/paste";

/** 造一份假剪贴板：只给用得到的几项 */
function clip(init: { files?: File[]; text?: string; html?: string }): ClipboardLike {
  const types = [
    ...(init.files?.length ? ["Files"] : []),
    ...(init.text !== undefined ? ["text/plain"] : []),
    ...(init.html !== undefined ? ["text/html"] : []),
  ];
  return {
    files: init.files ?? [],
    types,
    getData: (type) => (type === "text/plain" ? (init.text ?? "") : (init.html ?? "")),
  };
}

const png = (name = "image.png") => new File(["a"], name, { type: "image/png" });

describe("readClipboard：剪贴板里取什么", () => {
  test("只有文件时取文件", () => {
    expect(readClipboard(clip({ files: [png()] }))).toEqual({
      files: [expect.any(File)],
      text: "",
    });
  });

  test("只有文字时取文字", () => {
    expect(readClipboard(clip({ text: "你好" }))).toEqual({ files: [], text: "你好" });
  });

  test("网页上复制图片：有 html 但没有纯文本，取文件", () => {
    const got = readClipboard(clip({ files: [png()], html: "<img src=x>" }));
    expect(got.files).toHaveLength(1);
    expect(got.text).toBe("");
  });

  test("从 Word / Excel 复制：文字加一张渲染图，取文字不取图", () => {
    const got = readClipboard(clip({ files: [png()], text: "a\tb", html: "<table></table>" }));
    expect(got.files).toHaveLength(0);
    expect(got.text).toBe("a\tb");
  });

  test("纯空白的文字当没有", () => {
    expect(readClipboard(clip({ text: "  \n " }))).toEqual({ files: [], text: "" });
  });

  test("文字统一成 \\n 换行，保留首尾以外的内容", () => {
    expect(readClipboard(clip({ text: "一\r\n二" })).text).toBe("一\n二");
  });
});

describe("decidePaste：节点、文件还是文字", () => {
  const copied = "分镜一\n分镜二";

  test("剪贴板文字就是刚复制节点时写下的那份，粘节点", () => {
    expect(decidePaste({ files: [], text: copied }, copied, true)).toBe("nodes");
  });

  test("剪贴板是空的，但画布里还留着复制的节点，粘节点（写剪贴板失败时的兜底）", () => {
    expect(decidePaste({ files: [], text: "" }, copied, true)).toBe("nodes");
  });

  test("复制节点之后又从别处复制了文字，粘文字", () => {
    expect(decidePaste({ files: [], text: "别处的文字" }, copied, true)).toBe("content");
  });

  test("有文件一律粘文件，哪怕留着复制的节点", () => {
    expect(decidePaste({ files: [png()], text: "" }, copied, true)).toBe("content");
  });

  test("没复制过节点，有内容就粘内容，什么都没有就不管", () => {
    expect(decidePaste({ files: [], text: "x" }, "", false)).toBe("content");
    expect(decidePaste({ files: [], text: "" }, "", false)).toBe("ignore");
  });
});

describe("clipboardTextOf：复制节点时写进剪贴板的文字", () => {
  test("文本节点取正文，其他节点取名字，按行拼起来", () => {
    expect(
      clipboardTextOf([
        { kind: "script", label: "文本", text: "第一幕" },
        { kind: "image", label: "角色图" },
      ]),
    ).toBe("第一幕\n角色图");
  });

  test("文本节点还没有正文就取名字，保证写进去的不是空串", () => {
    expect(clipboardTextOf([{ kind: "script", label: "文本", text: null }])).toBe("文本");
  });
});

describe("clipFileName：粘贴的文件起名", () => {
  test("截图那种统一叫 image.xxx 的，改叫「粘贴图片」并保留扩展名", () => {
    expect(clipFileName(png("image.png"))).toBe("粘贴图片.png");
    expect(clipFileName(new File(["a"], "", { type: "image/jpeg" }))).toBe("粘贴图片.jpeg");
  });

  test("有真名字的原样保留", () => {
    expect(clipFileName(png("角色.png"))).toBe("角色.png");
    expect(clipFileName(new File(["a"], "image.mp4", { type: "video/mp4" }))).toBe("image.mp4");
  });
});

describe("clipText：正文上限", () => {
  test("不超过上限原样放", () => {
    expect(clipText("abc")).toEqual({ text: "abc", clipped: false });
  });

  test("超过上限按字符截断（不拆开代理对）并标记", () => {
    const long = "😀".repeat(TEXT_BODY_MAX + 5);
    const got = clipText(long);
    expect(got.clipped).toBe(true);
    expect([...got.text]).toHaveLength(TEXT_BODY_MAX);
  });
});
