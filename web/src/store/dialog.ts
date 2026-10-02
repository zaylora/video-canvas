import { createElement, type ComponentType, type ReactNode } from "react";
import { create } from "zustand";

/**
 * 交给 store 管理的弹窗组件统一收到的三个控制属性。
 * 组件内把它们接到 Base UI 的 Dialog / AlertDialog 上：
 * `open={open} onOpenChange={(next) => !next && onClose()} onOpenChangeComplete={(next) => !next && onExited()}`
 */
export type DialogControl = {
  /** 是否打开。关闭时先变成 false 播退出动画，播完（onExited）才从 store 移除，内容不会提前消失 */
  open: boolean;
  /** 请求关闭（取消、点遮罩、Esc、操作成功后） */
  onClose: () => void;
  /** 退出动画播完：从 store 移除这个弹窗 */
  onExited: () => void;
};

type DialogEntry = {
  id: number;
  open: boolean;
  render: (control: DialogControl) => ReactNode;
};

export type DialogStore = {
  /** 当前挂着的弹窗，按打开顺序，后打开的叠在上面 */
  dialogs: DialogEntry[];
  /** 关闭（播退出动画），动画播完由 remove 真正移除 */
  close: (id: number) => void;
  /** 移除：只应由弹窗的 onExited 调用 */
  remove: (id: number) => void;
  /** 全部关闭：切换路由时由 DialogHost 调用 */
  closeAll: () => void;
};

let nextId = 1;

/**
 * 全站弹窗 store：页面级弹窗（删除确认、设置 Key、导入……）统一从这里开，DialogHost 负责渲染。
 * 这样页面不用为每个弹窗维护“目标对象 + busy + error”一组 state，关闭时内容也会留到退出动画播完。
 *
 * 注意：从另一个弹窗 / 抽屉**里面**打开的子弹窗（如渠道抽屉里的“设置 Key”、模型弹窗里的“测试”）
 * 不要走这里，要留在原组件树里做受控弹窗——挂到 DialogHost 后它和外层不在同一棵弹层树上，
 * 按 Esc 会把外层抽屉一起关掉。
 */
export const useDialogStore = create<DialogStore>((set) => ({
  dialogs: [],
  close: (id) =>
    set((state) => ({
      dialogs: state.dialogs.map((item) => (item.id === id ? { ...item, open: false } : item)),
    })),
  remove: (id) => set((state) => ({ dialogs: state.dialogs.filter((item) => item.id !== id) })),
  closeAll: () =>
    set((state) => ({ dialogs: state.dialogs.map((item) => ({ ...item, open: false })) })),
}));

/**
 * 打开一个弹窗。
 * @param component 弹窗组件：除业务 props 外接收 DialogControl
 * @param props 业务 props，打开时固定下来；需要实时数据的弹窗在组件内自己取
 * @returns 弹窗 id，可用 closeDialog 从外部关闭
 */
export function openDialog<P extends object>(
  component: ComponentType<P & DialogControl>,
  props: P,
): number {
  const id = nextId++;
  const render = (control: DialogControl) =>
    createElement(component, { ...props, ...control } as P & DialogControl);
  useDialogStore.setState((state) => ({
    dialogs: [...state.dialogs, { id, open: true, render }],
  }));
  return id;
}

/** 从外部关闭某个弹窗（播退出动画） */
export const closeDialog = (id: number) => useDialogStore.getState().close(id);
