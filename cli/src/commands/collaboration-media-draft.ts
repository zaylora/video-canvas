import { stat } from "node:fs/promises";

import { APIError, uploadFile } from "../client.ts";
import type { Args, Ctx, WireVersion } from "../types.ts";
import { stringFlag } from "./args.ts";
import { versionFlag } from "./collaboration-version.ts";
import {
  canvasPath,
  CollaborationError,
  collaborationRequest,
  getCollaboration,
  type CollaborationCommit,
} from "./collaboration.ts";
import { baseOf, emit, requireToken } from "./shared.ts";

const stageUsage =
  "用法：canvas media stage <画布> <来源节点> --file <文件> --source-asset <素材ID> --epoch E --revision N --op-id ID";
const attachUsage = "用法：canvas media attach <画布> --epoch E --op-id ID";

export async function canvasMediaDraft(
  ctx: Ctx,
  args: Args,
  action: "stage" | "attach",
): Promise<void> {
  const usage = action === "stage" ? stageUsage : attachUsage;
  const [, code, source] = args.positional;
  const epoch = versionFlag(args.flags, "epoch");
  const opId = stringFlag(args.flags, "op-id", usage);
  if (
    !code ||
    epoch === undefined ||
    args.positional.length !== (action === "stage" ? 3 : 2) ||
    !/^[\w-]{1,64}$/.test(opId)
  ) {
    throw new APIError(-1, usage, 2);
  }
  const snapshot = await getCollaboration(ctx, code);
  if (snapshot.epoch !== epoch)
    throw new CollaborationError(30018, "协作会话已变更", 409, "epoch_expired");
  if (snapshot.role === "viewer")
    throw new CollaborationError(-1, "没有加工产物挂载权限", 403, "forbidden");
  if (!snapshot.capabilities?.actions?.includes(`media-${action}`))
    throw new CollaborationError(-1, "站点未开放加工产物暂存与挂载", 406, "unsupported");
  const endpoint = `${canvasPath(snapshot.canvas.id)}/collaboration/media/drafts/${encodeURIComponent(opId)}`;
  if (action === "attach") {
    const result = await collaborationRequest<CollaborationCommit>(ctx, `${endpoint}/attach`, {
      method: "POST",
      body: { epoch },
    });
    if (result?.opId !== opId || result.epoch !== epoch || result.changes?.length !== 2) {
      throw new APIError(-1, "未收到有效的挂载收据，请按原操作编号重试", 502);
    }
    emit(ctx, result, `已确认加工素材挂载，画布版本 ${result.revision}`);
    return;
  }
  await stageDraft(ctx, args, source, epoch, endpoint, opId);
}

async function stageDraft(
  ctx: Ctx,
  args: Args,
  source: string | undefined,
  epoch: WireVersion,
  endpoint: string,
  opId: string,
) {
  const file = stringFlag(args.flags, "file", stageUsage);
  const sourceAsset = stringFlag(args.flags, "source-asset", stageUsage);
  const revision = versionFlag(args.flags, "revision");
  if (
    !source ||
    revision === undefined ||
    !/^[1-9]\d{0,19}$/.test(sourceAsset) ||
    BigInt(sourceAsset) > 18_446_744_073_709_551_615n
  ) {
    throw new APIError(-1, stageUsage, 2);
  }
  const info = await stat(file).catch(() => undefined);
  if (!info?.isFile() || !info.size) throw new APIError(-1, "请选择非空的本地加工文件", 2);
  const result = await uploadFile<{ draftId: string }>({
    base: baseOf(ctx),
    token: requireToken(ctx),
    path: endpoint,
    file,
    fetchImpl: ctx.fetchImpl,
    query: { sourceNode: source, sourceAsset, revision, epoch },
    signal: AbortSignal.timeout(600_000),
  });
  if (result?.draftId !== opId) throw new APIError(-1, "站点返回了无效的暂存编号", 502);
  emit(ctx, result, `已暂存加工产物 ${opId}`);
}
