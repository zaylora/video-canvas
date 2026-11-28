import { createHash, randomUUID } from "node:crypto";
import fs from "node:fs";
import path from "node:path";

import { APIError } from "../client.ts";
import { configDir } from "../config.ts";
import type { Args, Ctx, WireVersion } from "../types.ts";
import { stringFlag } from "./args.ts";
import { isWireVersion, versionFlag } from "./collaboration-version.ts";
import {
  canvasPath,
  CollaborationError,
  collaborationRequest,
  getCollaboration,
  isPositiveVersion,
  type CollaborationCapabilities,
  type CollaborationChange,
  type CollaborationCommit,
  type CollaborationSnapshot,
} from "./collaboration.ts";
import { askYes, baseOf, emit, readJSONFile, requireInteractive } from "./shared.ts";

export interface Batch {
  opId: string;
  epoch: WireVersion;
  baseRevision: WireVersion;
  requireBaseRevision?: boolean;
  changes: CollaborationChange[];
}

interface Plan extends Batch {
  schemaVersion: 1;
  base: string;
  canvasId: string;
  actor: string;
  createdAt: number;
}

interface PlanBatch {
  id: string;
  after: string[];
  opId: string;
  changes: CollaborationChange[];
}

interface MultiPlan extends Omit<Plan, keyof Batch | "schemaVersion"> {
  schemaVersion: 2;
  runId: string;
  epoch: WireVersion;
  baseRevision: WireVersion;
  batches: PlanBatch[];
}

interface BatchOutcome {
  id: string;
  opId: string;
  status: string;
  revision?: string;
  details?: unknown;
  blockedBy?: string[];
}

interface ReceiptStatus {
  epoch: WireVersion;
  revision?: WireVersion;
  opId: string;
  result: "applied" | "conflict" | "missing";
}

const OP_ID = /^[a-zA-Z0-9_-]{1,64}$/;
const MAX_AGE = 24 * 60 * 60 * 1_000;
const EDITABLE = new Set([
  "position",
  "parentId",
  "height",
  "width",
  "data.label",
  "data.labelColor",
  "data.color",
  "data.prompt",
  "data.text",
  "data.model",
  "data.params",
  "data.character",
  "data.scene",
  "data.storyboard",
  "data.manualReferences",
  "data.assetId",
  "data.activeOutputId",
]);

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function validChange(change: unknown): change is CollaborationChange {
  if (
    !isRecord(change) ||
    typeof change.id !== "string" ||
    !change.id ||
    [...change.id].length > 128 ||
    change.id.includes("\0") ||
    !["node", "group", "edge"].includes(String(change.kind))
  )
    return false;
  if (Object.keys(change).some((key) => !["kind", "id", "action", "value", "fields"].includes(key)))
    return false;
  if (change.action === "create") return isRecord(change.value) && change.fields === undefined;
  if (change.action === "delete") return change.value === undefined && change.fields === undefined;
  if (change.action === "storyboard-shot" || change.action === "director-object")
    return change.kind === "node" && isRecord(change.value) && change.fields === undefined;
  if (
    change.action !== undefined ||
    change.kind === "edge" ||
    change.value !== undefined ||
    !isRecord(change.fields)
  )
    return false;
  const fields = Object.keys(change.fields);
  return fields.length > 0 && fields.length <= 10 && fields.every((field) => EDITABLE.has(field));
}

function validateChanges(changes: unknown): CollaborationChange[] {
  if (
    !Array.isArray(changes) ||
    changes.length < 1 ||
    changes.length > 100 ||
    !changes.every(validChange)
  ) {
    throw new APIError(-1, "计划需要 1–100 条合法的节点、组或连线变更", 2);
  }
  if (new Set(changes.map(({ kind, id }) => `${kind}:${id}`)).size !== changes.length) {
    throw new APIError(-1, "同一批次中每条记录只能出现一次", 2);
  }
  return changes;
}

function batchOf({ opId, epoch, baseRevision, requireBaseRevision, changes }: Batch): Batch {
  return { opId, epoch, baseRevision, requireBaseRevision, changes };
}

