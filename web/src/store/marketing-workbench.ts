import { create } from "zustand";

import type { WorkbenchSelection } from "@/utils/marketing/workbench";

/** 舞台模式：单看一条，或和原片并排对比 */
export type WorkbenchMode = "single" | "compare";

/** 检查器标签页：脚本、提示词、镜头对照（对比模式才有）；选中原片时只有拆解，不看这个值 */
export type WorkbenchTab = "script" | "prompt" | "pair";

/** 舞台对外暴露的播放控制：检查器、时间轴、快捷键都通过它操作视频 */
export interface WorkbenchControls {
  /** 跳到成片（或原片）时间 t；play 为 true 时跳过去接着播 */
  seek: (t: number, play?: boolean) => void;
  /** 播放 / 暂停切换 */
  toggle: () => void;
  /** 暂停 */
  pause: () => void;
  /** 进 / 出全屏 */
  fullscreen: () => void;
}

type WorkbenchState = {
  /** 当前看的轮次 */
  roundId: string | null;
  /** 左栏选中项 */
  selection: WorkbenchSelection | null;
  mode: WorkbenchMode;
  tab: WorkbenchTab;
  /** 选中脚本看第几条成片（按时间正序的下标）；null 为最新一条 */
  take: number | null;
  /** 对比时哪边出声 */
  audio: "main" | "ref";
  /** 主视频当前时间（秒）：时间轴播放头、检查器当前镜头都从它派生 */
  time: number;
  /** 主视频正在播放 */
  playing: boolean;
  /** 选中项的视频还没加载好时先记下要跳到哪：视频就绪后由舞台执行 */
  pendingSeek: number | null;
  /** 让检查器滚到某一镜并聚焦旁白（对比层「去改」）；nonce 每次加一，重复点同一镜也生效 */
  focusShot: { index: number; nonce: number } | null;
  /** 舞台注册的播放控制；舞台不在（没有可播内容）时为 null */
  controls: WorkbenchControls | null;
  /** 换项目时整体清空 */
  reset: () => void;
  /** 切轮次：选中项由调用方按默认规则给出，模式回到单看 */
  setRound: (roundId: string, selection: WorkbenchSelection | null) => void;
  /** 选中左栏一项：时间归零、回到单看、检查器回到脚本页 */
  select: (selection: WorkbenchSelection | null, seek?: number) => void;
  setMode: (mode: WorkbenchMode) => void;
  setTab: (tab: WorkbenchTab) => void;
  setAudio: (audio: "main" | "ref") => void;
  setTake: (take: number | null) => void;
  setClock: (time: number, playing: boolean) => void;
  setPendingSeek: (time: number | null) => void;
  focus: (index: number) => void;
  setControls: (controls: WorkbenchControls | null) => void;
};

const INITIAL = {
  roundId: null,
  selection: null,
  mode: "single" as WorkbenchMode,
  tab: "script" as WorkbenchTab,
  take: null,
  audio: "main" as const,
  time: 0,
  playing: false,
  pendingSeek: null,
  focusShot: null,
  controls: null,
};

/**
 * 营销创作台的视图状态（设计 docs/design/营销工作室/创作台.md §5）：
 * 选中项与当前时间是三栏共享的唯一来源，左栏、舞台、检查器的联动都从这里派生。
 * 只在内存里，刷新回到默认选中。
 */
export const useWorkbenchStore = create<WorkbenchState>((set) => ({
  ...INITIAL,
  reset: () => set(INITIAL),
  setRound: (roundId, selection) =>
    set({
      roundId,
      selection,
      mode: "single",
      tab: "script",
      take: null,
      time: 0,
      pendingSeek: null,
    }),
  select: (selection, seek) =>
    set({
      selection,
      mode: "single",
      tab: "script",
      take: null,
      time: seek ?? 0,
      playing: false,
      pendingSeek: seek ?? null,
    }),
  setMode: (mode) =>
    set((state) => ({
      mode,
      tab: mode === "compare" ? "pair" : state.tab === "pair" ? "script" : state.tab,
    })),
  setTab: (tab) => set({ tab }),
  setAudio: (audio) => set({ audio }),
  setTake: (take) => set({ take, time: 0, playing: false }),
  setClock: (time, playing) => set({ time, playing }),
  setPendingSeek: (pendingSeek) => set({ pendingSeek }),
  focus: (index) =>
    set((state) => ({
      tab: "script",
      focusShot: { index, nonce: (state.focusShot?.nonce ?? 0) + 1 },
    })),
  setControls: (controls) => set({ controls }),
}));
