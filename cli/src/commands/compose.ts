import { APIError } from "../client.ts";
import { retryNotice, withRetry } from "../retry.ts";
import { boolFlag, intFlag, listFlag, stringFlag } from "./args.ts";
import { versionFlag } from "./collaboration-version.ts";
import { canvasPath, collaborationRequest, tryCollaboration } from "./collaboration.ts";
import { emit, sleep } from "./shared.ts";
import type {
  Args,
  CanvasNode,
  CanvasNodeData,
  ComposeJob,
  ComposeRow,
  Ctx,
  Graph,
  WireVersion,
} from "../types.ts";

const TERMINAL_COMPOSE_STATUSES = new Set<string>(["succeeded", "failed", "canceled"]);

/** 节点当前生效的素材 id：优先 activeOutputId 指向的那一版，老节点只有 assetId。 */
function activeAssetIdOf(data?: CanvasNodeData): string {
  const activeId = data?.activeOutputId;
  const outputs = Array.isArray(data?.outputs) ? data.outputs : [];
  if (activeId !== undefined && activeId !== null) {
    const hit = outputs.find((one) => String(one.id) === String(activeId));
    if (hit?.assetId) return String(hit.assetId);
  }
  return data?.assetId ? String(data.assetId) : "";
}

/**
 * 按成片节点的上游推导素材顺序，规则与画布一致：
 * 先认 data.composeOrder（用户手动调过的段序），其余按画布位置（先上下、再左右）。
 * 只收视频节点；还没出视频的段跳过，并在返回值里报出来。
 */
export function deriveComposeAssets(
  graph: Graph,
  nodeId: string,
): { rows: ComposeRow[]; missing: number[] } {
  const nodes = graph.nodes || [];
  const composeNode = nodes.find((item) => item.id === nodeId);
  if (!composeNode) throw new APIError(-1, `画布里找不到节点 ${nodeId}`, 0);
  if (composeNode.data?.kind !== "compose")
    throw new APIError(
      -1,
      `节点 ${nodeId} 不是成片节点（data.kind=${composeNode.data?.kind || "-"}）`,
      2,
    );
  const order = Array.isArray(composeNode.data.composeOrder) ? composeNode.data.composeOrder : [];
  const rank = new Map(order.map((id, index) => [id, index]));
  const sources = (graph.edges || [])
    .filter((edge) => edge.target === nodeId)
    .map((edge) => nodes.find((item) => item.id === edge.source))
    .filter((item): item is CanvasNode => Boolean(item) && item?.data?.kind === "video");
  const positionOf = (item: CanvasNode): { x: number; y: number } => ({
    x: Number(item.position?.x ?? 0),
    y: Number(item.position?.y ?? 0),
  });
  const sorted = [...sources].sort((a, b) => {
    const ra = rank.get(a.id);
    const rb = rank.get(b.id);
    if (ra === undefined && rb === undefined) {
      const pa = positionOf(a);
      const pb = positionOf(b);
      return pa.y - pb.y || pa.x - pb.x || String(a.id).localeCompare(String(b.id));
    }
    if (ra === undefined) return 1;
    if (rb === undefined) return -1;
    return ra - rb;
  });
  const rows: ComposeRow[] = [];
  const missing: number[] = [];
  sorted.forEach((item, seq) => {
    const assetId = activeAssetIdOf(item.data);
    if (!assetId) {
      missing.push(seq);
      rows.push({ seq, node: item.id, label: item.data?.label || "视频", assetId: "" });
      return;
    }
    rows.push({ seq, node: item.id, label: item.data?.label || "视频", assetId });
  });
  return { rows, missing };
}

function formatComposeRows(rows: ComposeRow[]): string {
  if (!rows.length) return "（成片节点上没有连进来的视频）";
  const lines = ["SEQ\tASSET\tNODE\tLABEL"];
  for (const row of rows) {
    lines.push(`${row.seq}\t${row.assetId || "（还没有视频）"}\t${row.node}\t${row.label}`);
  }
  return lines.join("\n");
}

function formatComposeJob(job: ComposeJob): string {
  const lines = [`合成任务：${job.id}`, `状态：${job.status}`];
  if (job.segment_count !== undefined) lines.push(`片段：${job.segment_count}`);
  if (job.duration_ms) lines.push(`时长：${(job.duration_ms / 1000).toFixed(1)} 秒`);
  if (job.result_asset_id) lines.push(`成片素材：${job.result_asset_id}`);
  if (job.result_asset_url) lines.push(`成片地址：${job.result_asset_url}`);
  if (job.error) lines.push(`错误：${job.error}`);
  if (job.result_node) lines.push(`成片节点：${job.result_node}`);
  return lines.join("\n");
}
const usage =
  "canvas compose <画布> (--nodes 视频节点,... | --node 成片节点) --epoch E --revision N --op-id ID";

function composePath(code: string, id?: string): string {
  const base = `${canvasPath(code)}/collaboration/compose`;
  return id ? `${base}/${encodeURIComponent(id)}` : base;
}

function requireEpoch(args: Args): WireVersion {
  const epoch = versionFlag(args.flags, "epoch");
  if (epoch === undefined) throw new APIError(-1, "--epoch 需要协作会话代数", 2);
  return epoch;
}

