import { useCallback, useEffect, useRef, useState } from "react";

import { IDLE_SLEEP_MS, canGreet, type PetMode } from "@/utils/pet/mood";

/** 打招呼的 waving 播完约 700ms，兜底在这之后清掉，免得状态一直挂着 */
const GREET_CLEAR_MS = 1200;

/** 页面是否在前台，随 visibilitychange 更新 */
export function usePageVisible(): boolean {
  const [visible, setVisible] = useState(() => document.visibilityState !== "hidden");
  useEffect(() => {
    const onChange = () => setVisible(document.visibilityState !== "hidden");
    document.addEventListener("visibilitychange", onChange);
    return () => document.removeEventListener("visibilitychange", onChange);
  }, []);
  return visible;
}

/** 系统是否开启「减少动态效果」 */
export function useReducedMotionPref(): boolean {
  const [reduced, setReduced] = useState(
    () => window.matchMedia("(prefers-reduced-motion: reduce)").matches,
  );
  useEffect(() => {
    const mql = window.matchMedia("(prefers-reduced-motion: reduce)");
    const onChange = () => setReduced(mql.matches);
    mql.addEventListener("change", onChange);
    return () => mql.removeEventListener("change", onChange);
  }, []);
  return reduced;
}

/**
 * 空闲睡着：活泼档下，距离上次指针活动和事件满 10 分钟就睡着，之后有活动就醒。
 * 活动只写一个时间戳，不触发渲染；只有睡着、醒来这两次翻转才渲染。
 * touch 给「事件」用（助手运行、生成任务变化、冒出气泡等）。
 */
export function usePetSleep(mode: PetMode, onWake: () => void) {
  const [asleep, setAsleep] = useState(false);
  const last = useRef(0);
  const asleepRef = useRef(false);
  const wakeRef = useRef(onWake);
  useEffect(() => {
    wakeRef.current = onWake;
  });

  useEffect(() => {
    last.current = Date.now();
  }, []);

  const touch = useCallback(() => {
    last.current = Date.now();
    if (asleepRef.current) {
      asleepRef.current = false;
      setAsleep(false);
      wakeRef.current();
    }
  }, []);

  useEffect(() => {
    const events = ["pointermove", "pointerdown", "keydown", "wheel"] as const;
    for (const name of events) window.addEventListener(name, touch, { passive: true });
    return () => {
      for (const name of events) window.removeEventListener(name, touch);
    };
  }, [touch]);

  useEffect(() => {
    if (mode !== "lively" || asleep) return;
    let timer = 0;
    const tick = () => {
      const rest = IDLE_SLEEP_MS - (Date.now() - last.current);
      if (rest <= 0) {
        asleepRef.current = true;
        setAsleep(true);
      } else {
        timer = window.setTimeout(tick, rest);
      }
    };
    tick();
    return () => window.clearTimeout(timer);
  }, [mode, asleep]);

  // 只有活泼档才算睡着；切到别的档位时，之后的第一次活动会把残留状态清掉
  return { asleep: asleep && mode === "lively", touch };
}

/**
 * 悬停打招呼：同一只桌宠 10 秒内最多一次，只有活泼档才打。
 * greet 为真期间 petMood 返回 waving；播完（animationend）或兜底计时后清掉。
 * wave 是不经悬停的招呼（彩蛋气泡出现时），不受冷却限制。
 */
export function usePetGreeting(mode: PetMode) {
  const [greet, setGreet] = useState(false);
  const lastGreetAt = useRef<number | null>(null);

  useEffect(() => {
    if (!greet) return;
    const timer = window.setTimeout(() => setGreet(false), GREET_CLEAR_MS);
    return () => window.clearTimeout(timer);
  }, [greet]);

  const onHover = useCallback(() => {
    const now = Date.now();
    if (mode !== "lively" || !canGreet(lastGreetAt.current, now)) return;
    lastGreetAt.current = now;
    setGreet(true);
  }, [mode]);

  const wave = useCallback(() => setGreet(true), []);
  const clear = useCallback(() => setGreet(false), []);
  return { greet, onHover, wave, clear };
}
