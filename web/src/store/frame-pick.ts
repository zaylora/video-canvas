import { create } from "zustand";

import { useTrimStore } from "@/store/trim";

/** 一张暂存的帧：时刻、截好的文件，加给小块和悬浮预览用的本地地址 */
export type StagedFrame = { id: number; time: number; file: File; url: string };

type FramePickStore = {
  /** 正在选帧的节点；没有就是不在选帧模式。只活在内存里，不存档，不进撤销栈 */
  nodeId: string | null;
  /** 视频总时长（秒）；读出来之前为 0 */
  duration: number;
  /** 预览播放到哪（秒），播放头用 */
  current: number;
  /** 预览是否在播放；面板的播放键写它，节点里的 video 跟着播或停，video 自己停了也写回来 */
  playing: boolean;
  /** 给预览的定位请求：点胶片条、方向键时写入，nonce 每次不同，同一时刻连点两次也会重新定位 */
  seek: { time: number; nonce: number } | null;
  /** 暂存的帧，按截取顺序 */
  staged: StagedFrame[];
  /** 下一个暂存编号；一次会话内递增、丢弃后也不复用，小块上的 #N 才不会跳变 */
  nextId: number;
  /**
   * 进入某个节点的选帧模式（同一时刻只有一个会话）。
   * 别的节点正在修剪编码时不进，返回 false；修剪的编辑态会被顶掉。
   */
  enter: (id: string) => boolean;
  /** 退出选帧；传了 id 就只在正是这个节点时才退，免得误关别人的 */
  leave: (id?: string) => void;
  setDuration: (duration: number) => void;
  setCurrent: (time: number) => void;
  setPlaying: (playing: boolean) => void;
  seekTo: (time: number) => void;
  /** 暂存一帧，编号递增、不复用 */
  stage: (frame: Omit<StagedFrame, "id">) => void;
  /** 丢弃一张暂存帧并释放它的本地地址 */
  remove: (id: number) => void;
  /** 无条件清空，并释放所有暂存帧的本地地址 */
  reset: () => void;
};

const IDLE = {
  nodeId: null,
  duration: 0,
  current: 0,
  seek: null,
  playing: false,
  nextId: 1,
} as const;

const release = (frames: readonly StagedFrame[]) => {
  for (const frame of frames) URL.revokeObjectURL(frame.url);
};

/**
 * 选帧模式（设计稿「截取帧」）：点功能区「截取帧 → 自定义」进入；
 * 确认、Esc、取消选中、多选时退出。和视频修剪互斥：同一时刻只有一个模式。
 */
export const useFramePickStore = create<FramePickStore>((set, get) => ({
  ...IDLE,
  staged: [],
  enter: (id) => {
    const trim = useTrimStore.getState();
    if (trim.nodeId !== null && trim.phase === "encoding") return false;
    trim.reset();
    release(get().staged);
    set({ ...IDLE, nodeId: id, staged: [] });
    return true;
  },
  leave: (id) => {
    const state = get();
    if (id !== undefined && state.nodeId !== id) return;
    release(state.staged);
    set({ ...IDLE, staged: [] });
  },
  setDuration: (duration) => set({ duration }),
  setCurrent: (time) => set((state) => (state.current === time ? state : { current: time })),
  setPlaying: (playing) => set((state) => (state.playing === playing ? state : { playing })),
  seekTo: (time) => set((state) => ({ seek: { time, nonce: (state.seek?.nonce ?? 0) + 1 } })),
  stage: (frame) =>
    set((state) => ({
      staged: [...state.staged, { ...frame, id: state.nextId }],
      nextId: state.nextId + 1,
    })),
  remove: (id) =>
    set((state) => {
      release(state.staged.filter((frame) => frame.id === id));
      return { staged: state.staged.filter((frame) => frame.id !== id) };
    }),
  reset: () => {
    release(get().staged);
    set({ ...IDLE, staged: [] });
  },
}));
