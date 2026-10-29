import { createContext, useContext } from "react";

/**
 * 「打开媒体预览灯箱」的入口：灯箱状态在 flow.tsx，节点功能区的「放大」按钮要用它，
 * 效果和双击节点一样。节点没有可预览的素材时调用它什么也不会发生。
 */
const NodePreviewContext = createContext<((nodeId: string) => void) | null>(null);
export const NodePreviewProvider = NodePreviewContext.Provider;
export const useNodePreview = () => useContext(NodePreviewContext);
