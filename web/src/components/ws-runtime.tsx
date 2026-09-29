import { useEffect } from "react";

import { useTaskSocket } from "@/utils/ws/use-task-socket";
import { useCreditsStore } from "@/store/credits";

/** 登录后常驻的运行时：任务 WebSocket + 积分余额，不渲染任何东西 */
export function WsRuntime() {
  useTaskSocket();
  useEffect(() => {
    void useCreditsStore.getState().refresh();
  }, []);
  return null;
}
