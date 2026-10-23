import { describe, expect, test } from "bun:test";

import { anchoredScrollTop, trackHistoryKeys } from "@/utils/agent/message-scroll";

describe("消息流：哪些是历史、哪些是实时新增", () => {
  test("第一次：当时已有的全是历史，不播入场动画", () => {
    expect([...trackHistoryKeys(null, ["a", "b"])]).toEqual(["a", "b"]);
  });

  test("往前补了更早的一页：新补的也是历史", () => {
    const known = trackHistoryKeys(null, ["c", "d"]);
    const next = trackHistoryKeys(known, ["a", "b", "c", "d"]);
    expect(["a", "b", "c", "d"].every((k) => next.has(k))).toBe(true);
  });

  test("末尾新增的是实时消息：不算历史，要播入场动画", () => {
    const known = trackHistoryKeys(null, ["c", "d"]);
    const next = trackHistoryKeys(known, ["c", "d", "e"]);
    expect(next.has("e")).toBe(false);
  });

  test("同一次渲染里既补了更早的一页又来了新消息：只有新消息不是历史", () => {
    const known = trackHistoryKeys(null, ["c", "d"]);
    const next = trackHistoryKeys(known, ["a", "b", "c", "d", "e"]);
    expect(["a", "b", "c", "d"].every((k) => next.has(k))).toBe(true);
    expect(next.has("e")).toBe(false);
  });

  test("头几项消失（合并、重排）：不把后面的实时消息当成历史", () => {
    const known = trackHistoryKeys(null, ["c", "d"]);
    const next = trackHistoryKeys(known, ["d", "e"]);
    expect(next.has("e")).toBe(false);
  });

  test("和已知的完全对不上（整条时间线被换掉）：都当历史，不全部飞入", () => {
    const known = trackHistoryKeys(null, ["a", "b"]);
    const next = trackHistoryKeys(known, ["x", "y"]);
    expect(next.has("x") && next.has("y")).toBe(true);
  });

  test("不改入参", () => {
    const known = trackHistoryKeys(null, ["c"]);
    trackHistoryKeys(known, ["a", "c"]);
    expect([...known]).toEqual(["c"]);
  });
});

describe("补页后的滚动位置：视线停在原来看的那条消息上", () => {
  test("内容在上面变高了多少，滚动位置就加多少", () => {
    expect(anchoredScrollTop(120, 1000, 1800)).toBe(920);
  });

  test("没有变高或变矮：不动", () => {
    expect(anchoredScrollTop(120, 1000, 1000)).toBe(120);
    expect(anchoredScrollTop(120, 1000, 900)).toBe(120);
  });
});
