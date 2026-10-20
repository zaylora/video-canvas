import { create } from "zustand";

type DoodleStore = {
  /** 正在涂鸦的图片节点 id；没有就是不在涂鸦模式。只活在内存里，不存档 */
  nodeId: string | null;
  /** 进入某个节点的涂鸦模式（同一时刻只有一个节点） */
  enter: (id: string) => void;
  /** 退出涂鸦模式；传了 id 就只在正是这个节点时才退，免得误关别人的 */
  leave: (id?: string) => void;
};

/** 涂鸦模式：点功能区「图片编辑」进入，保存、×、取消选中、多选时退出，未保存的笔迹直接丢弃（设计稿 §5） */
export const useDoodleStore = create<DoodleStore>((set) => ({
  nodeId: null,
  enter: (id) => set({ nodeId: id }),
  leave: (id) =>
    set((state) => (id === undefined || state.nodeId === id ? { nodeId: null } : state)),
}));
