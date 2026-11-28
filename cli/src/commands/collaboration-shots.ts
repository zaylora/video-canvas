import { randomUUID } from "node:crypto";

import { APIError } from "../client.ts";
import type { Args, Ctx, StoryboardShot } from "../types.ts";
import { intFlag, stringFlag } from "./args.ts";
import { compareVersions, versionFlag } from "./collaboration-version.ts";
import { newCollaborationPlan, requireEditor, runCollaborationPlan } from "./collaboration-plan.ts";
import {
  CollaborationError,
  getCollaboration,
  type CollaborationSnapshot,
} from "./collaboration.ts";
import { askYes, emit, readJSONFile, requireInteractive } from "./shared.ts";

const usage =
  "用法：canvas storyboard shot list|add|set|move|rm <画布> <节点> [镜头] [--epoch N --revision N]";
const textFields = new Set(["scene", "shot", "camera", "action", "dialogue"]);
const referenceKinds: Record<string, string> = { imageNodeId: "image", videoNodeId: "video" };

function validShotId(id: string): boolean {
  return id !== "" && [...id].length <= 128 && !id.includes("\0");
}

function readPatch(args: Args, snapshot: CollaborationSnapshot): StoryboardShot {
  const input: unknown = readJSONFile(stringFlag(args.flags, "input", usage));
  if (!input || typeof input !== "object" || Array.isArray(input)) {
    throw new APIError(-1, "镜头输入必须是 JSON 对象", 2);
  }
  const fields = Object.entries(input as Record<string, unknown>);
  for (const [key, value] of fields) {
    if (textFields.has(key) && typeof value === "string" && !value.includes("\0")) continue;
    if (
      key === "duration" &&
      typeof value === "number" &&
      value >= 1 &&
      value <= 60 &&
      Math.round(value * 10) === value * 10
    )
      continue;
    if (
      Object.hasOwn(referenceKinds, key) &&
      (value === null ||
        snapshot.canvas.payload_json?.nodes?.some(
          (node) => node.id === value && node.data?.kind === referenceKinds[key],
        ))
    )
      continue;
    throw new APIError(-1, `镜头字段或取值无效：${key}`, 2);
  }
  return Object.fromEntries(fields);
}

export async function storyboardShot(ctx: Ctx, args: Args): Promise<void> {
  const [action, code, node, shot] = args.positional;
  if (
    !action ||
    !code ||
    !node ||
    !["list", "add", "set", "move", "rm"].includes(action) ||
    args.positional.length > 4
  ) {
    throw new APIError(-1, usage, 2);
  }
  const snapshot = await getCollaboration(ctx, code);
  const data = snapshot.canvas.payload_json?.nodes?.find((entry) => entry.id === node)?.data;
  if (data?.kind !== "storyboard") throw new APIError(-1, "目标不是分镜表节点", 2);
  const { epoch, canvas } = snapshot;
  const shots = data.storyboard?.shots ?? [];
  if (action === "list") {
    const state = { epoch: String(epoch), revision: String(canvas.revision), shots };
    return emit(ctx, state, JSON.stringify(state, null, 2));
  }
  requireEditor(snapshot);
  const observedEpoch = versionFlag(args.flags, "epoch");
  const revision = versionFlag(args.flags, "revision");
  if (observedEpoch === undefined || revision === undefined) throw new APIError(-1, usage, 2);
  if (epoch !== observedEpoch)
    throw new CollaborationError(30018, "协作会话已变更，请重新读取分镜", 409, "epoch_expired");
  if (compareVersions(revision, canvas.revision) > 0)
    throw new CollaborationError(30002, "观察版本已变化，请重新读取分镜", 409, "conflict");
  const shotId = shot ?? (action === "add" ? randomUUID() : "");
  if (!validShotId(shotId)) throw new APIError(-1, "镜头 ID 无效", 2);
  const value = await prepareShotEdit(ctx, args, snapshot, action, shots, shotId, node);
  const plan = await newCollaborationPlan(
    ctx,
    snapshot,
    [{ kind: "node", id: node, action: "storyboard-shot", value }],
    false,
    revision,
  );
  const result = await runCollaborationPlan(ctx, plan);
  emit(ctx, { shotId, ...result }, `镜头 ${shotId} 已确认，版本 ${result.revision}`);
}

async function prepareShotEdit(
  ctx: Ctx,
  args: Args,
  snapshot: CollaborationSnapshot,
  action: string,
  shots: StoryboardShot[],
  id: string,
  node: string,
): Promise<Record<string, unknown>> {
  if (action === "add") {
    const fields = args.flags.input === undefined ? undefined : readPatch(args, snapshot);
    return { action, id, fields };
  }
  if (action === "set") {
    const fields = readPatch(args, snapshot);
    if (Object.keys(fields).length === 0) throw new APIError(-1, "请填写要修改的镜头字段", 2);
    return { action, id, fields };
  }
  if (action === "move") {
    const index = intFlag(args.flags, "index", undefined, { max: 2000 });
    if (index === undefined) throw new APIError(-1, "请用 --index 指定新镜号（从 1 开始）", 2);
    return { action, id, index };
  }
  const index = shots.findIndex((entry) => entry.id === id);
  if (index < 0)
    throw new CollaborationError(30002, "该镜头已不存在，请重新读取分镜", 409, "conflict");
  requireInteractive(ctx, "删除镜头", "请在终端确认目标镜头。");
  ctx.stderr(
    `从分镜表 ${node} 删除第 ${index + 1} 镜 ${JSON.stringify(id)}，关联的媒体节点会保留。`,
  );
  await askYes(ctx, "输入 yes 确认删除：", "已取消删除镜头");
  return { action, id };
}
