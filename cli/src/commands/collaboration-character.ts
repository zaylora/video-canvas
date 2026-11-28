import { createHash, randomUUID } from "node:crypto";
import path from "node:path";
import { isDeepStrictEqual } from "node:util";

import { APIError } from "../client.ts";
import { configDir } from "../config.ts";
import type { Args, Ctx, Task, WireVersion } from "../types.ts";
import { boolFlag, intFlag, stringFlag } from "./args.ts";
import {
  generationBudget,
  readGenerationJournal,
  submitGenerationRequest,
  unknownGeneration,
  type GenerationPreview,
  type GenerationBudget,
} from "./collaboration-generation.ts";
import {
  getCollaborationActor,
  requireEditor,
  writeCollaborationJournal,
} from "./collaboration-plan.ts";
import {
  CollaborationError,
  getCollaboration,
  isPositiveVersion,
  type CollaborationSnapshot,
} from "./collaboration.ts";
import { baseOf, emit, readJSONFile } from "./shared.ts";
import { waitTaskTerminal } from "./task.ts";

interface CharacterStage {
  model: string;
  input: Record<string, unknown>;
  budget: number;
}

interface CharacterRun {
  canvas: string;
  epoch: WireVersion;
  actor: string;
  node: string;
  image: CharacterStage;
  voice: CharacterStage;
  fromImage: boolean;
  cumulative?: GenerationBudget;
}

interface VoiceBinding {
  imageId: number;
  revision: WireVersion;
  input: Record<string, unknown>;
}

const usage =
  "用法：canvas character generate <画布> <角色节点> --kind image-and-voice --image-model <模型> --voice-model <模型> --image-input <JSON> --voice-input <JSON> --max-credits N --image-max-credits N";

function stage(args: Args, kind: "image" | "voice", budget: number): CharacterStage {
  const model = stringFlag(args.flags, `${kind}-model`, usage);
  const input: unknown = readJSONFile(stringFlag(args.flags, `${kind}-input`, usage));
  if (
    !model.trim() ||
    !input ||
    typeof input !== "object" ||
    Array.isArray(input) ||
    !Object.keys(input).length
  ) {
    throw new APIError(-1, "每个阶段都需要模型和非空的 JSON 输入", 2);
  }
  return { model, budget, input: input as Record<string, unknown> };
}

function characterState(snapshot: CollaborationSnapshot, node: string) {
  requireEditor(snapshot);
  if (!snapshot.canGenerate)
    throw new CollaborationError(-1, "当前成员没有生成授权", 403, "permission_denied");
  const data = snapshot.canvas.payload_json?.nodes?.find((entry) => entry.id === node)?.data;
  if (data?.kind !== "character") throw new APIError(-1, "目标不是角色卡节点", 2);
  return data;
}

async function submitStage(
  ctx: Ctx,
  run: CharacterRun,
  name: "image" | "voice",
  key: string,
  child: string,
  binding?: VoiceBinding,
): Promise<Task> {
  const config = run[name];
  const target = name === "image" ? run.node : `voice:${run.node}`;
  const preview: GenerationPreview = {
    epoch: run.epoch,
    revision: binding?.revision,
    resume: true,
    budget: run.cumulative,
  };
  const response = await submitGenerationRequest(
    ctx,
    `${child}-${name}`,
    {
      kind: name === "image" ? "image" : "audio",
      model_id: config.model,
      canvas_id: run.canvas,
      node_id: target,
      node_ids: [target],
      input: binding?.input ?? config.input,
    },
    config.budget,
    preview,
  ).catch((err: unknown) => {
    if (err instanceof CollaborationError && err.reason === "result_unknown")
      throw unknownGeneration(key);
    throw err;
  });
  const item = response.items?.[0];
  if (item?.task && isPositiveVersion(item.task.id)) return item.task;
  if (item?.error)
    throw new APIError(item.error.code ?? -1, item.error.message || "生成任务未受理", 409);
  throw unknownGeneration(key);
}

function pinVoice(
  run: CharacterRun,
  snapshot: CollaborationSnapshot,
  imageId: number,
): VoiceBinding {
  if (snapshot.epoch !== run.epoch)
    throw new CollaborationError(30018, "角色所属协作会话已失效", 409, "epoch_expired");
  const data = characterState(snapshot, run.node);
  if (
    String(data.characterImageTaskId) !== String(imageId) ||
    !data.assetId ||
    data.characterImageAssetId !== data.assetId
  ) {
    throw new CollaborationError(
      30002,
      "本次形象尚未成为角色主图，请核对任务和画布后使用原编号恢复",
      409,
      "conflict",
      { imageId },
    );
  }
  const input = { ...run.voice.input };
  if (run.fromImage) {
    const asset = Number(data.assetId);
    if (!isPositiveVersion(asset)) throw new APIError(-1, "图片素材编号超出当前生成接口范围", 409);
    input.images = [asset];
  }
  return { imageId, revision: snapshot.canvas.revision, input };
}

