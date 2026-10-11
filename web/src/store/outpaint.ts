import { create } from "zustand";

type OutpaintStore = {
  /** 正在扩图的节点 id；没有就是不在扩图模式。只活在内存里，不存档 */
  nodeId: string | null;
  /** 进入某个节点的扩图模式（同一时刻只有一个节点） */
  enter: (id: string) => void;
  /** 退出扩图模式；传了 id 就只在正是这个节点时才退，免得误关别人的 */
  leave: (id?: string) => void;
};

/** 扩图模式：点功能区「扩图」进入，发送、Esc、取消选中、多选时退出（设计稿 6.17） */
export const useOutpaintStore = create<OutpaintStore>((set) => ({
  nodeId: null,
  enter: (id) => set({ nodeId: id }),
  leave: (id) =>
    set((state) => (id === undefined || state.nodeId === id ? { nodeId: null } : state)),
}));
