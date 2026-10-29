import { listActiveJobs, mapJob } from "@/api/drama";
import type { BackendJobView } from "@/api/drama/type";
import { useDramaJobsStore } from "@/store/drama";

/** 收到 WebSocket 的 drama.job 事件：并进该短剧的任务卡 */
export function handleDramaJobEvent(event: { drama_id: string; job: BackendJobView }) {
  useDramaJobsStore.getState().applyJob(event.drama_id, mapJob(event.job));
}

/**
 * 对账一部短剧：拉服务端进行中的任务，和本地任务卡合并。
 * 断线期间结束的任务在这里被发现（本地仍显示进行中、服务端已经没有），触发工作台重新拉详情。
 * 请求失败不打扰用户，下次连接 / 回到前台会再对一次。
 */
export async function reconcileDrama(dramaId: string) {
  try {
    const active = await listActiveJobs(dramaId);
    useDramaJobsStore.getState().applyActive(dramaId, active);
  } catch {
    // 静默：HTTP 对账只是兜底
  }
}

/** WebSocket 连接成功（含重连）或页面回到前台时，对账所有打开着的工作台 */
export async function reconcileWatchedDramas() {
  const ids = Object.keys(useDramaJobsStore.getState().watching);
  await Promise.all(ids.map(reconcileDrama));
}
