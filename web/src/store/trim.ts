import { create } from "zustand";

import type { TrimPhase } from "@/utils/canvas/trim";

type TrimStore = {
  /** 正在修剪的节点 id；没有就是不在修剪模式。只活在内存里，不存档 */
  nodeId: string | null;
  /** 加载中（读时长、取缩略图）、编辑中、编码中 */
  phase: TrimPhase;
  /** 编码进度 0 到 100 的整数 */
  progress: number;
  /** 上一次失败的原因，写给用户看；编辑态下面板里显示 */
  error: string | null;
  /** 源视频总时长（秒）和有没有音轨；读出来之前为 0 / false */
  duration: number;
  hasAudio: boolean;
  /** 保留片段的起止（秒）。放在仓库里，编码时面板被卸载也不丢 */
  start: number;
  end: number;
  /** 去掉音轨导出；没有音轨时永远是 false */
  discardAudio: boolean;
  /** 预览播放到哪（秒），播放头用 */
  current: number;
  /** 给预览的定位请求：拖手柄、点胶片条时写入，nonce 每次不同，同一时刻连点两次也会重新定位 */
  seek: { time: number; nonce: number } | null;
  /**
   * 进入某个节点的修剪模式（同一时刻只有一个会话）。
   * 别的节点正在编码时不能顶替，返回 false，调用方提示「正在修剪另一个视频」。
   */
  enter: (id: string) => boolean;
  /** 退出修剪模式；传了 id 就只在正是这个节点时才退。编码中不退，要先中止 */
  leave: (id?: string) => void;
  /** 读到时长和音轨：选区默认全长并进入编辑态；已经在编辑或编码（面板重新挂上）时不动选区 */
  ready: (duration: number, hasAudio: boolean) => void;
  /** 改选区；编码中锁定 */
  setRange: (start: number, end: number) => void;
  setDiscardAudio: (discard: boolean) => void;
  setCurrent: (time: number) => void;
  seekTo: (time: number) => void;
  setPhase: (phase: TrimPhase) => void;
  setProgress: (percent: number) => void;
  /** 编码或读取失败：回到编辑态、留下原因，选区由面板自己保管所以不丢 */
  fail: (message: string) => void;
  /** 无条件清空：中止编码、离开画布时用 */
  reset: () => void;
};

const IDLE = {
  nodeId: null,
  phase: "loading",
  progress: 0,
  error: null,
  duration: 0,
  hasAudio: false,
  start: 0,
  end: 0,
  discardAudio: false,
  current: 0,
  seek: null,
} as const;

/**
 * 修剪模式（设计稿「视频修剪」）：点功能区「视频修剪」进入；
 * 编码完成、点 ×（编辑态）、取消选中（编辑态）、多选时退出；编码期间取消选中和 Esc 都不退出。
 */
export const useTrimStore = create<TrimStore>((set, get) => ({
  ...IDLE,
  enter: (id) => {
    const { nodeId, phase } = get();
    if (nodeId !== null && nodeId !== id && phase === "encoding") return false;
    set({ ...IDLE, nodeId: id });
    return true;
  },
  leave: (id) =>
    set((state) => {
      if (state.phase === "encoding") return state;
      return id === undefined || state.nodeId === id ? { ...IDLE } : state;
    }),
  ready: (duration, hasAudio) =>
    set((state) =>
      state.phase === "loading"
        ? { duration, hasAudio, start: 0, end: duration, phase: "editing", current: 0 }
        : { duration, hasAudio },
    ),
  setRange: (start, end) => set((state) => (state.phase === "editing" ? { start, end } : state)),
  setDiscardAudio: (discard) =>
    set((state) =>
      state.phase === "editing" ? { discardAudio: state.hasAudio && discard } : state,
    ),
  setCurrent: (time) => set((state) => (state.current === time ? state : { current: time })),
  seekTo: (time) => set((state) => ({ seek: { time, nonce: (state.seek?.nonce ?? 0) + 1 } })),
  setPhase: (phase) => set(phase === "encoding" ? { phase, progress: 0, error: null } : { phase }),
  setProgress: (percent) =>
    set((state) => {
      const next = Math.min(Math.max(Math.floor(percent), 0), 100);
      return next === state.progress ? state : { progress: next };
    }),
  fail: (message) => set({ phase: "editing", progress: 0, error: message }),
  reset: () => set({ ...IDLE }),
}));
