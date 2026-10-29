import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { useReducedMotion } from "motion/react";
import { toast } from "sonner";

import type { Segment } from "@/api/drama/type";
import {
  buildPlaylist,
  firstIndexFrom,
  nextIndex,
  playheadX,
  playlistTotal,
  timeAt,
  type PlayItem,
} from "@/utils/drama/playback";

/** 双缓冲的两个 video 槽位 */
export type Slot = 0 | 1;

/** 装进 video 的标记：同一个片段同一个地址就不重复装 */
const keyOf = (item: PlayItem) => `${item.id}|${item.url}`;

/** 片段条连播（两个 video 双缓冲）的对外状态与动作 */
export type Playback = {
  /** 有没有可连播的片段（至少一个采用了视频） */
  available: boolean;
  /** 连播会话进行中（播放或暂停都算）；预览区据此切换到连播画面 */
  active: boolean;
  playing: boolean;
  /** 此刻在播的片段 id；没有连播为 null */
  currentId: string | null;
  /** 连播已播放的总秒数（整数秒才更新，渲染只在秒数变化时发生） */
  elapsed: number;
  /** 可连播部分的总时长（秒） */
  total: number;
  /** 播放 / 暂停 / 继续；没在连播时从头开始 */
  toggle: () => void;
  /** 跳到某个片段的开头（没有视频的片段跳到它之后第一个有视频的）；没在连播时什么也不做 */
  jumpTo: (segmentId: string) => void;
  /** 预览区用：两个 video 的 ref、是不是当前画面、各自的事件 */
  slot: (slot: Slot) => {
    ref: (el: HTMLVideoElement | null) => void;
    visible: boolean;
    onEnded: () => void;
    onError: () => void;
    onTimeUpdate: () => void;
  };
  /** 绑到播放头元素：连播时由 hook 直接改它的 transform，不经过 React 渲染 */
  bindPlayhead: (el: HTMLElement | null) => void;
};

/**
 * 片段条连播：按 seq 依次播各片段的采用视频（没有采用的跳过）。
 * 两个 video 双缓冲：当前段在一个里播，下一段在另一个里预加载，切换只是换可见的那个，没有黑屏。
 * 播放头不走 React 渲染：连播时每帧（减弱动态时只在 timeupdate 时步进）直接改播放头元素的 transform，
 * 已播秒数只在整数秒变化时才触发渲染。
 * 片段列表变化（重排、换采用、删除）时按片段 id 找回当前位置，当前片段没有视频了就停止。
 */
