import { create } from "zustand";

import type { Guide } from "@/utils/canvas/alignment";

type AlignGuidesStore = {
  /** 拖动节点时正在显示的对齐辅助线，最多 x、y 各一条。只活在内存里，不存档、不进画布数据 */
  guides: Guide[];
  /** 内容没变就不写：拖动每帧都会调，写了就会让订阅的覆盖层白白重渲染 */
  setGuides: (guides: Guide[]) => void;
  clear: () => void;
};

const same = (a: Guide[], b: Guide[]) =>
  a.length === b.length &&
  a.every(
    (guide, i) =>
      guide.axis === b[i].axis &&
      guide.value === b[i].value &&
      guide.from === b[i].from &&
      guide.to === b[i].to,
  );

/**
 * 辅助线单独放一个 store，而不是 Flow 组件的 useState：
 * 只有覆盖层订阅它，拖动中 FlowCanvas 不会因为辅助线每帧重渲染（见画布拖动卡顿 P1 设计）。
 */
export const useAlignGuidesStore = create<AlignGuidesStore>((set, get) => ({
  guides: [],
  setGuides: (guides) => {
    if (!same(get().guides, guides)) set({ guides });
  },
  clear: () => {
    if (get().guides.length > 0) set({ guides: [] });
  },
}));
