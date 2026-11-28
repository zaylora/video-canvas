import { APIError, request, type RequestOptions } from "../client.ts";
import type { Args, Canvas, Ctx, WireVersion } from "../types.ts";
import { encodeCollaborationVersions, isWireVersion } from "./collaboration-version.ts";
import { baseOf, emit, requireToken } from "./shared.ts";

export interface CollaborationSnapshot {
  automationActor?: string;
  cumulativeBudget?: boolean;
  epoch: WireVersion;
  role: "owner" | "editor" | "viewer";
  canGenerate: boolean;
  generationBudgetLimit?: boolean;
  storyboardBudgetLimit?: boolean;
  canCopyToLibrary: boolean;
  canvas: Canvas;
  capabilities?: CollaborationCapabilities;
}

export interface CollaborationCapabilities {
  protocolVersion: number;
  maxRecordsPerBatch: number;
  maxBatchBytes: number;
  actions: string[];
}

export interface CollaborationChange {
  kind: "node" | "group" | "edge";
  id: string;
  action?: "create" | "delete" | "storyboard-shot" | "director-object";
  value?: Record<string, unknown>;
  fields?: Record<string, unknown>;
}

export interface CollaborationCommit {
  epoch: WireVersion;
  revision: WireVersion;
  opId?: string;
  changes?: CollaborationChange[];
  conflicts?: Array<{ id: string; field?: string; revision: WireVersion; value?: unknown }>;
  skipped?: Array<{ id: string; field?: string; revision: WireVersion }>;
}

export class CollaborationError extends APIError {
  readonly reason: string;
  readonly details?: unknown;

  constructor(code: number, message: string, status: number, reason: string, details?: unknown) {
    super(code, message, status);
    this.reason = reason;
    this.details = details;
  }
}

export function isPositiveVersion(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value > 0;
}

export function canvasPath(code: string): string {
  return `/canvas/${encodeURIComponent(code)}`;
}

export function collaborationRequest<Result>(
  ctx: Ctx,
  path: string,
  options: Pick<RequestOptions, "body" | "headers" | "method" | "query" | "signal"> = {},
): Promise<Result> {
  if (ctx.automation) return scopedAutomationRequest<Result>(ctx, path, options);
  return request<Result>({
    ...options,
    body: encodeCollaborationVersions(options.body),
    base: baseOf(ctx),
    token: requireToken(ctx),
    path,
    fetchImpl: ctx.fetchImpl,
    exactIntegers: true,
  });
}

async function scopedAutomationRequest<Result>(
  ctx: Ctx,
  path: string,
  options: Pick<RequestOptions, "body" | "headers" | "method" | "query" | "signal">,
): Promise<Result> {
  const key = ctx.automation!;
  const prefix = `${canvasPath(key.canvasId)}/collaboration`;
  const suffix = path.startsWith(prefix) ? path.slice(prefix.length) : "invalid";
  const method = options.method ?? "GET";
  const allowed =
    method === "GET"
      ? suffix === "" ||
        suffix === "/events" ||
        /^\/(?:ops|generation-tasks)\/[a-zA-Z0-9_-]{1,64}$/.test(suffix)
      : method === "POST" && (suffix === "/ops" || suffix === "/generation-tasks");
  if (!allowed) throw new APIError(-1, "自动化凭据不允许此操作或画布", 403);
  try {
    return await request<Result>({
      ...options,
      body: encodeCollaborationVersions(options.body),
      base: key.base,
      token: key.token,
      path: `/canvas-automation/${encodeURIComponent(key.canvasId)}/collaboration${suffix}`,
      fetchImpl: ctx.fetchImpl,
      exactIntegers: true,
      redirect: "error",
    });
  } catch (err) {
    if (err instanceof APIError && err.status === 401)
      throw new APIError(err.code, "自动化凭据已过期或撤销，请重新签发", 401);
    throw err;
  }
}

export async function getCollaboration(ctx: Ctx, code: string): Promise<CollaborationSnapshot> {
  const snapshot = await collaborationRequest<CollaborationSnapshot>(
    ctx,
    `${canvasPath(code)}/collaboration`,
  );
  if (
    !snapshot?.canvas ||
    (snapshot.canvas.id !== code &&
      !(/^[0-9a-f]+$/i.test(code) && snapshot.canvas.id === code.toLowerCase())) ||
    !isWireVersion(snapshot.epoch) ||
    !isWireVersion(snapshot.canvas.revision) ||
    !["owner", "editor", "viewer"].includes(snapshot.role)
  )
    throw new APIError(-1, "服务端返回了无效的协作快照", 502);
  return snapshot;
}

export async function tryCollaboration(
  ctx: Ctx,
  code: string,
): Promise<CollaborationSnapshot | null> {
  try {
    return await getCollaboration(ctx, code);
  } catch (err) {
    if (err instanceof APIError && (err.status === 404 || [30018, 30019].includes(err.code)))
      return null;
    throw err;
  }
}

export function snapshotCanvas(snapshot: CollaborationSnapshot): Canvas {
  return {
    ...snapshot.canvas,
    collaboration_active: true,
    collaboration_epoch: snapshot.epoch,
    collaboration_role: snapshot.role,
  };
}

export async function canvasStatus(ctx: Ctx, args: Args): Promise<void> {
  const code = args.positional[0];
  if (!code) throw new APIError(-1, "用法：canvas status <画布>", 2);
  const snapshot = await tryCollaboration(ctx, code);
  const canvas =
    snapshot?.canvas ?? (await readOwnedCanvas(ctx, code)) ?? (await readSharedCanvas(ctx, code));
  if (!snapshot && canvas.collaboration_active && canvas.collaboration_role !== "viewer") {
    throw new APIError(30018, "协作会话已变更，请重试", 409);
  }
  const role = snapshot?.role ?? (canvas.collaboration_role === "viewer" ? "viewer" : "owner");
  const data = {
    canvasId: canvas.id,
    title: canvas.title,
    mode: snapshot ? "collaboration" : role === "viewer" ? "shared" : "personal",
    revision: String(canvas.revision),
    role,
    ...(snapshot && {
      epoch: String(snapshot.epoch),
      canGenerate: snapshot.canGenerate,
      canCopyToLibrary: snapshot.canCopyToLibrary,
      capabilities: snapshot.capabilities,
    }),
  };
  emit(
    ctx,
    data,
    `${canvas.title}（${canvas.id}）版本 ${canvas.revision}，角色 ${role}${snapshot ? `，协作会话 ${snapshot.epoch}` : ""}`,
  );
}

export async function readOwnedCanvas(ctx: Ctx, code: string): Promise<Canvas | null> {
  try {
    return await collaborationRequest<Canvas>(ctx, canvasPath(code));
  } catch (err) {
    if (err instanceof APIError && err.status === 404) return null;
    throw err;
  }
}

export async function readSharedCanvas(ctx: Ctx, code: string): Promise<Canvas> {
  const canvas = await collaborationRequest<Canvas>(ctx, `${canvasPath(code)}/reader`);
  return { ...canvas, collaboration_role: "viewer" };
}
