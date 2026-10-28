import { createContext, useContext } from "react";

import type { CanvasTool } from "@/components/canvas";
import type { SaveStatus } from "@/hooks/use-canvas-persistence";
import type { CanvasHistory } from "@/hooks/use-canvas-history";
import type { FlowNode, NodeKind } from "@/types";

/**
 * 编辑器对外的命令。引用稳定，读的永远是画布当前的数据，不另存一份。
 * 既可以经 CanvasEditor 的 commandsRef 在编辑器外使用，也包含在 useCanvasEditor() 的返回值里。
 */
export type CanvasEditorCommands = {
  /** 读取当前的全部节点（含组、含选中状态），列表视图读这份，不另存 */
  getNodes: () => FlowNode[];
  /** 选中并把视口飞到该节点；节点不存在返回 false */
  focusNode: (id: string) => boolean;
  /** 只选中该节点（其余取消选中），不动视口；节点不存在返回 false */
  selectNode: (id: string) => boolean;
  /** 只把视口飞到该节点，不改选中；节点不存在返回 false */
  locateNode: (id: string) => boolean;
  /** 取消所有节点的选中（列表视图盖在画布上时，免得 Delete 键误删画布里选着的节点） */
  clearSelection: () => void;
};

/**
 * 编辑器的状态和动作，给 overlay / aside 插槽里的外壳组件用（顶栏、底栏、列表视图……）。
 * 状态部分（nodes、saveStatus、title、history 等）随画布变化重新渲染，需要少渲染的组件用 commands.getNodes() 自己读。
 */
export type CanvasEditorApi = CanvasEditorCommands & {
  canvasId: string;
  title: string;
  /** 改名并保存；失败会自己回滚并提示 */
  rename: (next: string) => void;
  saveStatus: SaveStatus;
  /** 立刻保存；返回是否保存成功 */
  saveNow: () => Promise<boolean>;
  /** 当前的全部节点（含组）；拖动时每帧都是新数组 */
  nodes: FlowNode[];
  tool: CanvasTool;
  setTool: (tool: CanvasTool) => void;
  history: CanvasHistory;
  minimap: boolean;
  setMinimap: (open: boolean) => void;
  /** 在视口中心新建一个指定种类的节点（页面的底栏、空状态用） */
  addAtCenter: (kind: NodeKind) => void;
  /** 选文件上传，节点落在视口中心 */
  uploadAtCenter: () => void;
  /** 打开资产库弹框，引入的素材落在视口中心 */
  openAssetPicker: () => void;
  /** 打开设置弹窗（里面按节点种类列出模型清单） */
  openSettings: () => void;
};

const CanvasEditorApiContext = createContext<CanvasEditorApi | null>(null);

export const CanvasEditorApiProvider = CanvasEditorApiContext.Provider;

/** 取编辑器的状态和命令；只能在 CanvasEditor 的 overlay / aside 插槽（或其内部）里用 */
export function useCanvasEditor(): CanvasEditorApi {
  const api = useContext(CanvasEditorApiContext);
  if (!api) throw new Error("useCanvasEditor 必须在 CanvasEditor 内部使用");
  return api;
}
