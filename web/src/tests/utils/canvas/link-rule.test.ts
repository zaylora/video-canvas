import { describe, expect, test } from "bun:test";

import { partitionLinkable } from "@/utils/canvas/link-rule";

const node = (id: string, kind: "script" | "image" | "video" | "audio") => ({ id, kind });

describe("partitionLinkable：多选引用时哪些节点能接到新节点上", () => {
  const selected = [
    node("t", "script"),
    node("i", "image"),
    node("v", "video"),
    node("a", "audio"),
  ];

  test("新建图片节点：文本、图片能接，视频、音频接不上", () => {
    const { linkable, skipped } = partitionLinkable(selected, { kind: "image" });
    expect(linkable.map((item) => item.id)).toEqual(["t", "i"]);
    expect(skipped.map((item) => item.id)).toEqual(["v", "a"]);
  });

  test("新建视频节点：四种都能接，视频连视频也行", () => {
    const { linkable, skipped } = partitionLinkable(selected, { kind: "video" });
    expect(linkable.map((item) => item.id)).toEqual(["t", "i", "v", "a"]);
    expect(skipped).toEqual([]);
  });

  test("新建音频节点：只有文本能接", () => {
    const { linkable } = partitionLinkable(selected, { kind: "audio" });
    expect(linkable.map((item) => item.id)).toEqual(["t"]);
  });

  test("保留调用方带的额外字段，方便按 id 去接线", () => {
    const { linkable } = partitionLinkable([{ id: "x", kind: "image" as const, extra: 1 }], {
      kind: "image",
    });
    expect(linkable[0].extra).toBe(1);
  });
});
