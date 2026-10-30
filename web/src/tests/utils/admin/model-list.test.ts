import { describe, expect, test } from "bun:test";

import { modelListQuery, reorderKeepsGroups } from "@/utils/admin/model-list";

describe("modelListQuery", () => {
  test("固定限定在当前渠道，其余参数原样带上", () => {
    expect(
      modelListQuery({ q: "gpt", page: 2, page_size: 20, kind: "text", status: "online" }, "main"),
    ).toEqual({
      q: "gpt",
      page: 2,
      page_size: 20,
      kind: "text",
      status: "online",
      channel: "main",
    });
  });

  test("没有筛选时不带 kind 与 status", () => {
    const query = modelListQuery({ page: 1, page_size: 20 }, "main");
    expect("kind" in query).toBe(false);
    expect("status" in query).toBe(false);
  });

  test("能力选“待配置”：改成 status=pending，不带 kind，覆盖状态筛选", () => {
    expect(
      modelListQuery({ page: 1, page_size: 20, kind: "pending", status: "online" }, "main"),
    ).toEqual({
      page: 1,
      page_size: 20,
      status: "pending",
      channel: "main",
    });
  });
});

describe("reorderKeepsGroups", () => {
  const rows = (...flags: [string, boolean][]) => flags.map(([key, enabled]) => ({ key, enabled }));
  const before = rows(["a", true], ["b", true], ["c", false], ["d", false]);

  test("只在已启用组内调换顺序：允许", () => {
    expect(
      reorderKeepsGroups(before, rows(["b", true], ["a", true], ["c", false], ["d", false])),
    ).toBe(true);
  });

  test("只在未启用组内调换顺序：允许", () => {
    expect(
      reorderKeepsGroups(before, rows(["a", true], ["b", true], ["d", false], ["c", false])),
    ).toBe(true);
  });

  test("未启用的行拖进已启用组：拒绝", () => {
    expect(
      reorderKeepsGroups(before, rows(["a", true], ["c", false], ["b", true], ["d", false])),
    ).toBe(false);
  });

  test("已启用的行拖到未启用组末尾：拒绝", () => {
    expect(
      reorderKeepsGroups(before, rows(["b", true], ["c", false], ["d", false], ["a", true])),
    ).toBe(false);
  });

  test("enabled 缺省视为未启用", () => {
    expect(reorderKeepsGroups([{ key: "a" }, { key: "b" }], [{ key: "b" }, { key: "a" }])).toBe(
      true,
    );
    expect(
      reorderKeepsGroups(
        [{ key: "a", enabled: true }, { key: "b" }],
        [{ key: "b" }, { key: "a", enabled: true }],
      ),
    ).toBe(false);
  });

  test("行数不一致：拒绝", () => {
    expect(reorderKeepsGroups(before, before.slice(1))).toBe(false);
  });
});
