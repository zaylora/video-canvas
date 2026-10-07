import type { CanvasPatchDto } from "@/api/agent/type";

type Listener = (patch: CanvasPatchDto) => void;

const listeners = new Map<string, Set<Listener>>();

/**
 * 订阅某张画布的 Agent 改动。WebSocket 是用户级的唯一一条，所有画布的改动都从它进来，
 * 这里按 canvas_id 分发给正在显示那张画布的页面。
 * @param canvasId 画布 ID
 * @param listener 收到改动时的回调
 * @returns 取消订阅
 */
export function subscribeCanvasPatch(canvasId: string, listener: Listener): () => void {
  let set = listeners.get(canvasId);
  if (!set) listeners.set(canvasId, (set = new Set()));
  set.add(listener);
  return () => {
    set.delete(listener);
    if (set.size === 0) listeners.delete(canvasId);
  };
}

/**
 * 分发一条 Agent 改动；没人订阅（画布没打开）就丢弃，下次打开画布时读到的就是最新的。
 * @param patch 后端推送的改动
 */
export function publishCanvasPatch(patch: CanvasPatchDto) {
  for (const listener of listeners.get(patch.canvas_id) ?? []) listener(patch);
}
