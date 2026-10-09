import { useEffect, useState } from "react";

import {
  typewriterFrame,
  type TypewriterFrame,
  type TypewriterTiming,
} from "@/utils/login/typewriter";

const sameFrame = (a: TypewriterFrame, b: TypewriterFrame) =>
  a.prefix === b.prefix &&
  a.word === b.word &&
  a.count === b.count &&
  a.ended === b.ended &&
  a.busy === b.busy &&
  a.done === b.done;

/**
 * 标题打字机：返回当前该显示的画面，开场打整句，之后依次换词，最后一个词打完就停。
 * 用 requestAnimationFrame 按真实经过的时间算，切后台掉帧也不会错位；画面没变就不触发重渲染，done 之后不再逐帧计算。
 * @param prefixLength 前缀字数
 * @param words 轮换的词，要传引用稳定的常量
 * @param timing 各阶段时长，要传引用稳定的常量
 * @param skip 为 true 时静止显示最后一个词的完整句子，用于「减少动态效果」
 */
export function useTypewriter(
  prefixLength: number,
  words: string[],
  timing: TypewriterTiming,
  skip: boolean,
): TypewriterFrame {
  const [frame, setFrame] = useState<TypewriterFrame>(() =>
    typewriterFrame(0, prefixLength, [], timing),
  );

  useEffect(() => {
    if (skip) return;
    const start = performance.now();
    let handle = 0;
    const tick = (now: number) => {
      const next = typewriterFrame(now - start, prefixLength, words, timing);
      setFrame((prev) => (sameFrame(prev, next) ? prev : next));
      if (!next.done) handle = requestAnimationFrame(tick);
    };
    handle = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(handle);
  }, [prefixLength, words, timing, skip]);

  if (skip) {
    const last = Math.max(0, words.length - 1);
    return {
      prefix: prefixLength,
      word: last,
      count: [...(words[last] ?? "")].length,
      ended: true,
      busy: false,
      done: true,
    };
  }
  return frame;
}
