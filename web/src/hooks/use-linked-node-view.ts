import { useStore, type ReactFlowState } from "@xyflow/react";

import { useTask } from "@/store/tasks";
import type { CanvasNodeData } from "@/types";
import type { LinkedNode } from "@/utils/storyboard/shot-image";
import { deriveVideoNodeView } from "@/utils/tasks/node-view";

/** 画格只关心关联节点的这几项 */
type LinkedData = Pick<
  CanvasNodeData,
  "status" | "src" | "error" | "errorTaskRef" | "taskId" | "uploadProgress" | "label"
>;

const sameLinked = (a: LinkedData | null, b: LinkedData | null) =>
  a === b ||
  (!!a &&
    !!b &&
    a.status === b.status &&
    a.src === b.src &&
    a.error === b.error &&
    a.errorTaskRef === b.errorTaskRef &&
    a.taskId === b.taskId &&
    a.uploadProgress === b.uploadProgress &&
    a.label === b.label);

/**
 * 实时读行上关联的图片 / 视频节点的状态：只订阅这几项，用相等判断挡掉别的字段和位置变化，
 * 50 行都关联了节点时，一个节点的进度变化只重渲染它自己那一格（AC13）。
 * label 一起带出来：格子上要能说清「这一镜挂的是哪一段」，否则挂错了也看不出来。
 */
export function useLinkedNodeView(nodeId: string | undefined): LinkedNode | undefined {
  const data = useStore((state: ReactFlowState): LinkedData | null => {
    const d = nodeId
      ? (state.nodeLookup.get(nodeId)?.internals.userNode.data as CanvasNodeData | undefined)
      : undefined;
    return d
      ? {
          status: d.status,
          src: d.src,
          error: d.error,
          errorTaskRef: d.errorTaskRef,
          taskId: d.taskId,
          uploadProgress: d.uploadProgress,
          label: d.label,
        }
      : null;
  }, sameLinked);
  const task = useTask(data?.taskId);
  if (!nodeId) return undefined;
  if (!data) return { exists: false };
  // 耗时不显示，传 0 就行：进度条只用任务快照里的 progress
  return { exists: true, view: deriveVideoNodeView(data, task, 0), label: data.label };
}
