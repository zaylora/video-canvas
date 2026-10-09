import { describe, expect, test } from "bun:test";

import type { RecordDto } from "@/api/conversation/type";
import { appendRecord, mergeOlderPage, removeRecord } from "@/utils/conversation/records";

const rec = (id: string): RecordDto => ({
  id,
  conversationId: "c1",
  kind: "image",
  modelId: "m",
  prompt: id,
  input: {},
  count: 1,
  tasks: [null],
  submitErrors: [],
  quoteCredits: 0,
  createdAt: "2026-10-09T08:00:00Z",
});

const ids = (list: RecordDto[]) => list.map((r) => r.id);

describe("记录列表（时间正序）", () => {
  test("mergeOlderPage：接口给的是新到旧，合并后放到最前并翻成正序", () => {
    const merged = mergeOlderPage([rec("c"), rec("d")], [rec("b"), rec("a")]);
    expect(ids(merged)).toEqual(["a", "b", "c", "d"]);
  });

  test("mergeOlderPage：重复的记录只留一份（以已有的为准）", () => {
    const existing = { ...rec("b"), prompt: "最新" };
    const merged = mergeOlderPage([existing, rec("c")], [rec("b"), rec("a")]);
    expect(ids(merged)).toEqual(["a", "b", "c"]);
    expect(merged[1].prompt).toBe("最新");
  });

  test("appendRecord：追加到最后；已有同 id 时原地替换，不重复", () => {
    expect(ids(appendRecord([rec("a")], rec("b")))).toEqual(["a", "b"]);
    const replaced = appendRecord([rec("a"), rec("b")], { ...rec("a"), prompt: "新" });
    expect(ids(replaced)).toEqual(["a", "b"]);
    expect(replaced[0].prompt).toBe("新");
  });

  test("removeRecord：删掉指定记录，其余保持顺序", () => {
    expect(ids(removeRecord([rec("a"), rec("b"), rec("c")], "b"))).toEqual(["a", "c"]);
    expect(ids(removeRecord([rec("a")], "x"))).toEqual(["a"]);
  });
});
