import { createContext, useContext } from "react";

import type { DramaWorkspace } from "./use-workspace";

export const DramaWorkspaceContext = createContext<DramaWorkspace | null>(null);

/** 取工作台的状态与动作；只能在工作台页面内部用 */
export function useWorkspace(): DramaWorkspace {
  const value = useContext(DramaWorkspaceContext);
  if (!value) throw new Error("useWorkspace 必须在短剧工作台内部使用");
  return value;
}
