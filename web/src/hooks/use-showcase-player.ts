import { useCallback, useEffect, useMemo, useState } from "react";
import { useReducedMotion } from "motion/react";

import type { ShowcaseItemDto } from "@/api/showcase/type";
import { DURATION } from "@/lib/motion";
import { clampIndex, nextIndex, shouldAutoAdvance, shouldPlayVideo } from "@/utils/showcase/rules";

/** 视频加载最多等多久（毫秒）：超时就按「只有封面」继续计时，不让进度卡在加载上 */
const LOAD_TIMEOUT_MS = 4000;

/** 浏览器是否开启了省流量（Network Information API，Safari 和 Firefox 没有） */
const readSaveData = () =>
  (navigator as Navigator & { connection?: { saveData?: boolean } }).connection?.saveData === true;

/** 播放器对外的状态与操作，舞台、字幕、进度条都读它 */
export type ShowcasePlayer = ReturnType<typeof useShowcasePlayer>;

/**
 * 登录页背景轮播的播放状态：一次只播一条，当前条进度走满就交叉溶解到下一条。
 * 进度由进度段的 CSS 动画驱动（动画结束触发 advance），所以暂停、标签页隐藏、
 * 视频还没加载好时只要停住动画，计时就一起停住，不会和画面脱节。
 * @param items 参与轮播的作品，按顺序
 * @param clipSeconds 每条播放多少秒
 * @param posterOnSaveData 浏览器省流量时是否只显示封面
 */
export function useShowcasePlayer(
  items: ShowcaseItemDto[],
  clipSeconds: number,
  posterOnSaveData: boolean,
) {
  const reducedMotion = useReducedMotion() ?? false;
  const count = items.length;
  const [rawIndex, setRawIndex] = useState(0);
  /** 正在被溶解掉的上一条的下标；null 表示没有在溶解 */
  const [prevIndex, setPrevIndex] = useState<number | null>(null);
  const [paused, setPaused] = useState(false);
  const [hidden, setHidden] = useState(() => document.hidden);
  /** 已经「就绪」的作品 ID：视频开始播放、失败、没有视频或等待超时后记下，进度才开始走 */
  const [settledId, setSettledId] = useState<string | null>(null);

  /** 作品被删、被禁用后，当前下标收回合法范围 */
  const index = clampIndex(rawIndex, count);
  const current = items[index] as ShowcaseItemDto | undefined;
  const playVideo = shouldPlayVideo(reducedMotion, readSaveData(), posterOnSaveData);
  const autoAdvance = shouldAutoAdvance(reducedMotion, count);
  const currentId = current?.id;
  /** 不播视频（减少动态效果、省流量）时封面一出现就算就绪，不用等 */
  const settled = currentId !== undefined && (!playVideo || settledId === currentId);

  useEffect(() => {
    const onChange = () => setHidden(document.hidden);
    document.addEventListener("visibilitychange", onChange);
    return () => document.removeEventListener("visibilitychange", onChange);
  }, []);

  /** 切条：记下被溶解的那条，溶解时间过后再卸掉 */
  const goTo = useCallback(
    (target: number) => {
      if (count <= 1) return;
      const next = clampIndex(target, count);
      if (next === index) return;
      setPrevIndex(index);
      setRawIndex(next);
    },
    [count, index],
  );

  const advance = useCallback(() => goTo(nextIndex(index, count)), [count, goTo, index]);

  useEffect(() => {
    if (prevIndex === null) return;
    const timer = window.setTimeout(
      () => setPrevIndex(null),
      reducedMotion ? 0 : DURATION.dissolve * 1000,
    );
    return () => window.clearTimeout(timer);
  }, [prevIndex, reducedMotion]);

  /** 当前条等不到视频就按封面继续，最多等 LOAD_TIMEOUT_MS */
  useEffect(() => {
    if (currentId === undefined || !playVideo) return;
    const timer = window.setTimeout(() => setSettledId(currentId), LOAD_TIMEOUT_MS);
    return () => window.clearTimeout(timer);
  }, [currentId, playVideo]);

  const settle = useCallback((id: string) => setSettledId(id), []);
  const togglePause = useCallback(() => setPaused((value) => !value), []);

  /** 进度段该不该走：要自动切换、没暂停、标签页可见、当前条已就绪 */
  const running = autoAdvance && !paused && !hidden && settled;

  return useMemo(
    () => ({
      items,
      count,
      index,
      current,
      prevIndex,
      paused,
      hidden,
      playVideo,
      autoAdvance,
      reducedMotion,
      running,
      clipMs: clipSeconds * 1000,
      goTo,
      advance,
      settle,
      togglePause,
    }),
    [
      items,
      count,
      index,
      current,
      prevIndex,
      paused,
      hidden,
      playVideo,
      autoAdvance,
      reducedMotion,
      running,
      clipSeconds,
      goTo,
      advance,
      settle,
      togglePause,
    ],
  );
}
