import type { CanvasVersion } from "@/utils/canvas/collaboration-version";

import type { CanvasPresenceEvent, CanvasPresenceState } from "@/api/generation-task/type";
import { sendCanvasPresence, watchCanvasRoom } from "./canvas-room";

const sampleInterval = 90;
const heartbeatInterval = 2000;
const expiry = 6000;

type OnlineAccount = { id: string; username?: string };

/** 将普通节点和传送到文档外层的提示词弹窗映射到所属节点。 */
export function canvasEditingNode(flow: HTMLElement, canvasId: string, target: EventTarget | null) {
  if (!(target instanceof Element)) return undefined;
  const field = target.closest<HTMLElement>("textarea:enabled, [contenteditable=true]");
  if (!field) return undefined;
  if (field.dataset.canvasPrompt === canvasId) return field.dataset.canvasNode;
  if (flow.contains(field)) return field.closest<HTMLElement>(".react-flow__node")?.dataset.id;
  return undefined;
}

/** 临时状态单独订阅，避免用光标刷新整个画布。 */
export class CanvasRoomPresence {
  private readonly canvasId: string;
  private readonly members = new Map<string, { state: CanvasPresenceEvent; expires: number }>();
  private readonly attendance = new Map<string, { username?: string; expires: number }>();
  private readonly listeners = new Set<() => void>();
  private readonly rosterListeners = new Set<() => void>();
  private readonly followListeners = new Set<() => void>();
  private snapshot: CanvasPresenceEvent[] = [];
  private roster: OnlineAccount[] = [];
  private local: CanvasPresenceState;
  private timer?: ReturnType<typeof setTimeout>;
  private following: string | null = null;

  constructor(canvasId: string, epoch: CanvasVersion) {
    this.canvasId = canvasId;
    this.local = { epoch };
  }

  getSnapshot = () => this.snapshot;
  getRoster = () => this.roster;
  getFollowing = () => this.following;

  member(id: string) {
    return this.members.get(id)?.state;
  }

  subscribe = (listen: () => void) => {
    this.listeners.add(listen);
    return () => {
      this.listeners.delete(listen);
    };
  };

  subscribeRoster = (listen: () => void) => {
    this.rosterListeners.add(listen);
    return () => {
      this.rosterListeners.delete(listen);
    };
  };

  subscribeFollowing = (listen: () => void) => {
    this.followListeners.add(listen);
    return () => {
      this.followListeners.delete(listen);
    };
  };

  follow(id: string) {
    if (!this.member(id)?.view) return false;
    this.setFollowing(id);
    return true;
  }

  stopFollowing = () => this.setFollowing(null);

  private setFollowing(id: string | null) {
    if (id === this.following) return;
    this.following = id;
    for (const listen of this.followListeners) listen();
    this.schedule();
  }

  receive(state: CanvasPresenceEvent, now = performance.now()) {
    if (state.epoch !== this.local.epoch) return;
    if (state.onlineOnly) {
      const previous = this.attendance.get(state.actorId);
      const username = state.actorUsername || previous?.username;
      this.attendance.set(state.actorId, { username, expires: now + expiry });
      if (!previous || username !== previous.username) this.publishRoster();
      return;
    }
    const previous = this.members.get(state.actorId)?.state;
    const actorUsername = state.actorUsername || previous?.actorUsername;
    this.members.set(state.actorId, { state: { ...state, actorUsername }, expires: now + expiry });
    if (!state.view && this.following === state.actorId) this.stopFollowing();
    this.publish(!previous || actorUsername !== previous.actorUsername);
  }

  expire(now = performance.now()) {
    let changed = false;
    let attendanceChanged = false;
    for (const [id, member] of this.attendance) {
      if (member.expires <= now)
        attendanceChanged = this.attendance.delete(id) || attendanceChanged;
    }
    for (const [id, member] of this.members) {
      if (member.expires <= now) changed = this.members.delete(id) || changed;
    }
    if (changed) {
      if (this.following && !this.members.has(this.following)) this.stopFollowing();
      this.publish(true);
    } else if (attendanceChanged) this.publishRoster();
  }

  update(data: Partial<Omit<CanvasPresenceState, "epoch">>) {
    this.local = { ...this.local, ...data };
    this.schedule();
  }

  connect() {
    const unwatch = watchCanvasRoom(this.canvasId, (event) => {
      if (event.type === "canvas.presence" || event.type === "canvas.attendance") {
        this.receive({ ...event.data, onlineOnly: event.type === "canvas.attendance" });
      } else if (event.type === "subscribed") this.schedule();
    });
    const timer = setInterval(() => {
      this.expire();
      this.schedule();
    }, heartbeatInterval);
    this.schedule();
    return () => {
      unwatch();
      clearInterval(timer);
      this.stopFollowing();
      clearTimeout(this.timer);
      this.timer = undefined;
      this.members.clear();
      this.attendance.clear();
      this.publish(true);
    };
  }

  private schedule() {
    if (this.timer !== undefined) return;
    this.timer = setTimeout(() => {
      this.timer = undefined;
      if (typeof document !== "undefined" && document.visibilityState === "visible") {
        sendCanvasPresence(
          this.canvasId,
          this.following ? { ...this.local, view: undefined } : this.local,
        );
      }
    }, sampleInterval);
  }

  private publish(rosterChanged: boolean) {
    this.snapshot = Array.from(this.members.values(), ({ state }) => state);
    for (const listener of this.listeners) listener();
    if (rosterChanged) this.publishRoster();
  }

  private publishRoster() {
    const accounts = new Map<string, string | undefined>();
    for (const [id, member] of this.attendance) accounts.set(id, member.username);
    for (const { actorId, actorUsername } of this.snapshot) {
      if (actorUsername || !accounts.has(actorId)) accounts.set(actorId, actorUsername);
    }
    this.roster = Array.from(accounts, ([id, username]) => ({ id, username }));
    for (const listener of this.rosterListeners) listener();
  }
}
