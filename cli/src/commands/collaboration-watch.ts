import { once as onceEvent } from "node:events";

import { APIError, parseExactIntegers } from "../client.ts";
import type { Args, Ctx, WireVersion } from "../types.ts";
import { boolFlag, intFlag, stringFlag } from "./args.ts";
import { CanvasPresence } from "./collaboration-presence.ts";
import { CanvasProgress } from "./collaboration-progress.ts";
import { readTaskSummaries, requireTaskSummaries } from "./collaboration-tasks.ts";
import {
  compareVersions,
  encodeWireVersion,
  isWireVersion,
  versionFlag,
} from "./collaboration-version.ts";
import {
  canvasPath,
  CollaborationError,
  collaborationRequest,
  getCollaboration,
  type CollaborationCommit,
  type CollaborationSnapshot,
} from "./collaboration.ts";
import { baseOf, emit } from "./shared.ts";

interface ReplayPage {
  epoch: WireVersion;
  revision: WireVersion;
  cursor: WireVersion;
  title?: string;
  events: CollaborationCommit[];
  snapshotRequired: boolean;
  role: string;
  canGenerate: boolean;
  canCopyToLibrary: boolean;
}

interface CommentReplayPage {
  cursor: string;
  items: Array<Record<string, unknown>>;
  more: boolean;
}

function validPage(page: ReplayPage, epoch: WireVersion, after: WireVersion): boolean {
  const previous = BigInt(after);
  return (
    String(page?.epoch) === String(epoch) &&
    isWireVersion(page.revision) &&
    isWireVersion(page.cursor, true) &&
    Array.isArray(page.events) &&
    BigInt(page.cursor) === previous + BigInt(page.events.length) &&
    compareVersions(page.cursor, page.revision) <= 0 &&
    page.events.every(
      (event, index) =>
        String(event?.epoch) === String(epoch) &&
        String(event.revision) === String(previous + BigInt(index) + 1n),
    )
  );
}

async function replayComments(
  ctx: Ctx,
  canvasId: string,
  epoch: WireVersion,
  cursor: string,
  signal: AbortSignal,
): Promise<CommentReplayPage> {
  const page = await collaborationRequest<CommentReplayPage>(
    ctx,
    `${canvasPath(canvasId)}/collaboration/comments/events`,
    {
      query: { epoch, after: cursor || undefined },
      signal: AbortSignal.any([signal, AbortSignal.timeout(10_000)]),
    },
  );
  if (
    !page?.cursor ||
    !Array.isArray(page.items) ||
    typeof page.more !== "boolean" ||
    (page.cursor === cursor && (page.items.length || page.more))
  ) {
    throw new APIError(-1, "评论回放游标无效", 502);
  }
  return page;
}

