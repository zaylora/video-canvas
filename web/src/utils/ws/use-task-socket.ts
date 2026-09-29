import { useEffect } from "react";

import { getWsTicket } from "@/api/ws";
import { useCreditsStore } from "@/store/credits";
import { useWsStore } from "@/store/ws";
import { getToken } from "@/utils/storage/token";

import { handleTaskView, reconcileActiveTasks } from "./task-events";
import { buildWsUrl, TaskSocketClient } from "./socket-client";

/** 拿 ticket 遇到 401 说明登录已失效，重试没有意义 */
const isUnauthorized = (error: unknown) =>
  typeof error === "object" &&
  error !== null &&
  (error as { status?: unknown }).status === 401;

/**
 * 登录后在应用根部建立唯一一条用户级 WebSocket：
 * 列表页、画布页共用。连接成功（含重连）与页面回到前台都会对账一次。
 * 组件卸载（离开登录态的路由）时关闭。
 */
export function useTaskSocket() {
  useEffect(() => {
    const setConnection = useWsStore.getState().setConnection;
    const client = new TaskSocketClient({
      fetchTicket: async () => (await getWsTicket()).ticket,
      buildUrl: (ticket) => buildWsUrl(import.meta.env.VITE_API_BASE_URL, ticket),
      createSocket: (url) => new WebSocket(url),
      onTask: (view) => handleTaskView(view, "live"),
      onOpen: () => {
        void reconcileActiveTasks();
        void useCreditsStore.getState().refresh();
      },
      onState: setConnection,
      canConnect: () => !!getToken(),
      isFatal: isUnauthorized,
    });
    client.start();

    const onVisible = () => {
      if (document.visibilityState !== "visible") return;
      client.reconnectNow();
      void reconcileActiveTasks();
    };
    const onOnline = () => client.reconnectNow();
    document.addEventListener("visibilitychange", onVisible);
    window.addEventListener("online", onOnline);

    return () => {
      document.removeEventListener("visibilitychange", onVisible);
      window.removeEventListener("online", onOnline);
      client.stop();
      setConnection("idle");
    };
  }, []);
}
