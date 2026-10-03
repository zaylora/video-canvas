import { describe, expect, test } from "bun:test";

import {
  NODE_LABEL_MAX,
  copyLabels,
  newNodeLabel,
  normalizeNodeLabel,
  uploadLabel,
} from "@/utils/canvas/node-label";

describe("normalizeNodeLabel：重命名节点时输入的名字怎么落地", () => {
  test.each([
    ["  C05 — 周德发  ", "C05 — 周德发"],
    ["换\n行\t和  多个空格", "换 行 和 多个空格"],
    ["名字]里(有)括号", "名字]里(有)括号"],
  ])("去掉首尾空白、把换行和连续空白收成一个空格：%p", (input, expected) => {
    expect(normalizeNodeLabel(input, "图片")).toBe(expected);
  });

  test("清空了就退回原来的名字，不允许没有名字", () => {
    expect(normalizeNodeLabel("   ", "图片 2")).toBe("图片 2");
    expect(normalizeNodeLabel("", "图片 2")).toBe("图片 2");
  });

  test("超长截到上限，按字符算不切坏 emoji", () => {
    const long = "🎬".repeat(NODE_LABEL_MAX + 5);
    expect([...normalizeNodeLabel(long, "x")]).toHaveLength(NODE_LABEL_MAX);
  });
});

describe("新建节点的名字：同名就编号", () => {
  test("画布里没有同名的就用种类名，有了从 2 起找空着的号", () => {
    expect(newNodeLabel("图片", [])).toBe("图片");
    expect(newNodeLabel("图片", ["视频", "图片"])).toBe("图片 2");
    expect(newNodeLabel("图片", ["图片", "图片 2", "图片 4"])).toBe("图片 3");
  });

  test("改过名的节点不占号", () => {
    expect(newNodeLabel("图片", ["C05 — 周德发"])).toBe("图片");
  });
});

describe("复制节点的名字：副本 / 副本一、副本二", () => {
  test.each([
    ["复制一份", "猫", [], 1, ["猫 副本"]],
    ["一份但「副本」已被占用", "猫", ["猫 副本"], 1, ["猫 副本二"]],
    ["一次好几份", "猫", [], 3, ["猫 副本一", "猫 副本二", "猫 副本三"]],
    ["好几份时跳过已占用的", "猫", ["猫 副本二"], 2, ["猫 副本一", "猫 副本三"]],
    ["复制副本不叠「副本 副本」", "猫 副本二", ["猫", "猫 副本二"], 1, ["猫 副本"]],
    ["两位数用中文", "猫", [], 12, [...Array(11).fill(""), "猫 副本十二"]],
  ] as const)("%s", (_, label, existing, count, expected) => {
    const out = copyLabels(label, [...existing], count);
    expect(out).toHaveLength(count);
    expected.forEach((name, i) => name && expect(out[i]).toBe(name));
  });

  test("太长的名字截短底名，保住「副本」后缀", () => {
    const [out] = copyLabels("长".repeat(NODE_LABEL_MAX), [], 1);
    expect(out.endsWith(" 副本")).toBe(true);
    expect([...out]).toHaveLength(NODE_LABEL_MAX);
  });
});

describe("上传素材的节点名：用文件名", () => {
  test.each([
    ["ballet.final.png", "ballet.final"],
    ["  配乐 .mp3", "配乐"],
    [".env", ".env"],
    ["没有扩展名", "没有扩展名"],
  ])("%p → %p", (fileName, expected) => {
    expect(uploadLabel(fileName, "图片")).toBe(expected);
  });

  test("文件名为空时用种类名", () => {
    expect(uploadLabel("   ", "图片")).toBe("图片");
  });
});
