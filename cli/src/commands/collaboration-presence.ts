import type { WireVersion } from "../types.ts";
import { isWireVersion } from "./collaboration-version.ts";

const expiry = 6_000;
const maxMembers = 256;

type View = { x: number; y: number; zoom: number };
type Account = { id: string; username?: string };
type ViewState = { selection: string[]; view?: View; editing?: string; expires: number };

function record(value: unknown): Record<string, unknown> | undefined {
  return value && typeof value === "object" ? (value as Record<string, unknown>) : undefined;
}

function validId(value: unknown): value is string {
  return typeof value === "string" && value.length > 0 && value.length <= 256;
}

function validCoordinate(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value) && Math.abs(value) <= 1e9;
}

function parseView(value: unknown): View | undefined {
  const view = record(value);
  if (!view) return undefined;
  const { x, y, zoom } = view;
  if (
    validCoordinate(x) &&
    validCoordinate(y) &&
    typeof zoom === "number" &&
    zoom > 0 &&
    zoom <= 4
  ) {
    return { x, y, zoom };
  }
}

export class CanvasPresence {
  private readonly epoch: WireVersion;
  private readonly members = new Map<
    string,
    { expires: number; username?: string; scene?: ViewState }
  >();

  constructor(epoch: WireVersion) {
    this.epoch = epoch;
  }

  receive(data: unknown, now = performance.now()) {
    const event = record(data);
    if (
      !event ||
      !isWireVersion(event.epoch) ||
      String(event.epoch) !== String(this.epoch) ||
      !validId(event.actorId)
    )
      return;
    const { actorId, actorUsername } = event;
    const previous = this.members.get(actorId);
    if (!previous && this.members.size >= maxMembers) this.expire(now);
    if (!previous && this.members.size >= maxMembers) return;
    const expires = now + expiry;
    this.members.set(actorId, {
      expires,
      username: validId(actorUsername) ? actorUsername : previous?.username,
      scene:
        event.onlineOnly === true
          ? previous?.scene
          : {
              selection: Array.isArray(event.selection)
                ? event.selection.slice(0, 16).filter(validId)
                : [],
              view: parseView(event.view),
              editing: validId(event.editing) ? event.editing : undefined,
              expires,
            },
    });
  }

  roster(now = performance.now()): Account[] {
    this.expire(now);
    return Array.from(this.members, ([id, member]) => ({ id, username: member.username })).sort(
      (left, right) => left.id.localeCompare(right.id),
    );
  }

  follow(target: string, now = performance.now()) {
    const members = this.roster(now);
    const account = members.find(({ id }) => id === target) ?? uniqueUsername(members, target);
    if (!account) return null;
    const scene = this.members.get(account.id)?.scene;
    if (!scene?.view || scene.expires <= now) return null;
    return {
      actorId: account.id,
      actorUsername: account.username,
      view: scene.view,
      selection: scene.selection,
      ...(scene.editing ? { editing: scene.editing } : {}),
    };
  }

  clear() {
    this.members.clear();
  }

  private expire(now: number) {
    for (const [id, member] of this.members) {
      if (member.expires <= now) this.members.delete(id);
    }
  }
}

function uniqueUsername(members: Account[], target: string): Account | undefined {
  const matches = members.filter(({ username }) => username === target);
  return matches.length === 1 ? matches[0] : undefined;
}