function checkSize(plan: Batch, capabilities?: CollaborationCapabilities): void {
  if (
    capabilities &&
    (!isPositiveVersion(capabilities.maxRecordsPerBatch) ||
      !isPositiveVersion(capabilities.maxBatchBytes))
  ) {
    throw new APIError(-1, "站点返回了无效的批次限制", 502);
  }
  const maxRecords = Math.min(capabilities?.maxRecordsPerBatch ?? 100, 100);
  const maxBytes = Math.min(capabilities?.maxBatchBytes ?? 256 * 1024, 256 * 1024);
  if (
    plan.changes.length > maxRecords ||
    Buffer.byteLength(JSON.stringify(batchOf(plan))) > maxBytes
  ) {
    throw new APIError(-1, `每批次最多 ${maxRecords} 条变更、${maxBytes} 字节。请拆分独立变更`, 2);
  }
}

export async function getCollaborationActor(ctx: Ctx): Promise<string> {
  const id = ctx.automation
    ? (await getCollaboration(ctx, ctx.automation.canvasId)).automationActor
    : (await collaborationRequest<{ id?: number | string }>(ctx, "/me")).id;
  if (isPositiveVersion(id) || (typeof id === "string" && /^[1-9][0-9]*$/.test(id)))
    return String(id);
  throw new APIError(-1, "无法确认当前登录账号", 502);
}

export function requireEditor(
  snapshot: Pick<CollaborationSnapshot, "role" | "capabilities">,
): void {
  if (snapshot.role === "viewer") {
    throw new CollaborationError(-1, "仅查看成员不能提交画布变更", 403, "permission_denied");
  }
  if (snapshot.capabilities && snapshot.capabilities.protocolVersion !== 1) {
    throw new CollaborationError(-1, "当前 CLI 不支持站点使用的协作协议版本", 409, "unsupported");
  }
}

function validatePlan(value: unknown): Plan {
  if (
    !isRecord(value) ||
    value.schemaVersion !== 1 ||
    typeof value.base !== "string" ||
    typeof value.canvasId !== "string" ||
    typeof value.actor !== "string" ||
    typeof value.opId !== "string" ||
    !OP_ID.test(value.opId) ||
    !isWireVersion(value.epoch) ||
    !isWireVersion(value.baseRevision) ||
    (value.requireBaseRevision !== undefined && value.requireBaseRevision !== true) ||
    !isPositiveVersion(value.createdAt) ||
    value.createdAt > Date.now()
  ) {
    throw new APIError(-1, "无效的协作计划文件", 2);
  }
  const plan: Plan = {
    schemaVersion: 1,
    base: value.base,
    canvasId: value.canvasId,
    actor: value.actor,
    opId: value.opId,
    epoch: value.epoch,
    baseRevision: value.baseRevision,
    requireBaseRevision: value.requireBaseRevision,
    createdAt: value.createdAt,
    changes: validateChanges(value.changes),
  };
  if (Date.now() - plan.createdAt > MAX_AGE) {
    throw new CollaborationError(
      -1,
      "协作计划已超过 24 小时，请先核对原操作结果",
      409,
      "plan_expired",
      { opId: plan.opId },
    );
  }
  checkSize(plan);
  return plan;
}

function readPlanBatches(input: unknown, assignIDs: boolean): PlanBatch[] {
  if (!Array.isArray(input) || input.length < 1 || input.length > 100) {
    throw new APIError(-1, "多批计划需要 1–100 个批次", 2);
  }
  const earlier = new Set<string>();
  const operations = new Set<string>();
  return input.map((value: unknown) => {
    if (
      !isRecord(value) ||
      typeof value.id !== "string" ||
      !OP_ID.test(value.id) ||
      earlier.has(value.id)
    ) {
      throw new APIError(-1, "批次 ID 无效或重复", 2);
    }
    const after: unknown = value.after ?? [];
    if (
      !Array.isArray(after) ||
      !after.every((id: unknown) => typeof id === "string" && earlier.has(id)) ||
      new Set(after).size !== after.length
    ) {
      throw new APIError(-1, `批次 ${value.id} 的依赖必须在它之前定义，且不可重复`, 2);
    }
    const opId = assignIDs ? randomUUID() : value.opId;
    if (typeof opId !== "string" || !OP_ID.test(opId) || operations.has(opId)) {
      throw new APIError(-1, `批次 ${value.id} 的操作编号无效或重复`, 2);
    }
    earlier.add(value.id);
    operations.add(opId);
    return { id: value.id, after, opId, changes: validateChanges(value.changes) };
  });
}

