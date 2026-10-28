import { listSegments, mapSegment } from "@/api/drama";
import type { DramaSegmentEvent } from "@/api/drama/type";
import { parseSegmentKey, useDramaSegmentsStore } from "@/store/drama-segments";

/** 收到 WebSocket 的 drama.segment 事件：并进该短剧对应集的片段列表 */
export function handleDramaSegmentEvent(event: DramaSegmentEvent) {
  useDramaSegmentsStore
    .getState()
    .apply(event.drama_id, event.episode_no, mapSegment(event.segment));
}

/** 重新拉一集的片段（对账 / 首次载入）；失败不抛，返回是否成功 */
export async function reloadSegments(dramaId: string, no: number): Promise<boolean> {
  try {
    useDramaSegmentsStore.getState().setList(dramaId, no, await listSegments(dramaId, no));
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
