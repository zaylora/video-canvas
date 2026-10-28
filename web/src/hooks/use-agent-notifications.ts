import { useEffect, useRef } from "react";

import { subscribeLiveAgentEvents } from "@/store/agent";
import { notificationFor, showNotice } from "@/utils/agent/watch";

/**
 * 页面在后台时，把 Agent 的两类实时事件变成系统通知：出现待批准的生成、生成全部结束后 Agent 新开一轮验收。
 * 只对实时推来的事件通知，回放的历史不会；只通知当前画布的会话；没有权限或浏览器不支持时静默跳过
 * （权限在用户第一次批准生成时请求，见 ensureNotificationPermission）。
 * @param canvasId 当前画布
 * @param canvasTitle 画布名，写进通知标题
 */
export function useAgentNotifications(canvasId: string, canvasTitle: string) {
  const title = useRef(canvasTitle);
  useEffect(() => {
    title.current = canvasTitle;
  }, [canvasTitle]);
  useEffect(
    () =>
      subscribeLiveAgentEvents((event) => {
        if (event.canvas_id !== canvasId) return;
        const notice = notificationFor(event, {
          canvasTitle: title.current,
          hidden: typeof document !== "undefined" && document.hidden,
        });
        if (notice) showNotice(notice);
      }),
    [canvasId],
  );
}
