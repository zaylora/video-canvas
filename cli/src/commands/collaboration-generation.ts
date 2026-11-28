import { createHash, randomUUID } from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { isDeepStrictEqual } from "node:util";

import { APIError, request as apiRequest } from "../client.ts";
import { configDir } from "../config.ts";
import type { Ctx, Flags, GenerateResponse, WireVersion } from "../types.ts";
import { intFlag, stringFlag } from "./args.ts";
import { encodeCollaborationVersions, isWireVersion } from "./collaboration-version.ts";
import {
  getCollaborationActor,
  requireEditor,
  submitCollaborationBatch,
  writeCollaborationJournal,
  type Batch,
} from "./collaboration-plan.ts";
import {
  canvasPath,
  collaborationRequest,
  CollaborationError,
  isPositiveVersion,
  tryCollaboration,
} from "./collaboration.ts";
import { baseOf, requireToken } from "./shared.ts";

interface GenerationIntent {
  schemaVersion: 1;
  actor: string;
  canvasId: string;
  createdAt: number;
  maxCredits: number;
  budget?: GenerationBudget;
  request: Record<string, unknown>;
  confirmation: Batch;
}

interface PreparedGeneration {
  canvas_id: string;
  collaboration: {
    epoch: WireVersion;
    revision: WireVersion;
    max_credits: number;
    budget?: GenerationBudget;
  };
}

export interface GenerationBudget {
  key: string;
  limit: number;
}

export interface GenerationPreview {
  epoch?: WireVersion;
  revision?: WireVersion;
  resume?: boolean;
  budget?: GenerationBudget;
}

export function generationBudget(flags: Flags): GenerationBudget | undefined {
  const limit = intFlag(flags, "budget-credits", undefined, { min: 0 });
  if (limit === undefined && flags["budget-key"] === undefined) return undefined;
  const key = stringFlag(flags, "budget-key", "--budget-key ID --budget-credits N");
  if (!/^[a-zA-Z0-9_-]{1,64}$/.test(key) || limit === undefined)
    throw new APIError(-1, "累计积分上限需要 --budget-key ID 和 --budget-credits N", 2);
  return { key, limit };
}

const INTENT_LIFETIME = 24 * 60 * 60 * 1_000;

function journalFile(ctx: Ctx, key: string): string {
  const digest = createHash("sha256")
    .update(JSON.stringify([baseOf(ctx), key]))
    .digest("hex");
  return path.join(configDir(ctx.env), "collaboration-generation", digest);
}

export function readGenerationJournal<Value>(file: string): Value | undefined {
  try {
    return JSON.parse(fs.readFileSync(file, "utf8")) as Value;
  } catch (err) {
    if ((err as NodeJS.ErrnoException).code === "ENOENT") return undefined;
    throw new APIError(-1, "无法读取本地生成记录，请保留原幂等键核对任务", 409);
  }
}

function validateIntent(
  intent: GenerationIntent,
  actor: string,
  request: Record<string, unknown>,
  limit: number,
  budget?: GenerationBudget,
): void {
  if (intent.actor !== actor) {
    throw new CollaborationError(-1, "原生成意图属于另一个账号", 403, "permission_denied");
  }
  if (
    intent.schemaVersion !== 1 ||
    intent.maxCredits !== limit ||
    !isDeepStrictEqual(intent.budget, budget) ||
    !isDeepStrictEqual(intent.request, request)
  ) {
    throw new CollaborationError(-1, "幂等键已绑定其他生成参数或积分上限", 409, "op_id_reused");
  }
  if (!isPositiveVersion(intent.createdAt) || Date.now() - intent.createdAt > INTENT_LIFETIME) {
    throw new CollaborationError(-1, "生成意图已超过 24 小时，请核对原任务", 409, "plan_expired");
  }
}

export function unknownGeneration(key: string): CollaborationError {
  return new CollaborationError(
    -1,
    `尚无法确认任务是否已受理。核对后用原参数和 --idempotency-key ${JSON.stringify(key)} 重试。`,
    0,
    "result_unknown",
    { idempotencyKey: key },
  );
}

export async function submitGenerationRequest(
  ctx: Ctx,
  key: string,
  body: Record<string, unknown>,
  limit: number | undefined,
  preview?: GenerationPreview,
): Promise<GenerateResponse> {
  const token = ctx.automation ? undefined : requireToken(ctx);
  if (ctx.automation && (preview?.budget || body.canvas_id !== ctx.automation.canvasId)) {
    throw new APIError(-1, "自动化任务须使用凭据绑定的画布和服务端预算", 403);
  }
  const prepared = await prepareCollaborationGeneration(
    ctx,
    key,
    body,
    limit,
    "generationBudgetLimit",
    preview,
  );
  const request = {
    method: "POST" as const,
    headers: { "Idempotency-Key": key },
    signal: prepared ? AbortSignal.timeout(30_000) : undefined,
  };
  const result = await (
    ctx.automation
      ? collaborationRequest<GenerateResponse>(
          ctx,
          `${canvasPath(ctx.automation.canvasId)}/collaboration/generation-tasks`,
          {
            ...request,
            body: { ...body, ...prepared },
          },
        )
      : apiRequest<GenerateResponse>({
          ...request,
          base: baseOf(ctx),
          token,
          path: "/generation-tasks",
          body: prepared ? encodeCollaborationVersions({ ...body, ...prepared }) : body,
          fetchImpl: ctx.fetchImpl,
        })
  ).catch((err: unknown) => {
    if (prepared && err instanceof APIError && err.retryable) throw unknownGeneration(key);
    throw err;
  });
  if (prepared && !Array.isArray(result?.items)) throw unknownGeneration(key);
  return result;
}

