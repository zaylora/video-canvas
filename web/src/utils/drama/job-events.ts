import { listActiveJobs, mapJob } from "@/api/drama";
import type { BackendJobView } from "@/api/drama/type";
import { useDramaJobsStore } from "@/store/drama";

import { notifyDramaChanged } from "./episode-events";

/** 收到 WebSocket 的 drama.job 事件：并进该短剧的任务卡 */
export function handleDramaJobEvent(event: { drama_id: string; job: BackendJobView }) {
  useDramaJobsStore.getState().applyJob(event.drama_id, mapJob(event.job));
}

/**
 * 对账一部短剧：拉服务端进行中的任务，和本地任务卡合并。
 * 断线期间结束的任务在这里被发现（本地仍显示进行中、服务端已经没有），触发工作台重新拉详情。
 * 请求失败不打扰用户，下次连接 / 回到前台会再对一次；返回是否对账成功。
 */
export async function reconcileDrama(dramaId: string) {
  try {
    const active = await listActiveJobs(dramaId);
    useDramaJobsStore.getState().applyActive(dramaId, active);
    return true;
  } catch {
    /** 静默：HTTP 对账只是兜底。 */
    return false;
  }
}

/** 重连或回到前台时对账并刷新详情，补齐整批任务尚未结束时已写完的集。 */
export async function reconcileWatchedDramas() {
  const ids = Object.keys(useDramaJobsStore.getState().watching);
  await Promise.all(
    ids.map(async (id) => {
      if (await reconcileDrama(id)) notifyDramaChanged(id);
    }),
  );
}
