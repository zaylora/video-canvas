import { APIError, encodeUnsignedInteger } from "../client.ts";
import type { Args, Ctx } from "../types.ts";
import { intFlag, stringFlag } from "./args.ts";
import { compareVersions, versionFlag } from "./collaboration-version.ts";
import { canvasCharacterPair } from "./collaboration-character.ts";
import { newCollaborationPlan, requireEditor, runCollaborationPlan } from "./collaboration-plan.ts";
import {
  canvasPath,
  CollaborationError,
  collaborationRequest,
  getCollaboration,
  type CollaborationSnapshot,
} from "./collaboration.ts";
import { askYes, emit, readJSONFile, requireInteractive } from "./shared.ts";
import { generate } from "./generate.ts";

function findNode(snapshot: CollaborationSnapshot, id: string) {
  const node = snapshot.canvas.payload_json?.nodes?.find((entry) => entry.id === id);
  if (!node?.data || node.type === "group") throw new APIError(-1, `画布上没有节点 ${id}`, 2);
  return node.data;
}

async function setField(
  ctx: Ctx,
  snapshot: CollaborationSnapshot,
  node: string,
  field: string,
  value: unknown,
): Promise<void> {
  const plan = await newCollaborationPlan(
    ctx,
    snapshot,
    [{ kind: "node", id: node, fields: { [field]: value } }],
    field === "data.scene" || field === "data.storyboard" || field === "data.character",
  );
  const result = await runCollaborationPlan(ctx, plan);
  emit(ctx, result, `已确认操作 ${plan.opId}，版本 ${result.revision}`);
}

export async function canvasOutput(ctx: Ctx, args: Args): Promise<void> {
  const [action, code, node] = args.positional;
  const usage = "用法：canvas output list|select <画布> <节点> [--output-id <结果>]";
  if (!code || !node || !["list", "select"].includes(action ?? ""))
    throw new APIError(-1, usage, 2);
  const snapshot = await getCollaboration(ctx, code);
  const data = findNode(snapshot, node);
  const outputs = data.outputs ?? [];
  if (action === "list") {
    return emit(
      ctx,
      { activeOutputId: data.activeOutputId, outputs },
      outputs.map((output) => `${output.id ?? ""}\t${output.assetId ?? ""}`).join("\n") ||
        "当前没有候选产物",
    );
  }
  const outputId = stringFlag(args.flags, "output-id", usage);
  if (!outputs.some((output) => outputId === String(output.id)))
    throw new APIError(-1, "该节点没有指定的候选结果", 2);
  await setField(ctx, snapshot, node, "data.activeOutputId", outputId);
}

function optionalFlag(args: Args, name: string, usage: string): string | undefined {
  return args.flags[name] === undefined ? undefined : stringFlag(args.flags, name, usage);
}

export async function canvasAsset(ctx: Ctx, args: Args): Promise<void> {
  const [action, code, node] = args.positional;
  const usage = "用法：canvas asset attach|replace|collect <画布> <节点> --asset-id <素材>";
  if (!code || !node || !["attach", "replace", "collect"].includes(action ?? ""))
    throw new APIError(-1, usage, 2);
  const assetId = stringFlag(args.flags, "asset-id", usage);
  if (!/^[1-9]\d*$/.test(assetId)) throw new APIError(-1, "素材 ID 必须是正整数", 2);
  const snapshot = await getCollaboration(ctx, code);
  requireEditor(snapshot);
  const data = findNode(snapshot, node);
  if (action !== "collect") {
    if (!["image", "audio", "video"].includes(data.kind ?? ""))
      throw new APIError(-1, "请选择图片、视频或音频节点", 2);
    if (action === "attach" && (data.assetId || data.activeOutputId))
      throw new APIError(-1, "节点已有素材，请显式使用 replace", 2);
    return setField(ctx, snapshot, node, "data.assetId", assetId);
  }
  if (!snapshot.canCopyToLibrary)
    throw new CollaborationError(-1, "当前成员没有资产库收藏权限", 403, "permission_denied");
  await collaborationRequest(ctx, `${canvasPath(snapshot.canvas.id)}/collaboration/library`, {
    method: "POST",
    body: {
      epoch: snapshot.epoch,
      nodeId: node,
      asset_id: encodeUnsignedInteger(assetId),
      name: optionalFlag(args, "name", usage),
      folder_id: optionalFlag(args, "folder", usage),
    },
  });
  emit(ctx, { assetId }, "已加入个人资产库");
}

export async function assetLibraryList(ctx: Ctx, args: Args): Promise<void> {
  const usage =
    "用法：asset library list [--folder <文件夹|all>] [--kind image|video|audio] [--keyword 关键词] [--cursor 游标]";
  const data = await collaborationRequest(ctx, "/asset-library/items", {
    query: {
      folder_id: optionalFlag(args, "folder", usage),
      kind: optionalFlag(args, "kind", usage),
      q: optionalFlag(args, "keyword", usage),
      cursor: optionalFlag(args, "cursor", usage),
      limit: intFlag(args.flags, "limit", undefined, { max: 100 }),
    },
  });
  emit(ctx, data, JSON.stringify(data, null, 2));
}