function validateMultiPlan(input: Record<string, unknown>): MultiPlan {
  const { runId } = input;
  const batches = readPlanBatches(input.batches, false);
  if (
    typeof runId !== "string" ||
    !OP_ID.test(runId) ||
    batches.some(({ opId }) => opId === runId)
  ) {
    throw new APIError(-1, "无效的多批计划运行编号", 2);
  }
  const first = batches[0];
  const { base, canvasId, actor, epoch, baseRevision, createdAt } = validatePlan({
    ...input,
    ...first,
    schemaVersion: 1,
  });
  const plan: MultiPlan = {
    schemaVersion: 2,
    runId,
    base,
    canvasId,
    actor,
    epoch,
    baseRevision,
    createdAt,
    batches,
  };
  for (const batch of batches) checkSize(multiBatch(plan, batch));
  return plan;
}

function multiBatch(plan: MultiPlan, batch: PlanBatch): Batch {
  return {
    opId: batch.opId,
    epoch: plan.epoch,
    baseRevision: plan.baseRevision,
    changes: batch.changes,
  };
}

function persistPlan(ctx: Ctx, plan: Plan): void {
  writeCollaborationJournal(
    journalPath(ctx, plan.canvasId, plan.actor, plan.opId),
    plan,
    plan.opId,
  );
}

export function writeCollaborationJournal(target: string, value: unknown, opId: string): void {
  const directory = path.dirname(target);
  fs.mkdirSync(directory, { mode: 0o700, recursive: true });
  const temporary = path.join(directory, `${randomUUID()}.tmp`);
  const content = JSON.stringify(value);
  try {
    fs.writeFileSync(temporary, content, { flag: "wx", mode: 0o600, flush: true });
    try {
      fs.linkSync(temporary, target);
    } catch (err) {
      if ((err as NodeJS.ErrnoException).code !== "EEXIST") throw err;
      if (fs.readFileSync(target, "utf8") !== content) {
        throw new CollaborationError(-1, "操作编号已经绑定另一份计划", 409, "op_id_reused", {
          opId,
        });
      }
    }
  } finally {
    fs.rmSync(temporary, { force: true });
  }
}

function journalPath(ctx: Ctx, code: string, actor: string, opId: string): string {
  const scope = createHash("sha256")
    .update(JSON.stringify([baseOf(ctx), actor, code]))
    .digest("hex");
  return path.join(configDir(ctx.env), "collaboration", scope, `${opId}.json`);
}

export async function submitCollaborationBatch(
  ctx: Ctx,
  code: string,
  batch: Batch,
): Promise<CollaborationCommit> {
  let commit: CollaborationCommit;
  try {
    commit = await collaborationRequest<CollaborationCommit>(
      ctx,
      `${canvasPath(code)}/collaboration/ops`,
      {
        method: "POST",
        body: batch,
        signal: AbortSignal.timeout(15_000),
      },
    );
  } catch (err) {
    if (!(err instanceof APIError)) throw err;
    if (err.code === 30018)
      throw new CollaborationError(err.code, err.message, err.status, "epoch_expired");
    if (err.status === 403 || err.status === 404)
      throw new CollaborationError(err.code, err.message, err.status, "permission_denied");
    if (!err.retryable) throw err;
    throw unknownResult(batch.opId);
  }
  if (
    commit?.epoch !== batch.epoch ||
    commit.opId !== batch.opId ||
    !isWireVersion(commit.revision)
  ) {
    throw unknownResult(batch.opId);
  }
  if (commit.conflicts?.length) {
    const fields = commit.conflicts
      .map(({ id, field }) => (field ? `${id}.${field}` : id))
      .join("、");
    throw new CollaborationError(30002, `协作变更冲突：${fields}`, 409, "conflict", commit);
  }
  return commit;
}

