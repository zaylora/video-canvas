import { describe, expect, test } from "bun:test";

import { parseMessage, serializeChip } from "@/utils/agent/chips";

describe("行内 chip 的写法", () => {
  test("序列化后能原样解析回来", () => {
    const chip = { type: "model" as const, id: "nano-banana-pro", name: "Nano Banana Pro" };
    expect(serializeChip(chip)).toBe("@[Nano Banana Pro](model:nano-banana-pro)");
    expect(parseMessage(serializeChip(chip))).toEqual([{ kind: "chip", chip }]);
  });

  test("名字里的括号会破坏格式：序列化时去掉；空名字给个兜底", () => {
    expect(serializeChip({ type: "node", id: "n1", name: "镜头[1](特写)" })).toBe(
      "@[镜头1特写](node:n1)",
    );
    expect(serializeChip({ type: "node", id: "n1", name: "  " })).toBe("@[未命名](node:n1)");
  });

  test("文字和多个 chip 交错：按出现顺序拆开，文字原样保留", () => {
    const parts = parseMessage(
      "把 @[剧本](node:n_1) 拆开，角色用 @[A](model:a)，场景用 @[B](model:b)。",
    );
    expect(
      parts.map((p) => (p.kind === "text" ? p.text : `<${p.chip.type}:${p.chip.id}>`)),
    ).toEqual(["把 ", "<node:n_1>", " 拆开，角色用 ", "<model:a>", "，场景用 ", "<model:b>", "。"]);
  });

  test("不认识的类型、残缺的写法当普通文字", () => {
    for (const text of ["@[x](video:1)", "@[x](node:)", "@x(node:1)", "没有 chip"]) {
      expect(parseMessage(text)).toEqual([{ kind: "text", text }]);
    }
    expect(parseMessage("")).toEqual([]);
  });
});
