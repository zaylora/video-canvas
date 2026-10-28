import { create } from "zustand";

/**
 * 快捷键说明弹窗是否打开。放在 store 里而不是 Flow 的 state：
 * 编辑器（按 ? 键）和外壳（顶栏、视图控件的按钮）都要打开它，而开关弹窗不该让整张画布重渲染。
 */
export const useShortcutsDialog = create<{
  open: boolean;
  setOpen: (open: boolean) => void;
}>((set) => ({ open: false, setOpen: (open) => set({ open }) }));

/** 打开快捷键说明；引用稳定，直接作为 CanvasEditor 的 onOpenShortcuts */
export const openShortcuts = () => useShortcutsDialog.getState().setOpen(true);