function unknownResult(opId: string): CollaborationError {
  return new CollaborationError(
    -1,
    `尚无法确认操作结果。请用 canvas resume <画布> --run ${opId} 对账`,
    0,
    "result_unknown",
    { opId },
  );
}

export async function newCollaborationPlan(
  ctx: Ctx,
  snapshot: Pick<CollaborationSnapshot, "role" | "epoch" | "canvas" | "capabilities">,
  changes: CollaborationChange[],
  requireBaseRevision = false,
  observedRevision = snapshot.canvas.revision,
): Promise<Plan> {
  requireEditor(snapshot);
  const plan = validatePlan({
    schemaVersion: 1,
    base: baseOf(ctx),
    canvasId: snapshot.canvas.id,
    actor: await getCollaborationActor(ctx),
    epoch: snapshot.epoch,
    baseRevision: observedRevision,
    requireBaseRevision: requireBaseRevision || undefined,
    changes,
    opId: randomUUID(),
    createdAt: Date.now(),
  });
  checkSize(plan, snapshot.capabilities);
  return plan;
}

export async function runCollaborationPlan(ctx: Ctx, plan: Plan): Promise<CollaborationCommit> {
  persistPlan(ctx, plan);
  return submitCollaborationBatch(ctx, plan.canvasId, batchOf(plan));
}

export async function canvasPlan(ctx: Ctx, args: Args): Promise<void> {
  const [code] = args.positional;
  const usage = "用法：canvas plan <画布> --input <变更文件> [-o <计划文件>]";
  if (!code) throw new APIError(-1, usage, 2);
  const input: unknown = readJSONFile(stringFlag(args.flags, "input", usage));
  const plan =
    isRecord(input) && input.batches !== undefined
      ? await createMultiPlan(ctx, code, input)
      : await newCollaborationPlan(
          ctx,
          await getCollaboration(ctx, code),
          validateChanges(isRecord(input) ? input.changes : undefined),
        );
  const output = args.flags.output;
  if (output !== undefined) {
    if (typeof output !== "string" || output === "-") throw new APIError(-1, usage, 2);
    fs.writeFileSync(output, `${JSON.stringify(plan, null, 2)}\n`, { mode: 0o600, flag: "wx" });
  }
  emit(ctx, plan, JSON.stringify(plan, null, 2));
}

async function createMultiPlan(
  ctx: Ctx,
  code: string,
  input: Record<string, unknown>,
): Promise<MultiPlan> {
  if (input.changes !== undefined) throw new APIError(-1, "changes 与 batches 只能指定一个", 2);
  const batches = readPlanBatches(input.batches, true);
  const snapshot = await getCollaboration(ctx, code);
  requireEditor(snapshot);
  const plan = validateMultiPlan({
    base: baseOf(ctx),
    canvasId: snapshot.canvas.id,
    actor: await getCollaborationActor(ctx),
    epoch: snapshot.epoch,
    baseRevision: snapshot.canvas.revision,
    createdAt: Date.now(),
    runId: randomUUID(),
    batches,
  });
  for (const batch of plan.batches) checkSize(multiBatch(plan, batch), snapshot.capabilities);
  return plan;
}

