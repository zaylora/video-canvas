import { create } from "zustand";

type GridSplitStore = {
  /** 正在切分的图片节点 id；没有就是不在切分模式。只活在内存里，不存档 */
  nodeId: string | null;
  /** 切成几行，进入时定下，之后不变（要换宫格就重新进入） */
  rows: number;
  /** 切成几列 */
  cols: number;
  /** 进入某个节点的宫格切分（同一时刻只有一个节点） */
  enter: (id: string, rows: number, cols: number) => void;
  /** 退出切分模式；传了 id 就只在正是这个节点时才退，免得误关别人的 */
  leave: (id?: string) => void;
};

/** 宫格切分模式：点功能区「宫格切分」选好行列后进入，确认、取消、取消选中、多选时退出（设计稿 §4） */
export const useGridSplitStore = create<GridSplitStore>((set) => ({
  nodeId: null,
  rows: 3,
  cols: 3,
  enter: (id, rows, cols) => set({ nodeId: id, rows, cols }),
  leave: (id) =>
    set((state) => (id === undefined || state.nodeId === id ? { nodeId: null } : state)),
}));
