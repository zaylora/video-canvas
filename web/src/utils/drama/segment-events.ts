import { listSegments, mapSegment } from "@/api/drama";
import type { DramaSegmentEvent, Segment } from "@/api/drama/type";
import { segmentKey, parseSegmentKey, useDramaSegmentsStore } from "@/store/drama-segments";

import { notifyDramaChanged } from "./episode-events";

/** 只在影响分集摘要的状态变化时刷新；重复和过时的片段快照不触发请求。 */
function refreshSummary(
  dramaId: string,
  before: readonly Segment[] = [],
  after: readonly Segment[],
) {
  if (
    before.length !== after.length ||
    after.some((segment, index) => {
      const previous = before[index];
      return (
        previous.id !== segment.id ||
        previous.seq !== segment.seq ||
        previous.seconds !== segment.seconds ||
        previous.revision !== segment.revision ||
        previous.chosenTaskId !== segment.chosenTaskId ||
        previous.status !== segment.status
      );
    })
  )
    notifyDramaChanged(dramaId);
}

/** 收到 WebSocket 的 drama.segment 事件：并进该短剧对应集的片段列表 */
export function handleDramaSegmentEvent(event: DramaSegmentEvent) {
  const store = useDramaSegmentsStore.getState();
  const key = segmentKey(event.drama_id, event.episode_no);
  const before = store.byEpisode[key];
  store.apply(event.drama_id, event.episode_no, mapSegment(event.segment));
  refreshSummary(event.drama_id, before, useDramaSegmentsStore.getState().byEpisode[key]);
}

/** 重新拉一集的片段（对账 / 首次载入）；失败不抛，返回是否成功 */
export async function reloadSegments(dramaId: string, no: number): Promise<boolean> {
  try {
    const list = await listSegments(dramaId, no);
    const store = useDramaSegmentsStore.getState();
    const key = segmentKey(dramaId, no);
    const before = store.byEpisode[key];
    store.setList(dramaId, no, list);
    refreshSummary(dramaId, before, useDramaSegmentsStore.getState().byEpisode[key]);
    return true;
  } catch {
    return false;
  }
}

/** WebSocket 重连 / 页面回到前台时，对账所有打开着的片段工作区（断线期间的 take 变化在这里补上） */
export async function reconcileWatchedSegments() {
  const keys = Object.keys(useDramaSegmentsStore.getState().watching);
  await Promise.all(
    keys.map((key) => {
      const { dramaId, no } = parseSegmentKey(key);
      return reloadSegments(dramaId, no);
    }),
  );
}
