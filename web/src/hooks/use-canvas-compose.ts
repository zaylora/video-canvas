import { useCallback, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";

import { cancelCompose, composeCanvas, getActiveCompose } from "@/api/compose";
import type { CanvasComposeJob, ComposeMode } from "@/api/compose/type";
import { useCurrentCanvasId } from "@/utils/canvas/canvas-id";
import { ApiError } from "@/utils/requests/request";
import type { ComposeCandidate } from "@/utils/storyboard/compose";

/** 合成是非终态时多久查一次进度；ffmpeg 一般几十秒内结束，1.5 秒够用且不吵服务端 */
const POLL_MS = 1500;

/** 后端「这张表正在合成」的业务错误码 */
const COMPOSE_BUSY = 30018;

const messageOf = (err: unknown) =>
  err instanceof ApiError ? err.message : "合成失败，请稍后重试";

/**
 * 一个节点上的成片合成状态：打开时补查、提交、轮询进度、取消。
 *
 * 分镜表弹窗和「成片合成」节点共用这一份：两者差别只有触发方式（mode），
 * 收集素材都在调用方做完，这里只认「NodeID + 有序 assetIds」。
 *
 * 进度用轮询而不是 WebSocket：合成是低频操作，轮询不引入新的事件通道，
 * 也就不会碰到画布补丁的三方合并逻辑。
 */
export function useCanvasCompose(
  nodeId: string,
  candidates: ComposeCandidate[],
  mode: ComposeMode = "storyboard",
) {
  const canvasId = useCurrentCanvasId();
  const [job, setJob] = useState<CanvasComposeJob | null>(null);
  const [pending, setPending] = useState(false);
  const [loaded, setLoaded] = useState(false);

  // 依赖用拼好的字符串：候选数组每次渲染都是新引用，直接当依赖会反复触发请求
  const assetKey = useMemo(() => candidates.map((item) => item.assetId).join(","), [candidates]);
  const assetIds = useMemo(() => (assetKey ? assetKey.split(",") : []), [assetKey]);

  const busy = job?.status === "queued" || job?.status === "running";

  const refresh = useCallback(async () => {
    if (!canvasId) return;
    try {
      const res = await getActiveCompose(canvasId, nodeId, assetIds);
      setJob(res ?? null);
    } catch {
      // 查询失败不打扰用户：下一次轮询或重新打开弹窗会再试
    } finally {
      setLoaded(true);
    }
  }, [canvasId, nodeId, assetIds]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  // 非终态才轮询，终态后自动停
  useEffect(() => {
    if (!busy || !canvasId) return;
    const timer = window.setInterval(() => void refresh(), POLL_MS);
    return () => window.clearInterval(timer);
  }, [busy, canvasId, refresh]);

  const compose = useCallback(async () => {
    if (!canvasId || pending || busy || assetIds.length === 0) return;
    setPending(true);
    try {
      const res = await composeCanvas(canvasId, {
        node_id: nodeId,
        asset_ids: assetIds,
        seqs: candidates.map((item) => item.seq),
        mode,
      });
      setJob(res);
    } catch (err) {
      // 已经被另一个窗口提交过：不报错，直接把那条任务查回来显示
      if (err instanceof ApiError && err.code === COMPOSE_BUSY) {
        await refresh();
      } else {
        toast.error(messageOf(err));
      }
    } finally {
      setPending(false);
    }
  }, [canvasId, nodeId, assetIds, candidates, mode, pending, busy, refresh]);

  const cancel = useCallback(async () => {
    if (!canvasId || !job || !busy) return;
    try {
      await cancelCompose(canvasId, job.id);
      setJob({ ...job, status: "canceled" });
    } catch {
      toast.error("取消失败，请稍后重试");
    }
  }, [canvasId, job, busy]);

  return { job, pending, loaded, busy, compose, cancel, refresh };
}
