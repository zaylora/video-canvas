import { create } from "zustand";

import type { CreationMode } from "@/types";

type ComposerState = {
  /** 当前创作模式 */
  mode: CreationMode;
  /** 输入框里的草稿 */
  text: string;
  /** 外部填充输入框的次数：输入卡片看到它变了，就聚焦、把光标放到末尾并闪一下描边 */
  fillSeq: number;
  /** 切换创作模式 */
  setMode: (mode: CreationMode) => void;
  /** 更新草稿 */
  setText: (text: string) => void;
  /** 从输入框以外的地方（技能入口、重新编辑）填入草稿 */
  fill: (text: string) => void;
};

/**
 * 输入卡片的状态：创作页和对话页各渲染一个输入卡片，
 * 模式和草稿放在这里，页面之间切换时跟着走，不会丢。
 * 只存在内存里，刷新页面后重新开始。
 */
export const useComposerStore = create<ComposerState>((set) => ({
  mode: "agent",
  text: "",
  fillSeq: 0,
  setMode: (mode) => set({ mode }),
  setText: (text) => set({ text }),
  fill: (text) => set((state) => ({ text, fillSeq: state.fillSeq + 1 })),
}));