function waitOptions(args: Args): { timeout: number; interval: number } {
  return {
    timeout: intFlag(args.flags, "timeout", 900, { min: 1, max: 86400 }),
    interval: intFlag(args.flags, "interval", 5, { min: 1, max: 3600 }),
  };
}

export async function canvasCompose(ctx: Ctx, args: Args): Promise<void> {
  const code = args.positional[0];
  if (!code) throw new APIError(-1, `用法：${usage}`, 2);
  const epoch = requireEpoch(args);
  const revision = versionFlag(args.flags, "revision");
  if (revision === undefined) throw new APIError(-1, `用法：${usage}`, 2);
  const opId = stringFlag(args.flags, "op-id", usage);
  if (!/^[a-zA-Z0-9_-]{1,64}$/.test(opId)) throw new APIError(-1, "无效的 --op-id", 2);
  const selected = listFlag(args.flags, "nodes");
  const byNode = args.flags.node !== undefined;
  if (Boolean(selected.length) === byNode) throw new APIError(-1, `用法：${usage}`, 2);
  const wait = boolFlag(args.flags, "wait");
  const options = wait ? waitOptions(args) : undefined;
  const snapshot = await tryCollaboration(ctx, code);
  if (!snapshot?.capabilities?.actions?.includes("compose"))
    throw new APIError(-1, "当前服务端暂不支持成片合成，或当前成员无成片权限", 0);
  if (snapshot.epoch !== epoch) throw new APIError(30018, "协作会话已变更", 409);
  if (byNode && snapshot.canvas.revision !== revision)
    throw new APIError(-1, "成片节点的观察版本已变化，请核对顺序或用原 --nodes 列表重试", 409);
  const rows = byNode
    ? deriveComposeAssets(snapshot.canvas.payload_json ?? {}, stringFlag(args.flags, "node", usage))
        .rows
    : [];
  if (rows.some((row) => !row.assetId))
    throw new APIError(-1, `存在未生成视频的片段：\n${formatComposeRows(rows)}`, 2);
  const nodes = byNode ? rows.map((row) => row.node) : selected;
  if (nodes.length < 1 || nodes.length > 24) throw new APIError(-1, "成片需要 1–24 段视频", 2);
  const job = await collaborationRequest<ComposeJob>(ctx, composePath(code), {
    method: "POST",
    body: { epoch, revision, opId, nodes },
  });
  const final = options ? await waitCompose(ctx, code, epoch, job.id, options, job) : job;
  const plan = byNode ? formatComposeRows(rows) : `节点顺序：${nodes.join(" → ")}`;
  emit(ctx, { job: final, nodes, opId, epoch, revision }, `${plan}\n\n${formatComposeJob(final)}`);
  if (wait && final.status !== "succeeded") process.exitCode = 1;
}

async function waitCompose(
  ctx: Ctx,
  code: string,
  epoch: WireVersion,
  id: string | number,
  options: { timeout: number; interval: number },
  initial?: ComposeJob,
): Promise<ComposeJob> {
  const deadline = Date.now() + options.timeout * 1000;
  const path = composePath(code, String(id));
  let last = initial?.status;
  if (initial && TERMINAL_COMPOSE_STATUSES.has(initial.status)) return initial;
  while (Date.now() < deadline) {
    const job = await withRetry(
      () =>
        collaborationRequest<ComposeJob>(ctx, path, {
          query: { epoch },
          signal: AbortSignal.timeout(Math.max(1, deadline - Date.now())),
        }),
      { deadline, onRetry: (err, attempt, ms) => ctx.stderr(retryNotice(err, attempt, ms)) },
    );
    if (TERMINAL_COMPOSE_STATUSES.has(job.status)) return job;
    if (last !== job.status) ctx.stderr(`成片任务 ${id}：${job.status}`);
    last = job.status;
    await sleep(Math.max(0, Math.min(options.interval * 1000, deadline - Date.now())));
  }
  throw new APIError(
    -1,
    `成片任务 ${id} 等待超时，可用 task wait ${id} --canvas ${code} --epoch ${epoch} 继续等待`,
    0,
    3,
  );
}

export async function readComposeJob(
  ctx: Ctx,
  args: Args,
  code: string,
  id: string,
  action: "get" | "wait" | "cancel",
): Promise<void> {
  const epoch = requireEpoch(args);
  const job =
    action === "wait"
      ? await waitCompose(ctx, code, epoch, id, waitOptions(args))
      : await collaborationRequest<ComposeJob>(ctx, composePath(code, id), {
          method: action === "cancel" ? "DELETE" : "GET",
          query: { epoch },
        });
  emit(ctx, job, formatComposeJob(job));
  if (action === "wait" && job.status !== "succeeded") process.exitCode = 1;
}

export async function canvasComposeStatus(ctx: Ctx, args: Args): Promise<void> {
  const code = args.positional[0];
  if (!code) throw new APIError(-1, "用法：canvas compose-status <画布> --job ID --epoch E", 2);
  return readComposeJob(
    ctx,
    args,
    code,
    stringFlag(args.flags, "job", "canvas compose-status"),
    "get",
  );
}

export async function canvasComposeCancel(ctx: Ctx, args: Args): Promise<void> {
  const code = args.positional[0];
  if (!code) throw new APIError(-1, "用法：canvas compose-cancel <画布> --job ID --epoch E", 2);
  return readComposeJob(
    ctx,
    args,
    code,
    stringFlag(args.flags, "job", "canvas compose-cancel"),
    "cancel",
  );
}
