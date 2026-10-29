import { describe, expect, test } from "bun:test";

import {
  checkSourceFile,
  checkSourceText,
  dramaProgress,
  dramaStatusLabel,
  extOf,
  SOURCE_MAX_CHARS,
} from "@/utils/drama/options";

describe("新建短剧的本地预检", () => {
  test("支持 txt / md / docx，大小写不敏感", () => {
    for (const name of ["a.txt", "B.MD", "c.DOCX"]) expect(checkSourceFile({ name })).toBeNull();
  });

  test("PDF 给明确提示，其他格式提示支持范围", () => {
    expect(checkSourceFile({ name: "a.pdf" })).toContain("PDF 暂不支持");
    expect(checkSourceFile({ name: "a.doc" })).toContain("txt / md / docx");
    expect(checkSourceFile({ name: "noext" })).toContain("txt / md / docx");
    expect(extOf("a.b.c.TXT")).toBe("txt");
  });

  test("粘贴为空 / 全是空白算内容为空（对应 67002），超过 30 万字对应 67003", () => {
    expect(checkSourceText("   \n ")).toContain("为空");
    expect(checkSourceText("正文")).toBeNull();
    expect(checkSourceText("字".repeat(SOURCE_MAX_CHARS))).toBeNull();
    expect(checkSourceText("字".repeat(SOURCE_MAX_CHARS + 1))).toContain("30 万字");
  });
});

describe("列表卡状态", () => {
  test("状态文案与进行中标记", () => {
    expect(dramaStatusLabel("analyzing")).toEqual({ text: "研读中", busy: true });
    expect(dramaStatusLabel("ready").busy).toBe(false);
  });

  test("三步进度点：剧本 / 资产 / 视频（一期视频恒为未开始）", () => {
    expect(dramaProgress({ status: "analyzing", episodeCount: 0, lockedCount: 0 })).toEqual([
      0, 0, 0,
    ]);
    expect(dramaProgress({ status: "outlined", episodeCount: 6, lockedCount: 0 })).toEqual([
      0.5, 0, 0,
    ]);
    expect(dramaProgress({ status: "ready", episodeCount: 6, lockedCount: 1 })).toEqual([
      1, 0.5, 0,
    ]);
    expect(dramaProgress({ status: "ready", episodeCount: 6, lockedCount: 6 })).toEqual([1, 1, 0]);
  });
});
