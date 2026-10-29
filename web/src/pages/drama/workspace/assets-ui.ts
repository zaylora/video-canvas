import { createContext, useContext, type RefObject } from "react";

import type { CanvasEditorCommands } from "@/components/canvas-editor/editor-api";
import type { NodeDrama } from "@/types";
import type { AssetType, EpisodeFilter } from "@/utils/drama/assets";

/** 资产区的视图状态：头部（在编辑器外）和列表 / 浮窗（在编辑器里）共用 */
export type AssetsUi = {
  view: "list" | "canvas";
  setView: (view: "list" | "canvas") => void;
  filter: EpisodeFilter;
  type: AssetType;
  setType: (type: AssetType) => void;
  /** 列表的多选模式 */
  multi: boolean;
  setMulti: (multi: boolean) => void;
  selected: ReadonlySet<string>;
  setSelected: (next: ReadonlySet<string>) => void;
  /** 对外命令（读节点、定位、选中）；编辑器挂载后才有 */
  commands: RefObject<CanvasEditorCommands | null>;
  /** 从列表新建节点前设置：新节点要带的短剧元数据（null = 未分类）；传 null 清除设置，之后新建的按默认「全集」 */
  setNewDrama: (pending: { drama: NodeDrama | null } | null) => void;
  /** 顶栏「全部生成」的位置；列表中的按钮从编辑器内透传过来，沿用实时节点数据 */
  generateAllTarget: HTMLDivElement | null;
};

export const AssetsUiContext = createContext<AssetsUi | null>(null);

/** 取资产区的视图状态；只能在资产区内部用 */
export function useAssetsUi(): AssetsUi {
  const value = useContext(AssetsUiContext);
  if (!value) throw new Error("useAssetsUi 必须在资产区内部使用");
  return value;
}
