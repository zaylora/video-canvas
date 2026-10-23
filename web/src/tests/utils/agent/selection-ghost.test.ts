import { describe, expect, test } from "bun:test";

import { ghostContent, pendingRefs } from "@/utils/agent/selection-ghost";
import type { RefNode } from "@/utils/agent/selection-refs";

const nodes: RefNode[] = [
  { id: "n1", label: "镜头 1 分镜脚本", kind: "script" },
  { id: "n2", label: "镜头 1 画面", kind: "image" },
  { id: "g1", label: "", kind: "group" },
];

describe("pendingRefs：画布选中、但还没写进输入框的节点", () => {
  test("没有选中时没有占位", () => {
    expect(pendingRefs([], nodes, [])).toEqual([]);
  });

  test("按选中顺序出占位，标题取画布上的当前标题", () => {
    const refs = pendingRefs(["n2", "n1"], nodes, []);
    expect(refs.map((r) => [r.id, r.label])).toEqual([
      ["n2", "镜头 1 画面"],
      ["n1", "镜头 1 分镜脚本"],
    ]);
  });

  test("已经是输入框里的 chip 的节点不再出占位", () => {
    const refs = pendingRefs(["n1", "n2"], nodes, ["n1"]);
    expect(refs.map((r) => r.id)).toEqual(["n2"]);
  });

  test("选中列表里重复的 id 只出一个占位", () => {
    expect(pendingRefs(["n1", "n1"], nodes, []).map((r) => r.id)).toEqual(["n1"]);
  });

  test("画布上已经没有的节点不出占位", () => {
    expect(pendingRefs(["gone", "n1"], nodes, []).map((r) => r.id)).toEqual(["n1"]);
  });

  test("选中的是组：保留组种类，没名字用「节点组」", () => {
    const [ref] = pendingRefs(["g1"], nodes, []);
    expect(ref).toMatchObject({ id: "g1", label: "节点组", kind: "group" });
  });
});

describe("ghostContent：占位变深时写进文档的内容", () => {
  test("每个占位一个节点 chip，最后带一个空格；组按节点引用写，并带上种类标记（变深后图标不变）", () => {
    const content = ghostContent(pendingRefs(["n1", "g1"], nodes, []));
    expect(content).toEqual([
      { type: "mention", attrs: { id: "n1", label: "镜头 1 分镜脚本", ctype: "node" } },
      { type: "mention", attrs: { id: "g1", label: "节点组", ctype: "node", nodeKind: "group" } },
      { type: "text", text: " " },
    ]);
  });

  test("角色卡也带种类标记，图标是角色卡的", () => {
    const withCard: RefNode[] = [...nodes, { id: "k1", label: "林夏", kind: "character" }];
    const [chip] = ghostContent(pendingRefs(["k1"], withCard, []));
    expect(chip).toEqual({
      type: "mention",
      attrs: { id: "k1", label: "林夏", ctype: "node", nodeKind: "character" },
    });
  });

  test("没有占位时什么都不写", () => {
    expect(ghostContent([])).toEqual([]);
  });
});