export function usePlayback(segments: readonly Segment[]): Playback {
  const reduce = useReducedMotion();
  const playlist = useMemo(() => buildPlaylist(segments), [segments]);
  const total = playlistTotal(playlist);

  const [index, setIndex] = useState(-1);
  const [playing, setPlaying] = useState(false);
  const [elapsed, setElapsed] = useState(0);
  const [activeSlot, setActiveSlot] = useState<Slot>(0);

  const videos = useRef<[HTMLVideoElement | null, HTMLVideoElement | null]>([null, null]);
  const playheadEl = useRef<HTMLElement | null>(null);
  const activeRef = useRef<Slot>(0);
  const indexRef = useRef(-1);
  const playingRef = useRef(false);
  const currentIdRef = useRef<string | null>(null);
  const playlistRef = useRef<PlayItem[]>(playlist);
  const segmentsRef = useRef(segments);
  useEffect(() => {
    playlistRef.current = playlist;
    segmentsRef.current = segments;
  }, [playlist, segments]);

  const setIdx = useCallback((value: number) => {
    indexRef.current = value;
    currentIdRef.current = playlistRef.current[value]?.id ?? null;
    setIndex(value);
  }, []);
  const setPlay = useCallback((value: boolean) => {
    playingRef.current = value;
    setPlaying(value);
  }, []);

  /** 把片段装进某个 video；已经装着同一个就不动，item 为空表示清空 */
  const load = useCallback((slot: Slot, item: PlayItem | undefined) => {
    const video = videos.current[slot];
    if (!video) return;
    if (!item) {
      if (video.dataset.key) {
        delete video.dataset.key;
        video.removeAttribute("src");
        video.load();
      }
      return;
    }
    const key = keyOf(item);
    if (video.dataset.key === key) return;
    video.dataset.key = key;
    video.src = item.url;
    video.load();
  }, []);

  /** 同步播放头与已播秒数 */
  const tick = useCallback(() => {
    const item = playlistRef.current[indexRef.current];
    const video = videos.current[activeRef.current];
    if (!item || !video) return;
    const length = item.end - item.start;
    const offset = Math.min(video.currentTime, length);
    const el = playheadEl.current;
    if (el) {
      const x = playheadX(segmentsRef.current, item.id, length > 0 ? offset / length : 0);
      el.style.transform = `translateX(${x}px)`;
    }
    setElapsed(Math.floor(timeAt(playlistRef.current, indexRef.current, offset)));
  }, []);

  const play = useCallback(
    (slot: Slot) => {
      void videos.current[slot]?.play().catch(() => {
        // 浏览器拦了自动播放或视频读不出来：回到暂停，用户可以再点
        setPlay(false);
      });
    },
    [setPlay],
  );

  /** 显示第 i 段：它已经预加载在另一个 video 里就直接换过去（无黑屏），否则装进当前 video */
  const show = useCallback(
    (i: number, autoplay: boolean) => {
      const list = playlistRef.current;
      const item = list[i];
      if (!item) return;
      const before = activeRef.current;
      const preloaded = videos.current[(1 - before) as Slot]?.dataset.key === keyOf(item);
      const slot: Slot = preloaded ? ((1 - before) as Slot) : before;
      if (!preloaded) load(slot, item);
      // 回到同一段的开头（点了当前段）
      if (!preloaded && videos.current[slot]) videos.current[slot].currentTime = 0;
      activeRef.current = slot;
      setActiveSlot(slot);
      // 不可见的那个 video 预加载再下一段
      const other = (1 - slot) as Slot;
      videos.current[other]?.pause();
      load(other, list[i + 1]);
      setIdx(i);
      if (autoplay) play(slot);
      else videos.current[slot]?.pause();
      tick();
    },
    [load, play, setIdx, tick],
  );

  const stop = useCallback(
    (finished: boolean) => {
      videos.current.forEach((video) => video?.pause());
      load(0, undefined);
      load(1, undefined);
      setPlay(false);
      setIdx(-1);
      setElapsed(0);
      if (finished) toast.info("连播结束（没出视频的片段已跳过）");
    },
    [load, setIdx, setPlay],
  );

  const advance = useCallback(() => {
    const next = nextIndex(playlistRef.current, indexRef.current);
    if (next === null) stop(true);
    else show(next, true);
  }, [show, stop]);

  const toggle = useCallback(() => {
    if (playlistRef.current.length === 0) return;
    if (indexRef.current < 0) {
      setPlay(true);
      show(0, true);
    } else if (playingRef.current) {
      videos.current[activeRef.current]?.pause();
      setPlay(false);
    } else {
      setPlay(true);
      play(activeRef.current);
    }
  }, [play, setPlay, show]);

  const jumpTo = useCallback(
    (segmentId: string) => {
      if (indexRef.current < 0) return;
      const segment = segmentsRef.current.find((item) => item.id === segmentId);
      if (!segment) return;
      const i = firstIndexFrom(playlistRef.current, segment);
      if (i >= 0) show(i, playingRef.current);
    },
    [show],
  );

  // 片段列表变了：按片段 id 找回当前位置；当前片段没有视频了就停止；采用了另一条视频就重新装
  useEffect(() => {
    const id = currentIdRef.current;
    if (!id || indexRef.current < 0) return;
    const at = playlist.findIndex((item) => item.id === id);
    if (at < 0) {
      stop(false);
      return;
    }
    const video = videos.current[activeRef.current];
    if (video?.dataset.key !== keyOf(playlist[at])) {
      show(at, playingRef.current);
      return;
    }
    if (at !== indexRef.current) setIdx(at);
    load((1 - activeRef.current) as Slot, playlist[at + 1]);
  }, [playlist, load, setIdx, show, stop]);

  // 播放头：不减弱动态时每帧同步，否则只在 timeupdate 时步进
  useEffect(() => {
    if (!playing || reduce) return;
    let frame = 0;
    const loop = () => {
      tick();
      frame = requestAnimationFrame(loop);
    };
    frame = requestAnimationFrame(loop);
    return () => cancelAnimationFrame(frame);
  }, [playing, reduce, tick]);

  // 离开页面时停止，避免后台继续出声
  useEffect(() => {
    const held = videos.current;
    return () => held.forEach((video) => video?.pause());
  }, []);

  const bindPlayhead = useCallback(
    (el: HTMLElement | null) => {
      playheadEl.current = el;
      if (el) tick();
    },
    [tick],
  );

  const slot = useCallback<Playback["slot"]>(
    (which) => ({
      ref: (el) => {
        videos.current[which] = el;
      },
      visible: which === activeSlot,
      // 只处理当前画面那个 video 的事件；预加载的那个不触发连播逻辑
      onEnded: () => {
        if (which === activeRef.current && indexRef.current >= 0) advance();
      },
      onError: () => {
        if (which === activeRef.current && indexRef.current >= 0 && playingRef.current) advance();
      },
      onTimeUpdate: () => {
        if (which === activeRef.current && reduce) tick();
      },
    }),
    [activeSlot, advance, reduce, tick],
  );

  return {
    available: playlist.length > 0,
    active: index >= 0,
    playing,
    currentId: index >= 0 ? (playlist[index]?.id ?? null) : null,
    elapsed,
    total,
    toggle,
    jumpTo,
    slot,
    bindPlayhead,
  };
}

export const PlaybackContext = createContext<Playback | null>(null);

export function usePlaybackContext(): Playback {
  const value = useContext(PlaybackContext);
  if (!value) throw new Error("usePlaybackContext 必须在片段工作区内部使用");
  return value;
}
