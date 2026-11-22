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
  // 导演台没有素材可加工，不出功能区：进编辑器的入口在节点正文的按钮上，导出在编辑器里
  if (data.kind === "director") return { visible: false, actionsDisabled: true };
  // 分镜表空表也出功能区：「编辑分镜」随时能点
  if (data.kind === "storyboard") return { visible: true, actionsDisabled: false };
  // 成片节点没有可加工的原素材：合成与下载都在正文的合成条上，出功能区只会让两个入口打架
  if (data.kind === "compose") return { visible: false, actionsDisabled: true };
  const hasOutput =
    data.kind === "script" ? !!data.text?.trim() : !!data.src && !data.src.startsWith("blob:");
  const running = data.status === "running";
  const historyCount = readOutputs(data).length;
  return {
    visible: hasOutput || historyCount > 0,
    actionsDisabled: running || !hasOutput,
  };
}

/** 没有处理函数就不出现的查看类按钮 */
const CONDITIONAL_TAIL: ReadonlySet<ToolbarTail> = new Set(["addToLibrary", "replaceFromLibrary"]);

/**
 * 功能区右半边实际要显示的查看类按钮。放大、下载、复制没有处理函数时仍显示（点了提示「敬请期待」）；
 * 「加入资产库」「从资产库替换」是有条件的功能，调用方没给处理函数（节点没有可入库的素材、不能换图）就不出现。
 * @param tail 配置里这种节点的查看类按钮
 * @param handlers 调用方给了处理函数的按钮
 */
export function visibleTail(
  tail: ToolbarTail[],
  handlers: Partial<Record<ToolbarTail, () => void>> | undefined,
): ToolbarTail[] {
  return tail.filter((item) => !CONDITIONAL_TAIL.has(item) || !!handlers?.[item]);
}