function createRoom(ctx: Ctx, snapshot: CollaborationSnapshot, signal: AbortSignal) {
  let wake: (() => void) | undefined;
  let pending = false;
  let socket: WebSocket | undefined;
  let subscribed = false;
  let connecting = false;
  let retryAt = 0;
  let heartbeat: ReturnType<typeof setInterval> | undefined;
  const { epoch, canvas } = snapshot;
  const channel = `canvas-collaboration:${canvas.id}`;
  const presence = new CanvasPresence(epoch);
  const progress = new CanvasProgress(
    canvas.id,
    epoch,
    canvas.revision,
    canvas.payload_json?.nodes ?? [],
  );
  const notify = () => {
    pending = true;
    if (wake) wake();
  };
  return {
    presence,
    progress,
    subscribed: () => subscribed,
    takeNotice() {
      const notified = pending;
      pending = false;
      return notified;
    },
    connect() {
      if (ctx.automation) return;
      if (socket || connecting || signal.aborted || Date.now() < retryAt) return;
      connecting = true;
      retryAt = Date.now() + 5_000;
      void collaborationRequest<{ ticket: string }>(ctx, "/ws/ticket", {
        method: "POST",
        signal: AbortSignal.any([signal, AbortSignal.timeout(5_000)]),
      })
        .then(({ ticket }) => {
          if (signal.aborted || !ticket) return;
          const url = new URL(`${baseOf(ctx)}/ws`);
          url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
          url.searchParams.set("ticket", ticket);
          const connection = new WebSocket(url);
          socket = connection;
          const handshake = setTimeout(() => connection.close(), 5_000);
          connection.addEventListener("open", () => {
            connection.send(JSON.stringify({ type: "subscribe", channel }));
          });
          connection.addEventListener("message", (message) => {
            if (typeof message.data !== "string") return;
            let event: { type?: string; channel?: string; data?: unknown };
            try {
              event = parseExactIntegers(message.data) as typeof event;
            } catch {
              return;
            }
            if (event?.channel !== channel) return;
            if (event.type === "subscribed") {
              clearTimeout(handshake);
              if (!subscribed && snapshot.capabilities?.actions.includes("presence")) {
                const announce = () => {
                  if (connection.readyState !== WebSocket.OPEN || signal.aborted) return;
                  connection.send(
                    JSON.stringify({
                      type: "canvas.attendance",
                      channel,
                      data: { epoch: encodeWireVersion(epoch) },
                    }),
                  );
                };
                announce();
                heartbeat = setInterval(announce, 2_000);
              }
              subscribed = true;
            }
            if (event.type === "unsubscribed" || event.type === "error") {
              presence.clear();
              progress.clearSamples();
              connection.close();
              notify();
            }
            if (
              subscribed &&
              (event.type === "canvas.presence" || event.type === "canvas.attendance")
            ) {
              if (event.data && typeof event.data === "object") {
                presence.receive({ ...event.data, onlineOnly: event.type === "canvas.attendance" });
              }
            }
            if (subscribed && event.type === "canvas.generation_progress")
              progress.receive(event.data);
            if (
              [
                "canvas.collaboration",
                "canvas.comment_changed",
                "subscribed",
                "unsubscribed",
              ].includes(event.type ?? "")
            )
              notify();
          });
          connection.addEventListener("close", () => {
            clearTimeout(handshake);
            clearInterval(heartbeat);
            presence.clear();
            progress.clearSamples();
            socket = undefined;
            subscribed = false;
            retryAt = Date.now() + 2_000 + Math.random() * 1_000;
            notify();
          });
        })
        .catch(() => {})
        .finally(() => {
          connecting = false;
        });
    },
    wait(interval: number) {
      if (signal.aborted || pending) {
        return Promise.resolve();
      }
      return new Promise<void>((resolve) => {
        const finish = () => {
          clearTimeout(timer);
          signal.removeEventListener("abort", finish);
          wake = undefined;
          resolve();
        };
        const timer = setTimeout(finish, interval);
        wake = finish;
        signal.addEventListener("abort", finish, { once: true });
      });
    },
    close() {
      clearInterval(heartbeat);
      presence.clear();
      progress.clearSamples();
      socket?.close();
    },
  };
}

