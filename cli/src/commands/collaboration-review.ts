import { randomUUID } from "node:crypto";

import { APIError } from "../client.ts";
import type { Args, Ctx } from "../types.ts";
import { listFlag, stringFlag } from "./args.ts";
import { isWireVersion, versionFlag } from "./collaboration-version.ts";
import { requireEditor } from "./collaboration-plan.ts";
import {
  canvasPath,
  CollaborationError,
  collaborationRequest,
  getCollaboration,
  type CollaborationCommit,
} from "./collaboration.ts";
import { askYes, emit, requireInteractive } from "./shared.ts";

function operationId(args: Args, usage: string, max = 64): string {
  const id =
    args.flags["op-id"] === undefined ? randomUUID() : stringFlag(args.flags, "op-id", usage);
  if (id.length > max || !/^[a-zA-Z0-9_-]+$/.test(id)) throw new APIError(-1, "无效的 --op-id", 2);
  return id;
}

async function reviewSession(ctx: Ctx, code: string, args: Args) {
  const expected = versionFlag(args.flags, "epoch");
  const snapshot = await getCollaboration(ctx, code);
  if (expected !== undefined && snapshot.epoch !== expected) {
    throw new CollaborationError(30018, "协作会话已变更", 409, "epoch_expired");
  }
  return {
    snapshot,
    epoch: snapshot.epoch,
    path: `${canvasPath(snapshot.canvas.id)}/collaboration`,
  };
}

function emitReview(ctx: Ctx, result: unknown): void {
  emit(ctx, result, JSON.stringify(result, null, 2));
}

export async function canvasComment(ctx: Ctx, args: Args): Promise<void> {
  const [action, code] = args.positional;
  const usage =
    "用法：canvas comment list|add <画布> --node <节点> [--text 正文] [--mention 成员ID]";
  if (!code || !["list", "add"].includes(action ?? "")) throw new APIError(-1, usage, 2);
  const nodeId = stringFlag(args.flags, "node", usage);
  const { snapshot, epoch, path } = await reviewSession(ctx, code, args);
  if (action === "list") {
    const before =
      args.flags.before === undefined ? undefined : stringFlag(args.flags, "before", usage);
    return emitReview(
      ctx,
      await collaborationRequest(ctx, `${path}/comments`, { query: { epoch, nodeId, before } }),
    );
  }
  requireEditor(snapshot);
  const body = stringFlag(args.flags, "text", usage).trim();
  const mentions = [...new Set(listFlag(args.flags, "mention"))];
  if (!body || [...body].length > 2_000 || mentions.length > 10) {
    throw new APIError(-1, "评论需要 1–2000 个字，最多提及 10 名成员", 2);
  }
  const opId = operationId(args, usage);
  ctx.stderr(
    `评论编号 ${opId}，会话 ${epoch}。重试时保留正文，并使用 --op-id ${opId} --epoch ${epoch}。`,
  );
  emitReview(
    ctx,
    await collaborationRequest(ctx, `${path}/comments`, {
      method: "POST",
      body: { epoch, nodeId, body, opId, mentions },
    }),
  );
}

export async function canvasMention(ctx: Ctx, args: Args): Promise<void> {
  const [action, code] = args.positional;
  const usage = "用法：canvas mention list <画布> [--after 提醒ID]";
  if (action !== "list" || !code) throw new APIError(-1, usage, 2);
  const after = args.flags.after === undefined ? undefined : stringFlag(args.flags, "after", usage);
  const { epoch, path } = await reviewSession(ctx, code, args);
  emitReview(ctx, await collaborationRequest(ctx, `${path}/mentions`, { query: { epoch, after } }));
}

export async function canvasHistory(ctx: Ctx, args: Args): Promise<void> {
  const [action, code, target] = args.positional;
  const usage =
    "用法：canvas history list <画布> [--before 版本] 或 canvas history show|copy <画布> <版本>";
  if (!code || !["list", "show", "copy"].includes(action ?? "")) throw new APIError(-1, usage, 2);
  if (action === "copy") requireInteractive(ctx, "另存历史版本", "请在终端确认来源和版本。");
  const { snapshot, epoch, path } = await reviewSession(ctx, code, args);
  if (action === "list") {
    const before = versionFlag(args.flags, "before");
    return emitReview(
      ctx,
      await collaborationRequest(ctx, `${path}/history`, { query: { epoch, before } }),
    );
  }
  if (!target) throw new APIError(-1, usage, 2);
  if (!isWireVersion(target)) throw new APIError(-1, "历史版本编号无效", 2);
  const revision = target;
  if (snapshot.role !== "owner")
    throw new CollaborationError(-1, "只有所有者可以查看或复制历史图谱", 403, "permission_denied");
  if (action === "show") {
    return emitReview(
      ctx,
      await collaborationRequest(ctx, `${path}/history/${revision}`, { query: { epoch } }),
    );
  }
  const opId = operationId(args, usage, 55);
  ctx.stderr(
    `将把 ${snapshot.canvas.title}（${snapshot.canvas.id}）的版本 ${revision} 另存到你的个人画布。`,
  );
  await askYes(ctx, "输入 yes 确认另存：", "已取消另存");
  ctx.stderr(`另存编号 ${opId}，会话 ${epoch}。重试时使用 --op-id ${opId} --epoch ${epoch}。`);
  emitReview(
    ctx,
    await collaborationRequest(ctx, `${path}/history/${revision}/copy`, {
      method: "POST",
      body: { epoch, opId },
    }),
  );
}

export async function canvasUndo(ctx: Ctx, args: Args, redo: boolean): Promise<void> {
  const [code] = args.positional;
  const action = redo ? "重做" : "撤销";
  const usage = `用法：canvas ${redo ? "redo" : "undo"} <画布> --target <目标版本>`;
  if (!code || args.flags.target === undefined) throw new APIError(-1, usage, 2);
  const expectedRevision = versionFlag(args.flags, "target");
  const opId = operationId(args, usage);
  requireInteractive(ctx, action, "请在终端确认目标版本。");
  const { snapshot, epoch, path } = await reviewSession(ctx, code, args);
  requireEditor(snapshot);
  ctx.stderr(
    `画布 ${snapshot.canvas.id} 将${action}你的目标版本 ${expectedRevision}。该操作可能变更节点和连线。`,
  );
  await askYes(ctx, `输入 yes 确认${action}：`, `已取消${action}`);
  ctx.stderr(
    `操作编号 ${opId}。结果不明时可用 canvas op status ${snapshot.canvas.id} ${opId} --epoch ${epoch} 查询。`,
  );
  const result = await collaborationRequest<CollaborationCommit>(ctx, `${path}/undo`, {
    method: "POST",
    body: { epoch, opId, expectedRevision, redo },
    signal: AbortSignal.timeout(15_000),
  });
  if (result?.epoch !== epoch || result.opId !== opId || !isWireVersion(result.revision)) {
    throw new CollaborationError(-1, "无法确认操作结果，请查询操作收据", 502, "result_unknown", {
      opId,
    });
  }
  if (result.conflicts?.length) {
    const latest = result.conflicts[0]?.revision || "无";
    throw new CollaborationError(
      30002,
      `目标已变更，最新可操作版本 ${latest}`,
      409,
      "conflict",
      result,
    );
  }
  if (result.skipped?.length) {
    throw new CollaborationError(
      -1,
      `已确认${action}，有 ${result.skipped.length} 处因队友更新而跳过`,
      409,
      "partial",
      result,
    );
  }
  emit(ctx, result, `已确认${action} ${opId}，版本 ${result.revision}`);
}
