import { create } from "zustand";

type NodeHistoryStore = {
  /** 正在看历史记录的节点 id；null 是关着。只活在内存里，不存档 */
  nodeId: string | null;
  open: (nodeId: string) => void;
  close: () => void;
};

/** 「历史记录」弹窗开在哪个节点上；弹窗本身挂在画布层，节点右上角的徽标只负责打开 */
export const useNodeHistoryStore = create<NodeHistoryStore>((set) => ({
  nodeId: null,
  open: (nodeId) => set({ nodeId }),
  close: () => set({ nodeId: null }),
}));
