import { randomUUID } from "node:crypto";
import { readFileSync } from "node:fs";

import { APIError } from "../client.ts";
import type { Args, Ctx } from "../types.ts";
import { intFlag, stringFlag } from "./args.ts";
import {
  generationBudget,
  prepareCollaborationGeneration,
  unknownGeneration,
  type GenerationPreview,
} from "./collaboration-generation.ts";
import { canvasPath, collaborationRequest, getCollaboration } from "./collaboration.ts";
import { askYes, emit, requireInteractive, sleep } from "./shared.ts";

interface SplitJob {
  id: string;
  status: "pending" | "running" | "succeeded" | "failed";
  error?: string;
}

export async function storyboardSplit(ctx: Ctx, args: Args): Promise<void> {
  const [, code, node] = args.positional;
  const usage =
    "用法：canvas storyboard split <画布> <节点> --script-file <剧本> --mode append|replace --max-credits N";
  if (!code || !node) throw new APIError(-1, usage, 2);
  const file = stringFlag(args.flags, "script-file", usage);
  let script: string;
  try {
    script = readFileSync(file, "utf8").trim();
  } catch {
    throw new APIError(-1, "无法读取剧本文件", 2);
  }
  if (!script) throw new APIError(-1, "剧本不能为空", 2);
  const mode = stringFlag(args.flags, "mode", usage);
  if (mode !== "append" && mode !== "replace") throw new APIError(-1, usage, 2);
  const limit = intFlag(args.flags, "max-credits", undefined, { min: 0 });
  const budget = generationBudget(args.flags);
  if (limit === undefined) throw new APIError(-1, usage, 2);
  const key =
    args.flags["idempotency-key"] === undefined
      ? randomUUID()
      : stringFlag(args.flags, "idempotency-key", usage);
  if (!/^[a-zA-Z0-9_-]{1,64}$/.test(key)) throw new APIError(-1, "无效的拆分镜幂等键", 2);
  const model = args.flags.model === undefined ? undefined : stringFlag(args.flags, "model", usage);
  if (mode === "replace") {
    requireInteractive(ctx, "覆盖分镜", "可使用 --mode append 保留已有镜头。");
    const snapshot = await getCollaboration(ctx, code);
    const target = snapshot.canvas.payload_json?.nodes?.find((entry) => entry.id === node);
    if (target?.data?.kind !== "storyboard") throw new APIError(-1, "目标不是分镜表节点", 2);
    ctx.stderr(
      `将用拆分结果覆盖分镜表 ${node} 当前的 ${target.data.storyboard?.shots?.length ?? 0} 个镜头，积分上限 ${limit}。`,
    );
    await askYes(ctx, "输入 yes 确认覆盖：", "已取消拆分镜");
    return submitSplit(ctx, code, node, key, script, mode, model, limit, {
      epoch: snapshot.epoch,
      revision: snapshot.canvas.revision,
      budget,
    });
  }
  await submitSplit(ctx, code, node, key, script, mode, model, limit, { budget });
}

async function submitSplit(
  ctx: Ctx,
  code: string,
  node: string,
  key: string,
  script: string,
  mode: string,
  model: string | undefined,
  limit: number,
  preview?: GenerationPreview,
): Promise<void> {
  const input = { script, mode };
  const request = {
    canvas_id: code,
    node_id: node,
    model_id: "storyboard-split",
    input,
    model: model ?? "",
    request_kind: "storyboard",
  };
  const prepared = await prepareCollaborationGeneration(
    ctx,
    key,
    request,
    limit,
    "storyboardBudgetLimit",
    preview,
  );
  if (!prepared) throw new APIError(-1, "目标画布未启用协作拆分镜", 409);
  const body = {
    node_id: node,
    script,
    mode,
    model,
    collaboration: prepared.collaboration,
    request_key: key,
  };
  const result = await collaborationRequest<SplitJob>(
    ctx,
    `${canvasPath(prepared.canvas_id)}/storyboard/split`,
    {
      method: "POST",
      body,
      signal: AbortSignal.timeout(15_000),
    },
  ).catch((err: unknown) => {
    if (err instanceof APIError && err.retryable) throw unknownGeneration(key);
    throw err;
  });
  if (!result?.id) throw unknownGeneration(key);
  emit(ctx, result, `拆分镜任务 ${result.id} ${result.status}`);
}

export async function storyboardJob(ctx: Ctx, args: Args): Promise<void> {
  const [action, code, job] = args.positional;
  if (!code || !job || !/^[0-9a-f]{32}$/i.test(job))
    throw new APIError(-1, "用法：canvas storyboard status|wait <画布> <任务>", 2);
  const interval = intFlag(args.flags, "interval", 3, { max: 60 });
  const timeout = intFlag(args.flags, "timeout", 600, { max: 86_400 });
  const deadline = Date.now() + timeout * 1000;
  do {
    const result = await collaborationRequest<SplitJob>(
      ctx,
      `${canvasPath(code)}/storyboard/split/${job}`,
      { signal: AbortSignal.timeout(10_000) },
    );
    if (action === "status" || result.status === "succeeded")
      return emit(ctx, result, JSON.stringify(result, null, 2));
    if (result.status === "failed") throw new APIError(-1, result.error || "拆分镜失败", 409);
    await sleep(Math.max(0, Math.min(interval * 1_000, deadline - Date.now())));
  } while (Date.now() < deadline);
  throw new APIError(-1, `等待超时，拆分镜任务 ${job} 仍在服务器运行`, 0, 3);
}
