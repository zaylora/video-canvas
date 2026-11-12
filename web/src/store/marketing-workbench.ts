import { create } from "zustand";

import type { WorkbenchSelection } from "@/utils/marketing/workbench";

/** 检查器标签页：脚本、提示词；选中原片时只有拆解，不看这个值 */
export type WorkbenchTab = "script" | "prompt";

/** 舞台对外暴露的播放控制：检查器、快捷键都通过它操作视频 */
export interface WorkbenchControls {
  /** 跳到时间 t；play 为 true 时跳过去接着播 */
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
  tab: WorkbenchTab;
  /** 选中脚本看第几条成片（按时间正序的下标）；null 为最新一条 */
  take: number | null;
  /** 视频当前时间（秒）：检查器的当前镜头从它派生 */
  time: number;
  /** 视频正在播放 */
  playing: boolean;
  /** 选中项的视频还没加载好时先记下要跳到哪：视频就绪后由舞台执行 */
  pendingSeek: number | null;
  /** 舞台注册的播放控制；舞台不在（没有可播内容）时为 null */
  controls: WorkbenchControls | null;
  /** 换项目时整体清空 */
  reset: () => void;
  /** 切轮次：选中项由调用方按默认规则给出 */
  setRound: (roundId: string, selection: WorkbenchSelection | null) => void;
  /** 选中左栏一项：时间归零、检查器回到脚本页；seek 给了就在视频就绪后跳过去 */
  select: (selection: WorkbenchSelection | null, seek?: number) => void;
  setTab: (tab: WorkbenchTab) => void;
  setTake: (take: number | null) => void;
  setClock: (time: number, playing: boolean) => void;
  setPendingSeek: (time: number | null) => void;
  setControls: (controls: WorkbenchControls | null) => void;
};

const INITIAL = {
  roundId: null,
  selection: null,
  tab: "script" as WorkbenchTab,
  take: null,
  time: 0,
  playing: false,
  pendingSeek: null,
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
    set({ roundId, selection, tab: "script", take: null, time: 0, pendingSeek: null }),
  select: (selection, seek) =>
    set({
      selection,
      tab: "script",
      take: null,
      time: seek ?? 0,
      playing: false,
      pendingSeek: seek ?? null,
    }),
  setTab: (tab) => set({ tab }),
  setTake: (take) => set({ take, time: 0, playing: false }),
  setClock: (time, playing) => set({ time, playing }),
  setPendingSeek: (pendingSeek) => set({ pendingSeek }),
  setControls: (controls) => set({ controls }),
}));
