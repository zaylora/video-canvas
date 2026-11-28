import type { CanvasPresenceState, ServerMessage } from "@/api/generation-task/type";
import type { TaskSocketClient } from "./socket-client";

type Listener = (event: ServerMessage) => void;
const rooms = new Map<string, Set<Listener>>();
let socket: TaskSocketClient | null = null;
const roomChannel = (canvasId: string) => `canvas-collaboration:${canvasId}`;

/** 使用应用现有的 WebSocket 连接。 */
export function attachCanvasRoomSocket(client: TaskSocketClient) {
  socket = client;
  return () => {
    if (socket === client) socket = null;
  };
}

/** 在应用连接重连后恢复房间订阅。 */
export function restoreCanvasRoomSubscriptions(reconnected: boolean) {
  for (const channel of rooms.keys()) socket?.room("subscribe", channel, reconnected);
}

/** 将房间信号发给对应画布。 */
export function dispatchCanvasRoomMessage(message: ServerMessage) {
  if (!("channel" in message) || !message.channel) return;
  for (const receive of rooms.get(message.channel) ?? []) receive(message);
}

/** 订阅指定画布的版本和权限通知。 */
export function watchCanvasRoom(canvasId: string, receive: Listener) {
  const channel = roomChannel(canvasId);
  let listeners = rooms.get(channel);
  if (!listeners) {
    listeners = new Set();
    rooms.set(channel, listeners);
    socket?.room("subscribe", channel);
  }
  listeners.add(receive);
  return () => {
    listeners.delete(receive);
    if (listeners.size === 0) {
      rooms.delete(channel);
      socket?.room("unsubscribe", channel);
    }
  };
}

/** 发送临时状态样本。 */
export function sendCanvasPresence(canvasId: string, data: CanvasPresenceState) {
  socket?.presence(roomChannel(canvasId), data);
}
