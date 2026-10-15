import { describe, expect, test } from "bun:test";

import type { PulledModel } from "@/api/admin/ai/type";
import {
  countPrefixedKeys,
  filterPulled,
  initialSelection,
  selectionStats,
  setSelection,
} from "@/utils/admin/pull-models";

const items: PulledModel[] = [
  { id: "gpt-4o", exists: false },
  { id: "gpt-4o-mini", exists: true },
  { id: "sd2.5-c1", exists: false },
  { id: "text-embedding-3", exists: false },
];

describe("filterPulled", () => {
  test("空搜索词返回全部", () => {
    expect(filterPulled(items, "  ")).toEqual(items);
  });

  test("按模型 id 不区分大小写过滤", () => {
    expect(filterPulled(items, "GPT").map((x) => x.id)).toEqual(["gpt-4o", "gpt-4o-mini"]);
    expect(filterPulled(items, "sd2")).toEqual([items[2]]);
    expect(filterPulled(items, "nope")).toEqual([]);
  });
});

describe("initialSelection", () => {
  test("默认全选，包括已存在的", () => {
    expect([...initialSelection(items)].sort()).toEqual(items.map((x) => x.id).sort());
  });
});

describe("selectionStats", () => {
  test("统计总数、已选、将导入的新模型与已勾选的已存在模型", () => {
    expect(selectionStats(items, initialSelection(items))).toEqual({
      total: 4,
      selected: 4,
      newCount: 3,
      existingCount: 1,
    });
  });

  test("只统计清单里存在的 id，空选择为 0", () => {
    expect(selectionStats(items, new Set(["ghost", "gpt-4o"]))).toEqual({
      total: 4,
      selected: 1,
      newCount: 1,
      existingCount: 0,
    });
    expect(selectionStats(items, new Set()).selected).toBe(0);
  });
});

describe("setSelection", () => {
  test("全选只作用于当前搜索命中的项，不动其他已选状态", () => {
    const next = setSelection(items, new Set(["sd2.5-c1"]), "gpt", true);
    expect([...next].sort()).toEqual(["gpt-4o", "gpt-4o-mini", "sd2.5-c1"]);
  });

  test("取消全选只作用于命中项", () => {
    const next = setSelection(items, initialSelection(items), "gpt", false);
    expect([...next].sort()).toEqual(["sd2.5-c1", "text-embedding-3"]);
  });

  test("搜索词为空时作用于全部", () => {
    expect(setSelection(items, new Set(), "", true).size).toBe(4);
    expect(setSelection(items, initialSelection(items), "", false).size).toBe(0);
  });

  test("不改动传入的集合", () => {
    const before = new Set(["gpt-4o"]);
    setSelection(items, before, "", true);
    expect([...before]).toEqual(["gpt-4o"]);
  });
});

describe("countPrefixedKeys", () => {
  test("统计 key 因冲突加了渠道前缀的模型数", () => {
    expect(
      countPrefixedKeys("yswg", [
        { key: "gpt-4o", upstream_model: "gpt-4o" },
        { key: "yswg-gpt-4o-mini", upstream_model: "gpt-4o-mini" },
        { key: "yswg-sd2.5-2", upstream_model: "sd2.5" },
      ]),
    ).toBe(2);
  });

  test("不合法字符被替换成 - 不算前缀", () => {
    expect(countPrefixedKeys("yswg", [{ key: "a-b", upstream_model: "a/b" }])).toBe(0);
  });

  test("上游 id 本身就以渠道名开头、没有加前缀时不误判", () => {
    expect(countPrefixedKeys("yswg", [{ key: "yswg-x", upstream_model: "yswg-x" }])).toBe(0);
  });
});
