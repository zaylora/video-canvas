import type { CanvasVersion } from "@/utils/canvas/collaboration-version";

import { createContext, useContext } from "react";
import { useParams } from "react-router";

/**
 * 当前编辑器里这张画布的 ID。
 * 无限画布页的路由参数就是画布 ID；短剧工作台的路由参数是短剧 ID、画布 ID 在短剧详情里，
 * 所以编辑器自己经 context 把画布 ID 下发给节点，提交生成任务等需要画布 ID 的地方优先读它。
 */
export const CanvasIdContext = createContext<string | undefined>(undefined);

export const CanvasLibraryContext = createContext<{
  canvasId: string;
  epoch: CanvasVersion;
  allowed: boolean;
  flush: () => Promise<boolean>;
} | null>(null);

/** 协作生成需要先保存编辑，再固定请求对应的确认版本。 */
export const CanvasGenerationContext = createContext<{
  canGenerate: boolean;
  epoch: CanvasVersion;
  confirmGeneration: (
    id: string,
    model: string,
    input: Record<string, unknown>,
  ) => Promise<CanvasVersion | null>;
} | null>(null);

/** 取当前画布 ID：编辑器下发的优先，其次路由参数 :id（编辑器之外的旧用法） */
export function useCurrentCanvasId(): string | undefined {
  const fromEditor = useContext(CanvasIdContext);
  const { id } = useParams();
  return fromEditor ?? id;
}
