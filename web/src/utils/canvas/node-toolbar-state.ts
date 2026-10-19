import type { ToolbarTail } from "@/constants/node-toolbar";
import type { CanvasNodeData } from "@/types";

import { readOutputs } from "./outputs";

/** 功能区哪些按钮能点 */
export type ToolbarGate = {
  /** 要不要显示功能区：空节点（没有素材、也没有任何历史版本）没有可加工的东西，整条不出现 */
  visible: boolean;
  /** 加工类按钮、放大、下载、复制：生成中，或者还没有素材时不能点 */
  actionsDisabled: boolean;
};

/**
 * 按节点当前状态算功能区的可用性（设计稿 6.16 状态表）。
 * 「有素材」：文本看有没有正文，其余看有没有存下来的素材地址（本地 blob 还没传完，不算）。
 */
export function toolbarGate(data: CanvasNodeData): ToolbarGate {
  // 导演台没有素材可加工，功能区放的是「打开导演台」「导出视频」，始终可用
  if (data.kind === "director") return { visible: true, actionsDisabled: false };
  const hasOutput =
    data.kind === "script" ? !!data.text?.trim() : !!data.src && !data.src.startsWith("blob:");
  const running = data.status === "running";
  const historyCount = readOutputs(data).length;
  return {
    visible: hasOutput || historyCount > 0,
    actionsDisabled: running || !hasOutput,
  };
}

/**
 * 功能区右半边实际要显示的查看类按钮。放大、下载、复制没有处理函数时仍显示（点了提示「敬请期待」）；
 * 「加入资产库」是有条件的功能，调用方没给处理函数（节点没有可入库的素材）就不出现。
 * @param tail 配置里这种节点的查看类按钮
 * @param handlers 调用方给了处理函数的按钮
 */
export function visibleTail(
  tail: ToolbarTail[],
  handlers: Partial<Record<ToolbarTail, () => void>> | undefined,
): ToolbarTail[] {
  return tail.filter((item) => item !== "addToLibrary" || !!handlers?.addToLibrary);
}