export async function canvasStructuredNode(
  ctx: Ctx,
  args: Args,
  kind: "character" | "director" | "storyboard",
): Promise<void> {
  const [action, code, node] = args.positional;
  const usage = `用法：canvas ${kind} get|set <画布> <节点> [--input <文件> --revision <观察版本>]`;
  if (!code || !node || !["get", "set"].includes(action ?? "")) throw new APIError(-1, usage, 2);
  const snapshot = await getCollaboration(ctx, code);
  const data = findNode(snapshot, node);
  if (data.kind !== kind) throw new APIError(-1, `节点类型应为 ${kind}`, 2);
  const field = kind === "director" ? "scene" : kind;
  if (action === "get") {
    const result = { revision: String(snapshot.canvas.revision), data: data[field] };
    return emit(ctx, result, JSON.stringify(result, null, 2));
  }
  const filename = stringFlag(args.flags, "input", usage);
  const revision = versionFlag(args.flags, "revision");
  if (revision === undefined) throw new APIError(-1, usage, 2);
  if (revision !== snapshot.canvas.revision)
    throw new CollaborationError(
      30002,
      "观察版本已变化，请比较当前内容后重新提交",
      409,
      "conflict",
    );
  const value: unknown = readJSONFile(filename);
  if (typeof value !== "object" || value === null || Array.isArray(value))
    throw new APIError(-1, "节点内容须为 JSON 对象", 2);
  await setField(ctx, snapshot, node, `data.${field}`, value);
}

export async function canvasDirectorScene(ctx: Ctx, args: Args): Promise<void> {
  const [action, code, node] = args.positional;
  const usage =
    "用法：canvas director scene get|apply <画布> <节点> [--epoch E --revision N --input <文件>]";
  if (!code || !node || args.positional.length !== 3 || (action !== "get" && action !== "apply"))
    throw new APIError(-1, usage, 2);
  const snapshot = await getCollaboration(ctx, code);
  const data = findNode(snapshot, node);
  if (data.kind !== "director") throw new APIError(-1, "目标不是导演台节点", 2);
  const result = {
    epoch: String(snapshot.epoch),
    revision: String(snapshot.canvas.revision),
    data: data.scene,
  };
  if (action === "get") return emit(ctx, result, JSON.stringify(result, null, 2));
  requireEditor(snapshot);
  const epoch = versionFlag(args.flags, "epoch");
  const revision = versionFlag(args.flags, "revision");
  if (epoch === undefined || revision === undefined) throw new APIError(-1, usage, 2);
  if (epoch !== snapshot.epoch)
    throw new CollaborationError(30018, "协作会话已变更，请重新读取导演台", 409, "epoch_expired");
  if (compareVersions(revision, snapshot.canvas.revision) > 0)
    throw new CollaborationError(30002, "观察版本无效，请重新读取导演台", 409, "conflict");
  const value = readJSONFile(stringFlag(args.flags, "input", usage));
  if (
    !value ||
    typeof value !== "object" ||
    Array.isArray(value) ||
    !["add", "set", "rm"].includes(value.action) ||
    !["characters", "props", "cameras"].includes(value.collection) ||
    typeof value.id !== "string" ||
    !value.id ||
    [...value.id].length > 128 ||
    value.id.includes("\0")
  )
    throw new APIError(-1, "需要合法的 action、collection 和对象 id", 2);
  if (value.action === "rm") {
    requireInteractive(ctx, "删除导演台对象", "请在终端确认目标对象。");
    await askYes(
      ctx,
      `从导演台 ${JSON.stringify(node)} 删除 ${value.collection} 中的 ${JSON.stringify(value.id)}？输入 yes 确认：`,
      "已取消删除",
    );
  }
  const plan = await newCollaborationPlan(
    ctx,
    snapshot,
    [{ kind: "node", id: node, action: "director-object", value }],
    false,
    revision,
  );
  const committed = await runCollaborationPlan(ctx, plan);
  emit(ctx, committed, `已确认操作 ${plan.opId}，版本 ${committed.revision}`);
}

export async function canvasCharacterGenerate(ctx: Ctx, args: Args): Promise<void> {
  const [, code, node] = args.positional;
  const usage =
    "用法：canvas character generate <画布> <角色节点> --kind image|voice --model <模型> --max-credits N";
  const kind = stringFlag(args.flags, "kind", usage);
  if (kind === "image-and-voice") return canvasCharacterPair(ctx, args);
  if (!code || !node || (kind !== "image" && kind !== "voice") || args.positional.length !== 3) {
    throw new APIError(-1, usage, 2);
  }
  if (intFlag(args.flags, "max-credits", undefined, { min: 0 }) === undefined)
    throw new APIError(-1, usage, 2);
  const snapshot = await getCollaboration(ctx, code);
  requireEditor(snapshot);
  if (findNode(snapshot, node).kind !== "character")
    throw new APIError(-1, "目标不是角色卡节点", 2);
  if (kind === "voice" && intFlag(args.flags, "count", 1) !== 1)
    throw new APIError(-1, "角色一次只能生成一个音色", 2);
  return generate(ctx, {
    positional: [kind === "image" ? "image" : "audio"],
    flags: {
      ...args.flags,
      canvas: snapshot.canvas.id,
      node: kind === "image" ? node : `voice:${node}`,
    },
  });
}
