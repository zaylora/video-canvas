import type { CanvasNodeData } from "@/types";

import { readOutputs } from "./outputs";

/** 功能区哪些按钮能点 */
export type ToolbarGate = {
  /** 要不要显示功能区：空节点（没有素材、也没有任何历史版本）没有可加工的东西，整条不出现 */
  visible: boolean;
  /** 加工类按钮、放大、下载、复制：生成中，或者还没有素材时不能点 */
  actionsDisabled: boolean;
  /** 历史按钮：一版结果都没有时不能展开；生成中仍可展开查看（切换版本由调用方拦） */
  historyDisabled: boolean;
  /** 历史里有几版 */
  historyCount: number;
};

/**
 * 按节点当前状态算功能区的可用性（设计稿 6.16 状态表）。
 * 「有素材」：文本看有没有正文，其余看有没有存下来的素材地址（本地 blob 还没传完，不算）。
 */
export function toolbarGate(data: CanvasNodeData): ToolbarGate {
  const hasOutput =
    data.kind === "script" ? !!data.text?.trim() : !!data.src && !data.src.startsWith("blob:");
  const running = data.status === "running";
  const historyCount = readOutputs(data).length;
  return {
    visible: hasOutput || historyCount > 0,
    actionsDisabled: running || !hasOutput,
    historyDisabled: historyCount === 0,
    historyCount,
  };
}
