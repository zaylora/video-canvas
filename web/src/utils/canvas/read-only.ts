import { createContext, useContext } from "react";

/**
 * 画布此刻是不是只能看。
 *
 * 节点由 xyflow 按 nodeTypes 渲染，页面没法把 props 直接递进每个节点，所以经 context 下发；
 * 节点里的就地写入（标题改名、空节点放素材、功能区）都读它。
 * 真正的兜底不在这里：编辑器把所有写入口（节点变更、连线变更、保存与草稿）都按只读掐掉了，
 * 这个 context 只是让「点不动的按钮」别看起来像能点。
 */
const ReadOnlyContext = createContext(false);

export const CanvasReadOnlyProvider = ReadOnlyContext.Provider;

export const useCanvasReadOnly = () => useContext(ReadOnlyContext);

const SharedCanvasContext = createContext(false);
export const SharedCanvasProvider = SharedCanvasContext.Provider;
export const useSharedCanvas = () => useContext(SharedCanvasContext);