export async function prepareCollaborationGeneration(
  ctx: Ctx,
  key: string,
  request: Record<string, unknown>,
  limit: number | undefined,
  capability: "generationBudgetLimit" | "storyboardBudgetLimit" = "generationBudgetLimit",
  preview?: GenerationPreview,
): Promise<PreparedGeneration | undefined> {
  const journal = journalFile(ctx, key);
  const saved = readGenerationJournal<GenerationIntent>(`${journal}.intent`);
  const budget = preview?.budget;
  const code = typeof request.canvas_id === "string" ? request.canvas_id : undefined;
  if (!code && !saved && limit === undefined && !budget) return;
  if (!code) throw new APIError(-1, "协作生成需要指定 --canvas 和 --node", 2);
  const snapshot = await tryCollaboration(ctx, code);
  if (!snapshot && !saved && limit === undefined && !budget) return;
  if (!snapshot || !snapshot[capability]) {
    throw new CollaborationError(-1, "这个画布会话无法确认协作生成积分上限", 409, "unsupported");
  }
  requireEditor(snapshot);
  if (!snapshot.canGenerate) {
    throw new CollaborationError(-1, "当前成员没有这个画布的生成授权", 403, "permission_denied");
  }
  if (budget && !snapshot.cumulativeBudget)
    throw new CollaborationError(-1, "站点未开放累计积分上限", 409, "unsupported");
  const taskNode = String(request.node_id);
  const voice = request.kind === "audio" && taskNode.startsWith("voice:");
  const confirmationNode = voice ? taskNode.slice("voice:".length) : taskNode;
  const requiredKind =
    capability === "storyboardBudgetLimit" ? "storyboard" : voice ? "character" : undefined;
  if (preview?.epoch !== undefined && snapshot.epoch !== preview.epoch) {
    throw new CollaborationError(30018, "会话已变化，请重新读取画布", 409, "epoch_expired");
  }
  if (
    preview?.revision !== undefined &&
    !(saved && preview.resume) &&
    snapshot.canvas.revision !== preview.revision
  ) {
    throw new CollaborationError(30002, "确认期间画布有变化，请重新预览", 409, "conflict");
  }
  if (
    requiredKind &&
    snapshot.canvas.payload_json?.nodes?.find((node) => node.id === confirmationNode)?.data
      ?.kind !== requiredKind
  ) {
    throw new APIError(
      -1,
      requiredKind === "storyboard" ? "目标不是分镜表节点" : "目标不是角色卡节点",
      2,
    );
  }
  if (limit === undefined) throw new APIError(-1, "协作生成须指定 --max-credits 总积分上限", 2);
  const actor = await getCollaborationActor(ctx);
  const canonical = { ...request, canvas_id: snapshot.canvas.id };
  if (saved) validateIntent(saved, actor, canonical, limit, budget);
  const intent: GenerationIntent = saved ?? {
    schemaVersion: 1,
    actor,
    canvasId: snapshot.canvas.id,
    createdAt: Date.now(),
    maxCredits: limit,
    budget,
    request: canonical,
    confirmation: {
      opId: randomUUID(),
      epoch: snapshot.epoch,
      baseRevision: snapshot.canvas.revision,
      requireBaseRevision: preview?.revision === undefined ? undefined : true,
      changes: [
        {
          kind: "node",
          id: confirmationNode,
          fields: {
            "data.generationInput": { model: request.model_id, input: request.input },
          },
        },
      ],
    },
  };
  if (intent.canvasId !== snapshot.canvas.id || intent.confirmation.epoch !== snapshot.epoch) {
    throw new CollaborationError(30018, "生成意图所属协作会话已变更", 409, "epoch_expired");
  }
  if (Buffer.byteLength(JSON.stringify(intent.confirmation)) > 256 * 1024) {
    throw new APIError(-1, "生成参数超过协作提交限制", 2);
  }
  if (!saved) writeCollaborationJournal(`${journal}.intent`, intent, key);
  const cumulative = budget ? `，累计预算 ${budget.key} 上限 ${budget.limit}` : "";
  ctx.stderr(`协作生成积分上限 ${limit}${cumulative}，幂等键 ${JSON.stringify(key)}。`);
  const revision = await confirmedRevision(ctx, journal, intent, key);
  return {
    canvas_id: intent.canvasId,
    collaboration: { epoch: intent.confirmation.epoch, revision, max_credits: limit, budget },
  };
}

async function confirmedRevision(
  ctx: Ctx,
  journal: string,
  intent: GenerationIntent,
  key: string,
): Promise<WireVersion> {
  const file = `${journal}.binding`;
  const resolved = readGenerationJournal<Pick<Batch, "opId" | "epoch"> & { revision: WireVersion }>(
    file,
  );
  const { epoch, opId } = intent.confirmation;
  if (resolved) {
    if (resolved.epoch !== epoch || resolved.opId !== opId || !isWireVersion(resolved.revision)) {
      throw unknownGeneration(key);
    }
    return resolved.revision;
  }
  let revision: WireVersion;
  try {
    ({ revision } = await submitCollaborationBatch(ctx, intent.canvasId, intent.confirmation));
  } catch (err) {
    if (err instanceof CollaborationError && err.reason === "result_unknown")
      throw unknownGeneration(key);
    throw err;
  }
  writeCollaborationJournal(file, { epoch, opId, revision }, key);
  return revision;
}
