import service from "@/utils/requests/service";

import type { CanvasComposeJob, CanvasComposeRequest } from "./type";

/**
 * 提交一次成片合成：后端建任务后立刻返回 queued 快照，ffmpeg 在后台跑。
 * 不扣积分，错误由调用方自己处理，不弹默认 toast。
 */
export const composeCanvas = (canvasId: string, body: CanvasComposeRequest) =>
  service.post<CanvasComposeJob>(`/canvas/${canvasId}/compose`, body, { silent: true });

/** 按节点查任务（弹窗打开、刷新页面时恢复），并带回成片是否过期；没有任务时返回 null */
export const getActiveCompose = (canvasId: string, nodeId: string, assetIds: string[]) =>
  service.get<CanvasComposeJob | null>(
    `/canvas/${canvasId}/compose/active`,
    { node_id: nodeId, asset_ids: assetIds.join(",") },
    { silent: true },
  );

/** 按 id 读任务快照（断线后对账） */
export const getComposeJob = (canvasId: string, jobId: string) =>
  service.get<CanvasComposeJob>(`/canvas/${canvasId}/compose/${jobId}`, undefined, {
    silent: true,
  });

/** 取消一条还没落终态的合成任务 */
export const cancelCompose = (canvasId: string, jobId: string) =>
  service.post<{ canceled: boolean }>(`/canvas/${canvasId}/compose/${jobId}/cancel`, undefined, {
    silent: true,
  });
