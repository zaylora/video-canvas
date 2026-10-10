import { describe, expect, test } from "bun:test";

import { referencedText, textEditPatch } from "@/utils/canvas/text-body";

describe("textEditPatch：双击编辑完正文，节点数据怎么变", () => {
  test("内容没变不产生改动", () => {
    expect(textEditPatch({ text: "第一幕" }, "第一幕")).toBeNull();
    expect(textEditPatch({ text: null }, "")).toBeNull();
    expect(textEditPatch({}, "")).toBeNull();
  });

  test("写了内容：正文更新，节点算有结果，清掉失败信息", () => {
    expect(textEditPatch({ text: null }, "粘贴来的话")).toEqual({
      text: "粘贴来的话",
      status: "done",
      error: null,
      errorTaskRef: null,
    });
  });

  test("把正文清空：回到还没内容的状态，不留空白的「已完成」", () => {
    expect(textEditPatch({ text: "旧的" }, "  \n")).toEqual({
      text: "  \n",
      status: "idle",
      error: null,
      errorTaskRef: null,
    });
  });
});

describe("referencedText：下游引用文本节点时读到什么", () => {
  test("读正文区的内容", () => {
    expect(referencedText({ status: "done", text: "正文", prompt: "提示词" })).toBe("正文");
  });

  test("没有正文就是没有，不退回去读提示词", () => {
    expect(referencedText({ status: "idle", prompt: "只写了提示词" })).toBeUndefined();
    expect(referencedText({ status: "idle", text: null, prompt: "只写了提示词" })).toBeUndefined();
  });

  test("正在生成时正文还是旧的，不引用", () => {
    expect(referencedText({ status: "running", text: "旧正文" })).toBeUndefined();
  });
});