export async function canvasWatch(ctx: Ctx, args: Args): Promise<void> {
  const code = args.positional[0];
  const usage =
    "用法：canvas watch <画布> [--since 版本 --epoch 会话代数] [--comments-since 游标] [--once | --follow 成员] [--jsonl]";
  if (!code || ctx.json) throw new APIError(-1, usage, 2);
  const once = boolFlag(args.flags, "once");
  const tasks = boolFlag(args.flags, "tasks");
  const jsonl = boolFlag(args.flags, "jsonl");
  const follow =
    args.flags.follow === undefined ? undefined : stringFlag(args.flags, "follow", usage).trim();
  if (follow === "" || (follow && once)) throw new APIError(-1, usage, 2);
  if (ctx.automation && (tasks || follow || args.flags["comments-since"] !== undefined))
    throw new APIError(-1, "自动化凭据仅支持图谱增量监听", 403);
  const since = versionFlag(args.flags, "since", true);
  const cursorEpoch = versionFlag(args.flags, "epoch");
  const commentsSince =
    args.flags["comments-since"] === undefined
      ? undefined
      : stringFlag(args.flags, "comments-since", usage);
  if ((since !== undefined || commentsSince !== undefined) !== (cursorEpoch !== undefined))
    throw new APIError(-1, usage, 2);
  const interval = intFlag(args.flags, "interval", 2, { max: 60 }) * 1_000;
  const snapshot = await getCollaboration(ctx, code);
  if (tasks) requireTaskSummaries(snapshot);
  const { epoch, canvas } = snapshot;
  if (cursorEpoch !== undefined && String(cursorEpoch) !== String(epoch)) throw expiredWatch();
  const commentsSupported = snapshot.capabilities?.actions.includes("comments-replay");
  if (commentsSince !== undefined && snapshot.role === "viewer")
    throw new CollaborationError(-1, "当前成员没有评论读取权限", 403, "permission_denied");
  if (commentsSince !== undefined && !commentsSupported)
    throw new CollaborationError(-1, "站点未开放评论回放", 409, "unsupported");
  let after = since ?? canvas.revision;
  let title = canvas.title;
  let commentAfter = commentsSince ?? (commentsSupported && !once ? "" : undefined);
  let permissions = `${snapshot.role}/${snapshot.canGenerate}/${snapshot.canCopyToLibrary}`;
  let failures = 0;
  const controller = new AbortController();
  const stop = () => controller.abort();
  const room = createRoom(ctx, snapshot, controller.signal);
  const output = async (type: string, revision: WireVersion, data: unknown) => {
    ctx.stdout(
      jsonl
        ? JSON.stringify({
            schemaVersion: 1,
            type,
            canvasId: canvas.id,
            epoch: String(epoch),
            revision: String(revision),
            data,
          })
        : `${type}\t${revision}\t${JSON.stringify(data)}`,
    );
    if (process.stdout.writableNeedDrain) {
      await onceEvent(process.stdout, "drain", { signal: controller.signal });
    }
  };
  let lastRoster = "[]";
  let lastFollow = "null";
  let taskPollAfter = 0;
  let lastTasks = "";
  const outputTasks = async (role: string | undefined) => {
    if (!tasks) return;
    if (role === "viewer") {
      if (lastTasks && lastTasks !== "[]") await output("task-summaries", after, { items: [] });
      lastTasks = "";
      return;
    }
    if (performance.now() < taskPollAfter) return;
    taskPollAfter = performance.now() + 5_000;
    try {
      const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(10_000)]);
      const items = await readTaskSummaries(ctx, snapshot, 50, signal);
      const key = JSON.stringify(items);
      if (key !== lastTasks) {
        lastTasks = key;
        await output("task-summaries", after, { items });
      }
    } catch (err) {
      if (err instanceof APIError && err.code === 30018) throw expiredWatch();
      if (!once && err instanceof APIError && (err.retryable || err.status === 404)) return;
      throw err;
    }
  };
  const outputPresence = async () => {
    for (const sample of room.progress.drain()) await output("generation-progress", after, sample);
    const members = room.presence.roster();
    const rosterKey = JSON.stringify(members);
    if (rosterKey !== lastRoster) {
      lastRoster = rosterKey;
      await output("presence", after, { members });
    }
    if (!follow) return;
    const viewpoint = room.presence.follow(follow);
    const followKey = JSON.stringify(viewpoint);
    if (followKey !== lastFollow) {
      lastFollow = followKey;
      await output("follow", after, { target: follow, viewpoint });
    }
  };
  process.once("SIGINT", stop);
  try {
    if (since === undefined) await output("snapshot", after, snapshot);
    while (!controller.signal.aborted) {
      if (!once) room.connect();
      room.takeNotice();
      let page: ReplayPage;
      try {
        page = await collaborationRequest<ReplayPage>(
          ctx,
          `${canvasPath(canvas.id)}/collaboration/events`,
          {
            query: { epoch, after, limit: 50 },
            signal: AbortSignal.any([controller.signal, AbortSignal.timeout(10_000)]),
          },
        );
        failures = 0;
      } catch (err) {
        if (controller.signal.aborted) return;
        if (err instanceof APIError && err.code === 30018) throw expiredWatch();
        if (once || !(err instanceof APIError && err.retryable)) throw err;
        await room.wait(Math.min(30_000, interval * 2 ** Math.min(++failures, 4)));
        continue;
      }
      let role = page?.role;
      if (page?.snapshotRequired || !validPage(page, epoch, after)) {
        const fresh = await getCollaboration(ctx, canvas.id);
        if (fresh.epoch !== epoch) throw expiredWatch();
        room.presence.clear();
        room.progress.reset(fresh.canvas.revision, fresh.canvas.payload_json?.nodes ?? []);
        after = fresh.canvas.revision;
        title = fresh.canvas.title;
        role = fresh.role;
        permissions = `${fresh.role}/${fresh.canGenerate}/${fresh.canCopyToLibrary}`;
        await output("snapshot", after, fresh);
      } else {
        const currentPermissions = `${page.role}/${page.canGenerate}/${page.canCopyToLibrary}`;
        if (currentPermissions !== permissions) {
          room.presence.clear();
          room.progress.clearSamples();
          permissions = currentPermissions;
          await output("permissions", after, {
            role: page.role,
            canGenerate: page.canGenerate,
            canCopyToLibrary: page.canCopyToLibrary,
          });
        }
        for (const event of page.events) {
          room.progress.confirm(event);
          await output("change", event.revision, event);
        }
        after = page.cursor;
        if (compareVersions(after, page.revision) < 0) {
          if (page.events.length === 0) throw new APIError(-1, "协作回放游标没有推进", 502);
          continue;
        }
        if (typeof page.title === "string" && page.title !== title) {
          title = page.title;
          await output("title", after, { title });
        }
      }
      if (commentAfter !== undefined && role === "viewer") {
        if (commentsSince !== undefined && once)
          throw new CollaborationError(-1, "当前成员没有评论读取权限", 403, "permission_denied");
        commentAfter = "";
      } else if (commentAfter !== undefined) {
        const comments = await replayComments(
          ctx,
          canvas.id,
          epoch,
          commentAfter,
          controller.signal,
        ).catch((error: unknown) => {
          if (error instanceof APIError && error.code === 30018) throw expiredWatch();
          if (!once && error instanceof APIError && (error.status === 404 || error.retryable)) {
            if (error.status === 404) commentAfter = "";
            return undefined;
          }
          throw error;
        });
        if (comments) {
          for (const item of comments.items) await output("comment", after, item);
          if (comments.cursor !== commentAfter) {
            commentAfter = comments.cursor;
            await output("comment-cursor", after, { cursor: commentAfter });
          }
          if (comments.more) continue;
        }
      }
      await outputTasks(role);
      if (once) return;
      await waitForReplay(room, interval, follow ? 250 : 1_000, controller.signal, outputPresence);
    }
  } catch (err) {
    if (!controller.signal.aborted) throw err;
  } finally {
    process.removeListener("SIGINT", stop);
    controller.abort();
    room.close();
  }
}

