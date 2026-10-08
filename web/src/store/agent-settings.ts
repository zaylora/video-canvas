import { create } from "zustand";
import { persist } from "zustand/middleware";

/** 默认的本轮积分预算，与后端默认值一致 */
export const DEFAULT_BUDGET = 50;

/** 停靠宽度：默认、最小、最大（最大还不超过窗口的一半） */
export const DOCK_WIDTH = { default: 400, min: 320, max: 560 } as const;

/** 浮窗在画布里的位置：相对画布右下角的偏移（像素），拖动后记住 */
export type AgentPanelOffset = { dx: number; dy: number };

type AgentSettings = {
  /** 选用的 Agent 模型 key，空表示取第一个可用的 */
  modelKey: string;
  /** 每一轮运行的积分预算 */
  budget: number;
  /** 是否显示思考过程 */
  showThinking: boolean;
  /** 浮窗位置，null 是默认位置（右下角） */
  offset: AgentPanelOffset | null;
  /** 停靠到画布右侧（全高侧栏）；窗口太窄放不下时临时退回浮窗，偏好不变 */
  docked: boolean;
  /** 停靠时的宽度（像素） */
  dockWidth: number;
  /** 改设置 */
  patch: (next: Partial<Omit<AgentSettings, "patch">>) => void;
};

/** 浮窗的个人设置：存在本机，换设备不跟随 */
export const useAgentSettings = create<AgentSettings>()(
  persist(
    (set) => ({
      modelKey: "",
      budget: DEFAULT_BUDGET,
      showThinking: false,
      offset: null,
      docked: false,
      dockWidth: DOCK_WIDTH.default,
      patch: (next) => set(next),
    }),
    { name: "video-canvas-agent-settings" },
  ),
);