export async function canvasCharacterPair(ctx: Ctx, args: Args): Promise<void> {
  const [, code, node] = args.positional;
  if (!code || !node || args.positional.length !== 3) throw new APIError(-1, usage, 2);
  const limit = intFlag(args.flags, "max-credits", undefined, { min: 0 });
  const imageLimit = intFlag(args.flags, "image-max-credits", undefined, { min: 0, max: limit });
  const cumulative = generationBudget(args.flags);
  if (limit === undefined || imageLimit === undefined) throw new APIError(-1, usage, 2);
  const image = stage(args, "image", imageLimit);
  const voice = stage(args, "voice", limit - imageLimit);
  const fromImage = boolFlag(args.flags, "voice-from-image");
  if (fromImage && voice.input.images !== undefined)
    throw new APIError(-1, "使用 --voice-from-image 时请省略音色输入中的 images", 2);
  const key =
    args.flags["idempotency-key"] === undefined
      ? randomUUID()
      : stringFlag(args.flags, "idempotency-key", usage);
  if (!/^[a-zA-Z0-9_-]{1,64}$/.test(key)) throw new APIError(-1, "组合任务幂等键格式无效", 2);
  const startedAt = Date.now();
  const deadline = startedAt + intFlag(args.flags, "timeout", 600, { max: 86_400 }) * 1_000;
  const intervalSeconds = intFlag(args.flags, "interval", 3, { max: 60 });
  const snapshot = await getCollaboration(ctx, code);
  characterState(snapshot, node);
  const actor = await getCollaborationActor(ctx);
  const child = createHash("sha256")
    .update(JSON.stringify([baseOf(ctx), key, "character-workflow"]))
    .digest("hex")
    .slice(0, 40);
  const journal = path.join(configDir(ctx.env), "collaboration-character", child);
  const run: CharacterRun = {
    actor,
    canvas: snapshot.canvas.id,
    epoch: snapshot.epoch,
    node,
    image,
    voice,
    fromImage,
    ...(cumulative ? { cumulative } : {}),
  };
  const saved = readGenerationJournal<CharacterRun>(`${journal}.run`);
  if (saved && (saved.actor !== actor || saved.canvas !== run.canvas))
    throw new CollaborationError(-1, "组合任务属于另一账号或画布", 403, "permission_denied");
  if (saved && saved.epoch !== run.epoch)
    throw new CollaborationError(30018, "组合任务所属协作会话已失效", 409, "epoch_expired");
  if (saved && !isDeepStrictEqual(saved, run))
    throw new CollaborationError(-1, "编号已绑定其他组合任务参数", 409, "op_id_reused");
  if (!saved) writeCollaborationJournal(`${journal}.run`, run, key);
  ctx.stderr(
    `组合任务编号 ${JSON.stringify(key)}，形象积分上限 ${image.budget}，音色积分上限 ${voice.budget}。`,
  );
  let binding = readGenerationJournal<VoiceBinding>(`${journal}.voice`);
  if (!binding) {
    const imageTask = await submitStage(ctx, run, "image", key, child);
    const outcome = await waitTaskTerminal(ctx, imageTask, {
      deadline,
      startedAt,
      intervalSeconds,
    });
    if (outcome.error) throw outcome.error;
    if (outcome.timedOut)
      throw new APIError(
        -1,
        `等待形象任务 ${imageTask.id} 超时，请用原 --idempotency-key ${key} 恢复`,
        0,
        3,
      );
    if (outcome.task.status !== "succeeded")
      throw new APIError(-1, outcome.task.error_message || "角色形象生成未完成", 409);
    binding = pinVoice(run, await getCollaboration(ctx, code), imageTask.id);
    writeCollaborationJournal(`${journal}.voice`, binding, key);
  }
  const voiceTask = await submitStage(ctx, run, "voice", key, child, binding);
  const result = boolFlag(args.flags, "wait")
    ? await waitTaskTerminal(ctx, voiceTask, { deadline, startedAt, intervalSeconds })
    : undefined;
  if (result?.error) throw result.error;
  if (result?.timedOut)
    throw new APIError(
      -1,
      `等待音色任务 ${voiceTask.id} 超时，可用 task wait ${voiceTask.id} 继续等待`,
      0,
      3,
    );
  if (result && result.task.status !== "succeeded")
    throw new APIError(-1, result.task.error_message || "角色音色生成未完成", 409);
  emit(
    ctx,
    { idempotencyKey: key, imageId: binding.imageId, voice: result?.task ?? voiceTask },
    `角色形象任务 ${binding.imageId} 已完成，音色任务 ${voiceTask.id} ${result?.task.status ?? voiceTask.status}`,
  );
}
