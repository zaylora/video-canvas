import { describe, expect, test } from "bun:test";

import { typewriterFrame, type TypewriterTiming } from "@/utils/login/typewriter";

/**
 * 前缀 8 个字，词是「片」「电影」。
 * 开场：250ms 后每 100ms 打一个字，打到「片」（共 9 个字，1150ms 结束）。
 * 之后：停留 2000 → 删词（每字 50）→ 空档 300 → 打下一个词（每字 100），共 2550ms；
 * 「电影」打完（开场结束后 2550ms）就停下，不再循环。
 */
const timing: TypewriterTiming = {
  delay: 250,
  typeInterval: 100,
  deleteInterval: 50,
  hold: 2000,
  pause: 300,
};
const PREFIX = 8;
const WORDS = ["片", "电影"];
const at = (elapsed: number) => typewriterFrame(elapsed, PREFIX, WORDS, timing);
/** 换词从开场结束（1150ms）算起 */
const loop = (local: number) => at(1150 + local);

describe("typewriterFrame：开场整句打出来", () => {
  test("起始延迟之前什么都没有", () => {
    expect(at(0)).toEqual({ prefix: 0, word: 0, count: 0, ended: false, busy: false, done: false });
    expect(at(249)).toEqual({
      prefix: 0,
      word: 0,
      count: 0,
      ended: false,
      busy: false,
      done: false,
    });
  });

  test("先逐字打前缀，句号还没出现", () => {
    expect(at(250)).toEqual({
      prefix: 1,
      word: 0,
      count: 0,
      ended: false,
      busy: true,
      done: false,
    });
    expect(at(250 + 700)).toEqual({
      prefix: 8,
      word: 0,
      count: 0,
      ended: false,
      busy: true,
      done: false,
    });
  });

  test("前缀打完接着打第一个词，词一出现句号跟着出现", () => {
    expect(at(250 + 800)).toEqual({
      prefix: 8,
      word: 0,
      count: 1,
      ended: true,
      busy: true,
      done: false,
    });
  });
});

describe("typewriterFrame：换词，前缀和句号不动", () => {
  test("停留：词完整显示，光标闪烁", () => {
    expect(loop(0)).toEqual({
      prefix: 8,
      word: 0,
      count: 1,
      ended: true,
      busy: false,
      done: false,
    });
    expect(loop(1999)).toEqual({
      prefix: 8,
      word: 0,
      count: 1,
      ended: true,
      busy: false,
      done: false,
    });
  });

  test("删词：「片」被删掉，其余保留", () => {
    expect(loop(2000)).toEqual({
      prefix: 8,
      word: 0,
      count: 0,
      ended: true,
      busy: true,
      done: false,
    });
    expect(loop(2049)).toEqual({
      prefix: 8,
      word: 0,
      count: 0,
      ended: true,
      busy: true,
      done: false,
    });
  });

  test("空档：词是空的，光标闪烁", () => {
    expect(loop(2050)).toEqual({
      prefix: 8,
      word: 0,
      count: 0,
      ended: true,
      busy: false,
      done: false,
    });
    expect(loop(2349)).toEqual({
      prefix: 8,
      word: 0,
      count: 0,
      ended: true,
      busy: false,
      done: false,
    });
  });

  test("打下一个词「电影」：逐字出现", () => {
    expect(loop(2350)).toEqual({
      prefix: 8,
      word: 1,
      count: 1,
      ended: true,
      busy: true,
      done: false,
    });
    expect(loop(2450)).toEqual({
      prefix: 8,
      word: 1,
      count: 2,
      ended: true,
      busy: true,
      done: false,
    });
    expect(loop(2549)).toEqual({
      prefix: 8,
      word: 1,
      count: 2,
      ended: true,
      busy: true,
      done: false,
    });
  });
});

describe("typewriterFrame：打完「电影」就停下", () => {
  test("最后一个字一出现就是 done，之后画面不再变化", () => {
    expect(loop(2549)).toEqual({
      prefix: 8,
      word: 1,
      count: 2,
      ended: true,
      busy: true,
      done: false,
    });
    const stopped = { prefix: 8, word: 1, count: 2, ended: true, busy: false, done: true };
    expect(loop(2550)).toEqual(stopped);
    expect(loop(60_000)).toEqual(stopped);
  });
});

describe("typewriterFrame：边界", () => {
  test("没有词时只打前缀，打完停住", () => {
    expect(typewriterFrame(60_000, 8, [], timing)).toEqual({
      prefix: 8,
      word: 0,
      count: 0,
      ended: false,
      busy: false,
      done: false,
    });
  });

  test("只有一个词时，开场打完就停下", () => {
    expect(typewriterFrame(60_000, 8, ["片"], timing)).toEqual({
      prefix: 8,
      word: 0,
      count: 1,
      ended: true,
      busy: false,
      done: true,
    });
  });
});
