import path from "node:path";

import { APIError } from "../client.ts";
import type { Args, Ctx, Model } from "../types.ts";
import { intFlag, stringFlag } from "./args.ts";
import { versionFlag } from "./collaboration-version.ts";
import { generationBudget, submitGenerationRequest } from "./collaboration-generation.ts";
import { pngDimensions } from "./collaboration-media-grid.ts";
import { CollaborationError, collaborationRequest, getCollaboration } from "./collaboration.ts";
import { emit } from "./shared.ts";

const maxEdge = 4096;
const instruction = "保持原图内容不变，自然补全图片中的透明区域。";
const usage =
  "用法：canvas media outpaint-submit <画布> <底片节点> --source-asset ID --epoch E --revision N --model KEY --max-credits N --idempotency-key ID";

export function outpaintPadding(args: Args) {
  const options = { min: 0, max: maxEdge };
  const padding = {
    left: intFlag(args.flags, "left", 0, options),
    right: intFlag(args.flags, "right", 0, options),
    top: intFlag(args.flags, "top", 0, options),
    bottom: intFlag(args.flags, "bottom", 0, options),
  };
  if (!Object.values(padding).some(Boolean))
    throw new APIError(-1, "扩图至少需要一个大于 0 的像素边距", 2);
  return padding;
}

export async function renderOutpaint(
  input: string,
  padding: ReturnType<typeof outpaintPadding>,
  directory: string,
  encode: (input: string, mime: string, filters: string[], target: string) => Promise<void>,
) {
  const [originalWidth, originalHeight] = await pngDimensions(input);
  const fullWidth = originalWidth + padding.left + padding.right;
  const fullHeight = originalHeight + padding.top + padding.bottom;
  const shrink = Math.min(1, maxEdge / Math.max(fullWidth, fullHeight));
  const width = Math.max(1, Math.round(fullWidth * shrink));
  const height = Math.max(1, Math.round(fullHeight * shrink));
  const contentWidth = Math.max(1, Math.round(originalWidth * shrink));
  const contentHeight = Math.max(1, Math.round(originalHeight * shrink));
  const x = Math.min(width - contentWidth, Math.round(padding.left * shrink));
  const y = Math.min(height - contentHeight, Math.round(padding.top * shrink));
  if (width === contentWidth && height === contentHeight)
    throw new APIError(-1, "缩放后没有可扩展的像素，请增加边距", 422);
  const scale = shrink < 1 ? `scale=${contentWidth}:${contentHeight},` : "";
  const filter = `${scale}format=rgba,pad=${width}:${height}:${x}:${y}:color=black@0`;
  const file = path.join(directory, "outpaint.png");
  await encode(input, "image/png", ["-vf", filter, "-frames:v", "1", "-threads", "1"], file);
  return { file, width, height };
}

export async function outpaintSubmit(ctx: Ctx, args: Args): Promise<void> {
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
  if (target?.data?.kind !== "image") throw new APIError(-1, "请选择已挂载的扩图底片节点", 2);
  if (snapshot.canvas.revision === revision && String(target.data.assetId) !== asset)
    throw new CollaborationError(30002, "底片素材与观察版本不一致", 409, "conflict");
  const models = await collaborationRequest<Model[]>(ctx, "/models", { query: { kind: "image" } });
  const caps = models.find((entry) => entry.key === model && entry.kind === "image")?.capabilities;
  if (!caps?.ops?.includes("i2i") || !caps.refs?.image?.on)
    throw new CollaborationError(-1, "所选模型未开放图生图参考", 406, "unsupported");
  const text =
    args.flags.prompt === undefined ? "" : stringFlag(args.flags, "prompt", usage).trim();
  const input = {
    op: "i2i",
    prompt: instruction + text,
    images: [asset],
    ...outpaintRatio(args, caps),
  };
  const result = await submitGenerationRequest(
    ctx,
    key,
    {
      kind: "image",
      model_id: model,
      canvas_id: snapshot.canvas.id,
      node_id: node,
      node_ids: [node],
      input,
    },
    maxCredits,
    { epoch, revision, resume: true, budget },
  );
  if (!result.items?.some((item) => item.task))
    throw new APIError(-1, result.items?.[0]?.error?.message ?? "扩图任务未受理", 422);
  emit(ctx, { ...result, idempotencyKey: key }, `已受理扩图任务 ${key}`);
}

function outpaintRatio(args: Args, capabilities: NonNullable<Model["capabilities"]>) {
  const ratio = args.flags.ratio;
  if (ratio === undefined) return {};
  const key = ["aspect_ratio", "ratio"].find((name) => capabilities.params?.[name] !== undefined);
  const field = key === undefined ? undefined : capabilities.params?.[key];
  if (
    !key ||
    typeof ratio !== "string" ||
    !/^[1-9]\d{0,3}:[1-9]\d{0,3}$/.test(ratio) ||
    (field?.type === "enum" && !field.options?.includes(ratio))
  )
    throw new APIError(-1, "--ratio 必须为模型支持的宽高比", 2);
  return { [key]: ratio };
}
