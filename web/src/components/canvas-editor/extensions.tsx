import { createContext, useContext, type ReactNode } from "react";

import type { CanvasNode, CanvasNodeData } from "@/types";
import type { ToolbarGate } from "@/utils/canvas/node-toolbar-state";

/**
 * 画布编辑器留给页面的扩展点（节点层面）。
 * 节点由 xyflow 的 nodeTypes 渲染，页面没法把 props 直接递进去，所以经 context 下发；
 * 编辑器自己（CanvasEditor）在最外层提供，页面只管把函数传给 CanvasEditor。
 */

/** 节点功能区追加按钮时拿到的节点信息 */
export type NodeToolbarExtraProps = {
  id: string;
  data: CanvasNodeData;
};

export type NodeExtensions = {
  /**
   * 追加到节点功能区最右侧的内容（接在查看类按钮之后，自动补分隔线）。
   * 返回 null 表示这个节点不追加。功能区本身不显示时（空节点、导演台）也不会显示追加内容。
   * 返回的内容在功能区胶囊里，用 ChromeButton / ChromeTooltip 保持视觉一致。
   */
  toolbarExtra?: (node: NodeToolbarExtraProps) => ReactNode;
  /**
   * 默认规则不显示功能区的节点（空节点：没有素材也没有历史）要不要仍然显示功能区。
   * 返回 true 时只出现 toolbarExtra 的内容（加工类、查看类按钮没有素材可用，不摆）；
   * 默认规则已经显示功能区的节点不受它影响。不传就保持默认规则。
   */
  toolbarVisible?: (data: CanvasNodeData) => boolean;
};

/** 功能区怎么摆：完整 / 只有页面追加的内容 / 不显示 */
export type ToolbarMode = "full" | "extra-only" | "hidden";

/**
 * 综合默认规则（toolbarGate）和页面的 toolbarVisible 决定功能区怎么摆。
 * 默认规则显示的一律完整显示；默认隐藏的，页面放行才显示追加内容。
 */
export function resolveToolbar(
  gate: ToolbarGate,
  data: CanvasNodeData,
  toolbarVisible: NodeExtensions["toolbarVisible"],
): ToolbarMode {
  if (gate.visible) return "full";
  return toolbarVisible?.(data) ? "extra-only" : "hidden";
}

const NO_EXTENSIONS: NodeExtensions = {};

const NodeExtensionsContext = createContext<NodeExtensions>(NO_EXTENSIONS);

export const NodeExtensionsProvider = NodeExtensionsContext.Provider;

/** 节点内部读页面传下来的扩展；编辑器之外使用时是空扩展 */
export const useNodeExtensions = () => useContext(NodeExtensionsContext);

/**
 * 新建节点默认数据钩子的落地：比较改动前后的素材节点，凡是 id 之前不存在的，交给 onCreate 改写。
 * 只在经菜单 / 上传 / 粘贴 / 引入资产这些「用户新建」路径上套用；Agent 补丁、撤销重做不经过这里。
 * 没有新节点（或没给钩子）时原样返回 next，引用不变，不会引起多余的渲染。
 */
export function applyNodeDefaults(
  previous: CanvasNode[],
  next: CanvasNode[],
  onCreate: ((node: CanvasNode) => CanvasNode) | undefined,
): CanvasNode[] {
  if (!onCreate || next === previous) return next;
  const known = new Set(previous.map((node) => node.id));
  let changed = false;
  const result = next.map((node) => {
    if (known.has(node.id)) return node;
    changed = true;
    return onCreate(node);
  });
  return changed ? result : next;
}