async function waitForReplay(
  room: ReturnType<typeof createRoom>,
  interval: number,
  sample: number,
  signal: AbortSignal,
  outputPresence: () => Promise<void>,
) {
  const deadline = performance.now() + (room.subscribed() ? Math.max(interval, 5_000) : interval);
  for (;;) {
    const remaining = deadline - performance.now();
    if (signal.aborted || remaining <= 0 || room.takeNotice()) return;
    await outputPresence();
    await room.wait(Math.min(sample, remaining));
  }
}

export async function canvasPresence(ctx: Ctx, args: Args): Promise<void> {
  const [code] = args.positional;
  if (!code) throw new APIError(-1, "用法：canvas presence <画布> [--json]", 2);
  const snapshot = await getCollaboration(ctx, code);
  if (!snapshot.capabilities?.actions.includes("presence")) {
    throw new CollaborationError(-1, "站点未启用终端在线名单", 409, "unsupported");
  }
  const controller = new AbortController();
  const room = createRoom(ctx, snapshot, controller.signal);
  const stop = () => controller.abort();
  process.once("SIGINT", stop);
  try {
    room.connect();
    const deadline = performance.now() + 5_000;
    while (!room.subscribed() && !controller.signal.aborted && performance.now() < deadline) {
      room.takeNotice();
      await room.wait(Math.max(0, deadline - performance.now()));
    }
    if (controller.signal.aborted) return;
    if (!room.subscribed()) throw new APIError(-1, "连接协作房间超时", 408);
    await new Promise<void>((resolve) => {
      const finish = () => {
        clearTimeout(timer);
        controller.signal.removeEventListener("abort", finish);
        resolve();
      };
      const timer = setTimeout(finish, 2_300);
      controller.signal.addEventListener("abort", finish, { once: true });
    });
    if (controller.signal.aborted) return;
    const fresh = await getCollaboration(ctx, snapshot.canvas.id);
    if (fresh.epoch !== snapshot.epoch) throw expiredWatch();
    if (!room.subscribed()) throw new APIError(-1, "协作房间连接已断开", 503);
    const members = room.presence.roster();
    emit(
      ctx,
      { members },
      members
        .map(({ username }) => username)
        .filter(Boolean)
        .join("\n") || "当前没有其他在线成员",
    );
  } finally {
    process.removeListener("SIGINT", stop);
    controller.abort();
    room.close();
  }
}

function expiredWatch(): CollaborationError {
  return new CollaborationError(30018, "协作会话已变更，请重新运行 watch", 409, "epoch_expired");
}