async function approveDelete(ctx: Ctx, plan: Plan | MultiPlan): Promise<void> {
  const changes =
    plan.schemaVersion === 1 ? plan.changes : plan.batches.flatMap((batch) => batch.changes);
  const removed = changes.filter(
    (change) =>
      change.action === "delete" ||
      ((change.action === "storyboard-shot" || change.action === "director-object") &&
        change.value?.action === "rm"),
  );
  if (!removed.length) return;
  requireInteractive(ctx, "删除协作记录", "请在终端确认删除及关联引用的清理。");
  ctx.stderr(`画布 ${plan.canvasId}，基准版本 ${plan.baseRevision}`);
  for (const change of removed) {
    if (change.action === "storyboard-shot") {
      ctx.stderr(
        `从分镜表 ${JSON.stringify(change.id)} 删除镜头 ${JSON.stringify(change.value?.id)}，关联的媒体节点会保留。`,
      );
    } else if (change.action === "director-object") {
      ctx.stderr(
        `从导演台 ${JSON.stringify(change.id)} 删除 ${JSON.stringify(change.value?.collection)} 中的对象 ${JSON.stringify(change.value?.id)}。`,
      );
    } else {
      ctx.stderr(`删除 ${change.kind} ${JSON.stringify(change.id)} 并清理相关引用。`);
    }
  }
  await askYes(ctx, "确认上述删除？输入 yes 确认：", "已取消，画布未修改");
}

async function executeMultiBatch(
  ctx: Ctx,
  plan: MultiPlan,
  batch: PlanBatch,
  resume: boolean,
): Promise<BatchOutcome> {
  const identity = { id: batch.id, opId: batch.opId };
  try {
    if (resume) {
      const receipt = await readReceipt(ctx, plan.canvasId, plan.epoch, batch.opId);
      if (receipt.result !== "missing") {
        return { ...identity, status: receipt.result, revision: receipt.revision?.toString() };
      }
    }
    const result = await submitCollaborationBatch(ctx, plan.canvasId, multiBatch(plan, batch));
    return { ...identity, status: "applied", revision: String(result.revision) };
  } catch (err) {
    if (!(err instanceof APIError)) throw err;
    if (err instanceof CollaborationError) {
      return { ...identity, status: err.reason, details: err.details };
    }
    const status = err.retryable ? "result_unknown" : collaborationBatchError(err);
    return { ...identity, status, details: { code: err.code, message: err.message } };
  }
}

function collaborationBatchError(err: APIError): string {
  if (err.code === 30018) return "epoch_expired";
  if ([401, 403, 404].includes(err.status)) return "permission_denied";
  if (err.status === 429) return "rate_limited";
  return "rejected";
}

async function runMultiPlan(ctx: Ctx, plan: MultiPlan, resume: boolean): Promise<void> {
  if (!resume) {
    await approveDelete(ctx, plan);
    writeCollaborationJournal(
      journalPath(ctx, plan.canvasId, plan.actor, plan.runId),
      plan,
      plan.runId,
    );
  }
  const confirmed = new Set<string>();
  const batches: BatchOutcome[] = [];
  let stoppedBy: string | undefined;
  for (const batch of plan.batches) {
    const blockedBy = stoppedBy ? [stoppedBy] : batch.after.filter((id) => !confirmed.has(id));
    if (blockedBy.length) {
      batches.push({ id: batch.id, opId: batch.opId, status: "blocked", blockedBy });
      continue;
    }
    const outcome = await executeMultiBatch(ctx, plan, batch, resume);
    batches.push(outcome);
    if (outcome.status === "applied") confirmed.add(batch.id);
    if (["epoch_expired", "permission_denied", "rate_limited"].includes(outcome.status)) {
      stoppedBy = batch.id;
    }
  }
  const resolved = `${confirmed.size}/${plan.batches.length} 个批次已确认，运行编号 ${plan.runId}`;
  const result = { runId: plan.runId, epoch: String(plan.epoch), batches };
  if (confirmed.size !== plan.batches.length) {
    if (!ctx.json) for (const batch of batches) ctx.stderr(`${batch.id}: ${batch.status}`);
    throw new CollaborationError(-1, resolved, 409, "partial", result);
  }
  emit(ctx, result, resolved);
}

