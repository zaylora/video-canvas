import { describe, expect, test } from "bun:test";

import { createScrollTopRunner, type ScrollEnv } from "@/utils/assets/scroll-top";

/** 假的页面滚动：手动推进帧和时间 */
function fakeEnv(startY: number) {
  let y = startY;
  let time = 0;
  let frames: (() => void)[] = [];
  const scrolls: ScrollToOptions[] = [];
  const env: ScrollEnv = {
    scrollY: () => y,
    scrollTo: (options) => scrolls.push(options),
    nextFrame: (callback) => frames.push(callback),
    now: () => time,
  };
  /** 推进一帧：页面滚到 nextY，时间过去 ms */
  const tick = (nextY: number, ms = 16) => {
    y = nextY;
    time += ms;
    const pending = frames;
    frames = [];
    for (const callback of pending) callback();
  };
  return { env, scrolls, tick };
}

describe("先平滑滚回顶部，到顶再切换内容", () => {
  test("已经在顶部：直接切，不滚", () => {
    const { env, scrolls } = fakeEnv(0);
    const calls: string[] = [];
    createScrollTopRunner(env)(() => calls.push("apply"));
    expect(calls).toEqual(["apply"]);
    expect(scrolls).toEqual([]);
  });

  test("不在顶部：先平滑滚，滚到顶之前不切（否则列表变短，浏览器会直接把页面截到顶部，看起来是跳上去的）", () => {
    const { env, scrolls, tick } = fakeEnv(1200);
    const calls: string[] = [];
    createScrollTopRunner(env)(() => calls.push("apply"));
    expect(scrolls).toEqual([{ top: 0, behavior: "smooth" }]);
    expect(calls).toEqual([]);
    tick(600);
    expect(calls).toEqual([]);
    tick(0);
    expect(calls).toEqual(["apply"]);
    tick(0);
    expect(calls).toEqual(["apply"]);
  });

  test("滚动被打断、迟迟到不了顶：超时后照样切换", () => {
    const { env, tick } = fakeEnv(1200);
    const calls: string[] = [];
    createScrollTopRunner(env)(() => calls.push("apply"));
    for (let i = 0; i < 20; i += 1) tick(800, 100);
    expect(calls).toEqual(["apply"]);
  });

  test("连着点了两次：只切到最后一次选的", () => {
    const { env, tick } = fakeEnv(1200);
    const calls: string[] = [];
    const run = createScrollTopRunner(env);
    run(() => calls.push("video"));
    run(() => calls.push("audio"));
    tick(0);
    expect(calls).toEqual(["audio"]);
  });
});
