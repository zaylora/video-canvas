import { describe, expect, test } from "bun:test";

import { ghostContent, nextDismissed, pendingRefs } from "@/utils/agent/selection-ghost";
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

describe("pendingRefs：用户关闭过的占位不再冒出来", () => {
  test("已关闭的节点不出占位，别的照常出", () => {
    expect(pendingRefs(["n1", "n2"], nodes, [], ["n1"]).map((r) => r.id)).toEqual(["n2"]);
  });

  test("不传已关闭列表时和以前一样", () => {
    expect(pendingRefs(["n1"], nodes, []).map((r) => r.id)).toEqual(["n1"]);
  });
});

describe("nextDismissed：什么时候把占位关掉、什么时候重新放出来", () => {
  test("引用 chip 被删掉（退格、撤销、发送后清空）而节点还选着：关掉，占位不再回来", () => {
    expect(nextDismissed([], ["n1"], ["n1"], [])).toEqual(["n1"]);
  });

  test("只关掉真的消失的那个，还在输入框里的和没碰过的不动", () => {
    expect(nextDismissed([], ["n1", "n2", "n3"], ["n1", "n2"], ["n2"])).toEqual(["n1"]);
  });

  test("占位变深（输入框里多了 chip）不算关闭", () => {
    expect(nextDismissed([], ["n1"], [], ["n1"])).toEqual([]);
  });

  test("画布上取消选中后，关闭记录清掉：再选中同一个节点，占位重新出现一次", () => {
    const afterDelete = nextDismissed([], ["n1"], ["n1"], []);
    const afterDeselect = nextDismissed(afterDelete, [], [], []);
    expect(afterDeselect).toEqual([]);
    expect(pendingRefs(["n1"], nodes, [], afterDeselect).map((r) => r.id)).toEqual(["n1"]);
  });

  test("已关闭的 A 还选着时多选了 B：只出 B 的占位，A 保持关闭", () => {
    const dismissed = nextDismissed([], ["n1"], ["n1"], []);
    const next = nextDismissed(dismissed, ["n1", "n2"], [], []);
    expect(next).toEqual(["n1"]);
    expect(pendingRefs(["n1", "n2"], nodes, [], next).map((r) => r.id)).toEqual(["n2"]);
  });

  test("删掉的 chip 对应的节点本来就没被选中（手动 @ 的）：不记录", () => {
    expect(nextDismissed([], ["n2"], ["n1"], [])).toEqual([]);
  });

  test("同一个节点删了又被记录多次：不重复", () => {
    expect(nextDismissed(["n1"], ["n1"], ["n1"], [])).toEqual(["n1"]);
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
