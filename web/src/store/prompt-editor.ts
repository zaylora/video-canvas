import { create } from "zustand";

type PromptEditorState = {
  /** 正在弹框编辑生成设置的节点 id；null 表示没有 */
  nodeId: string | null;
  /** 请求在弹框里编辑某个节点的提示词、模型和参数 */
  open: (nodeId: string) => void;
  /** 关闭 */
  close: () => void;
};

/**
 * 编辑器之外 → 节点的「弹框编辑生成设置」请求（短剧资产列表的卡片用）。
 * 生成面板的状态活在节点组件里，弹框也挂在节点身上，节点订阅这里看是不是在叫自己，同一时刻只开一个。
 */
export const usePromptEditor = create<PromptEditorState>((set) => ({
  nodeId: null,
  open: (nodeId) => set({ nodeId }),
  close: () => set({ nodeId: null }),
}));
