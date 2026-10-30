import type { DramaEpisodeEvent } from "@/api/drama/type";

/** 按短剧分发详情变更，只通知当前打开的工作台。 */
const listeners = new Map<string, Set<() => void>>();

/** 订阅短剧详情变更；卸载时取消，重新进入工作台时由首次读取获取最新数据。 */
export function subscribeDramaChanges(dramaId: string, listener: () => void): () => void {
  let group = listeners.get(dramaId);
  if (!group) listeners.set(dramaId, (group = new Set()));
  group.add(listener);
  return () => {
    group.delete(listener);
    if (group.size === 0) listeners.delete(dramaId);
  };
}

/** 分集写入、片段完成或断线对账后，通知工作台重新读取服务端摘要。 */
export function notifyDramaChanged(dramaId: string): void {
  for (const listener of listeners.get(dramaId) ?? []) listener();
}

/** 分集保存事务已提交，立即刷新该短剧，独立于整批任务百分比。 */
export function handleDramaEpisodeEvent(event: DramaEpisodeEvent): void {
  notifyDramaChanged(event.drama_id);
}
