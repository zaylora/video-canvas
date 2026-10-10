import { create } from "zustand";

/**
 * 图片「扩图」的界面状态：同一时间只有一个节点在扩图。
 * 只存「哪个节点在扩图」，框、比例、提示词都是扩图面板自己的局部状态；
 * 不进画布数据，也不进撤销栈。
 */
type OutpaintState = {
  /** 正在扩图的节点，没有为 null */
  nodeId: string | null;
  /** 进入扩图 */
  begin: (nodeId: string) => void;
  /** 退出扩图；传了 nodeId 时，只有还是这个节点在扩图才退出，免得误关别人的 */
  end: (nodeId?: string) => void;
};

export const useOutpaintStore = create<OutpaintState>((set) => ({
  nodeId: null,
  begin: (nodeId) => set({ nodeId }),
  end: (nodeId) =>
    set((state) => (nodeId === undefined || state.nodeId === nodeId ? { nodeId: null } : state)),
}));