async function readReceipt(
  ctx: Ctx,
  canvasId: string,
  epoch: WireVersion,
  opId: string,
): Promise<ReceiptStatus> {
  const receipt = await collaborationRequest<ReceiptStatus>(
    ctx,
    `${canvasPath(canvasId)}/collaboration/ops/${opId}`,
    {
      query: { epoch },
      signal: AbortSignal.timeout(10_000),
    },
  );
  if (
    receipt?.opId !== opId ||
    receipt.epoch !== epoch ||
    !(
      receipt.result === "missing" ||
      (["applied", "conflict"].includes(receipt.result) && isWireVersion(receipt.revision))
    )
  ) {
    throw unknownResult(opId);
  }
  return receipt;
}

function receiptData(receipt: ReceiptStatus) {
  return { ...receipt, epoch: String(receipt.epoch), revision: receipt.revision?.toString() };
}

export async function canvasOpStatus(ctx: Ctx, args: Args): Promise<void> {
  const [code, opId] = args.positional;
  if (!code || !opId || !OP_ID.test(opId)) {
    throw new APIError(-1, "用法：canvas op status <画布> <操作编号> [--epoch 会话代数]", 2);
  }
  const epoch = versionFlag(args.flags, "epoch");
  const snapshot = await getCollaboration(ctx, code);
  requireEditor(snapshot);
  const receipt = await readReceipt(ctx, snapshot.canvas.id, epoch ?? snapshot.epoch, opId);
  emit(
    ctx,
    receiptData(receipt),
    receipt.result === "missing"
      ? `操作 ${opId} 尚无服务端收据`
      : `操作 ${opId} ${receipt.result === "conflict" ? "存在冲突" : "已确认"}，版本 ${receipt.revision}`,
  );
}

export async function canvasApply(ctx: Ctx, args: Args, resume = false): Promise<void> {
  const [code] = args.positional;
  const usage = resume
    ? "用法：canvas resume <画布> --run <操作编号>"
    : "用法：canvas apply <画布> --plan <文件>";
  if (!code) throw new APIError(-1, usage, 2);
  const selected = stringFlag(args.flags, resume ? "run" : "plan", usage);
  if (resume && !OP_ID.test(selected)) throw new APIError(-1, "无效的协作操作编号", 2);
  const snapshot = await getCollaboration(ctx, code);
  requireEditor(snapshot);
  const actor = await getCollaborationActor(ctx);
  const filename = resume ? journalPath(ctx, snapshot.canvas.id, actor, selected) : selected;
  if (resume && !fs.existsSync(filename)) {
    throw new CollaborationError(
      -1,
      "当前站点、账号和画布下没有这笔操作",
      404,
      "operation_not_found",
    );
  }
  const input: unknown = readJSONFile(filename);
  const plan =
    isRecord(input) && input.schemaVersion === 2 ? validateMultiPlan(input) : validatePlan(input);
  if (plan.base !== baseOf(ctx) || plan.canvasId !== snapshot.canvas.id || plan.actor !== actor) {
    throw new CollaborationError(
      -1,
      "协作计划与当前站点、账号或画布不匹配",
      403,
      "permission_denied",
    );
  }
  if (plan.epoch !== snapshot.epoch) {
    throw new CollaborationError(
      30018,
      "计划所属协作会话已失效，请重新读取画布",
      409,
      "epoch_expired",
    );
  }
  if (plan.schemaVersion === 2) {
    for (const batch of plan.batches) checkSize(multiBatch(plan, batch), snapshot.capabilities);
    return runMultiPlan(ctx, plan, resume);
  }
  checkSize(plan, snapshot.capabilities);
  if (resume) {
    const receipt = await readReceipt(ctx, plan.canvasId, plan.epoch, plan.opId);
    if (receipt.result === "conflict") {
      throw new CollaborationError(
        30002,
        "原操作与已确认的画布变更冲突",
        409,
        "conflict",
        receiptData(receipt),
      );
    }
    if (receipt.result === "applied") {
      emit(ctx, receiptData(receipt), `已确认操作 ${receipt.opId}，版本 ${receipt.revision}`);
      return;
    }
  }
  await approveDelete(ctx, plan);
  const commit = await runCollaborationPlan(ctx, plan);
  emit(ctx, commit, `已确认操作 ${plan.opId}，版本 ${commit.revision}`);
}
