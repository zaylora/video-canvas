import { useEffect } from "react";

import { useTaskSocket } from "@/utils/ws/use-task-socket";
import { useCreditsStore } from "@/store/credits";
import { usePaymentStore } from "@/store/payment";

/** 登录后常驻的运行时：任务 WebSocket + 积分余额 + 积分商城数据，不渲染任何东西 */
export function WsRuntime() {
  useTaskSocket();
  useEffect(() => {
    void useCreditsStore.getState().refresh();
    // 积分商城是否开启决定了点积分胶囊打开商城还是余额小卡片，也要恢复上次没付完的订单
    void usePaymentStore
      .getState()
      .init()
      .catch(() => undefined);
  }, []);
  return null;
}
