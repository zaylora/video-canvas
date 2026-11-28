import { APIError } from "../client.ts";
import type { Args, Ctx } from "../types.ts";
import { intFlag, stringFlag } from "./args.ts";
import { generationBudget, submitGenerationRequest } from "./collaboration-generation.ts";
import { versionFlag } from "./collaboration-version.ts";
import { CollaborationError, getCollaboration } from "./collaboration.ts";
import { emit } from "./shared.ts";

const usage =
  "用法：canvas media separate-voice <画布> <音频节点> --source-asset ID --epoch E --revision N --model KEY --max-credits N --idempotency-key ID";

export async function separateVoice(ctx: Ctx, args: Args): Promise<void> {
  const [, code, node] = args.positional;
  const epoch = versionFlag(args.flags, "epoch");
  const revision = versionFlag(args.flags, "revision");
  const maxCredits = intFlag(args.flags, "max-credits", undefined, { min: 0 });
  const budget = generationBudget(args.flags);
  const asset = stringFlag(args.flags, "source-asset", usage);
  const model = stringFlag(args.flags, "model", usage);
  const key = stringFlag(args.flags, "idempotency-key", usage);
  if (
    !code ||
    !node ||
    args.positional.length !== 3 ||
    epoch === undefined ||
    revision === undefined ||
    maxCredits === undefined ||
    !/^[1-9]\d{0,19}$/.test(asset) ||
    BigInt(asset) > 18_446_744_073_709_551_615n ||
    !/^[\w.-]{1,128}$/.test(key)
  ) {
    throw new APIError(-1, usage, 2);
  }
  const snapshot = await getCollaboration(ctx, code);
  if (snapshot.role === "viewer" || !snapshot.canGenerate)
    throw new CollaborationError(-1, "当前成员没有生成授权", 403, "permission_denied");
  if (snapshot.epoch !== epoch)
    throw new CollaborationError(30018, "协作会话已变更", 409, "epoch_expired");
  const target = snapshot.canvas.payload_json?.nodes?.find((item) => item.id === node);
  if (
    snapshot.canvas.revision === revision &&
    (target?.data?.kind !== "audio" || String(target.data.assetId) !== asset)
  ) {
    throw new CollaborationError(30002, "来源音频与观察版本不一致", 409, "conflict");
  }
  const result = await submitGenerationRequest(
    ctx,
    key,
    {
      kind: "audio",
      model_id: model,
      canvas_id: snapshot.canvas.id,
      node_id: node,
      node_ids: [node],
      input: { audio_operation: "vocal_separation", audios: [asset] },
    },
    maxCredits,
    { epoch, revision, resume: true, budget },
  );
  if (!result.items?.some((item) => item.task))
    throw new APIError(-1, result.items?.[0]?.error?.message ?? "人声分离任务未受理", 422);
  emit(ctx, { ...result, idempotencyKey: key }, `已受理人声分离任务 ${key}`);
}
