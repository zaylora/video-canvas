import { create } from "zustand";

/** 全局 WebSocket 的连接状态 */
export type ConnectionState =
  | "idle"
  | "connecting"
  | "connected"
  | "reconnecting"
  | "closed";

type WsState = {
  connection: ConnectionState;
  setConnection: (connection: ConnectionState) => void;
};

export const useWsStore = create<WsState>((set) => ({
  connection: "idle",
  setConnection: (connection) => set({ connection }),
}));
