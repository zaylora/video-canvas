import { APIError } from "../client.ts";
import type { Args, Ctx } from "../types.ts";
import { intFlag } from "./args.ts";
import {
  canvasPath,
  CollaborationError,
  collaborationRequest,
  getCollaboration,
  type CollaborationSnapshot,
} from "./collaboration.ts";
import { emit } from "./shared.ts";

interface TaskSummary {
  id: string;
  nodeId: string;
  kind: "storyboard-split" | "character-voice";
  status: string;
  username?: string;
  updatedAt: string;
}

export function requireTaskSummaries(snapshot: CollaborationSnapshot) {
  if (snapshot.role === "viewer")
    throw new CollaborationError(-1, "当前成员没有专项任务摘要权限", 403, "forbidden");
  if (!snapshot.capabilities?.actions?.includes("task-summaries"))
    throw new CollaborationError(-1, "站点未开放专项任务摘要", 406, "unsupported");
}

export async function readTaskSummaries(
  ctx: Ctx,
  snapshot: CollaborationSnapshot,
  limit = 50,
  signal?: AbortSignal,
) {
  const page = await collaborationRequest<{ items: TaskSummary[] }>(
    ctx,
    `${canvasPath(snapshot.canvas.id)}/collaboration/tasks`,
    {
      query: { epoch: snapshot.epoch, limit },
      signal,
    },
  );
  if (
    !Array.isArray(page?.items) ||
    page.items.length > limit ||
    page.items.some((item) => !validTaskSummary(item))
  )
    throw new APIError(-1, "站点返回了无效的专项任务摘要", 502);
  return page.items.map(({ id, nodeId, kind, status, username, updatedAt }) => ({
    id,
    nodeId,
    kind,
    status,
    username,
    updatedAt,
  }));
}

function validTaskSummary(item: TaskSummary) {
  return (
    item &&
    typeof item.id === "string" &&
    typeof item.nodeId === "string" &&
    (item.kind === "storyboard-split" || item.kind === "character-voice") &&
    typeof item.status === "string" &&
    typeof item.updatedAt === "string" &&
    (item.username === undefined || typeof item.username === "string")
  );
}

export async function canvasTaskList(ctx: Ctx, args: Args) {
  const [action, code] = args.positional;
  if (action !== "list" || !code || args.positional.length !== 2)
    throw new APIError(-1, "用法：canvas task list <画布> [--limit N]", 2);
  const limit = intFlag(args.flags, "limit", 50, { max: 100 });
  const snapshot = await getCollaboration(ctx, code);
  requireTaskSummaries(snapshot);
  const items = await readTaskSummaries(ctx, snapshot, limit, AbortSignal.timeout(10_000));
  const text = items.map(({ nodeId, kind, status, username }) =>
    [nodeId, kind, status, username].filter(Boolean).join("\t"),
  );
  emit(ctx, { items }, text.join("\n") || "当前没有近期专项任务");
}
