// 节点级编辑：只提交差异，且必须由人在终端确认。

import { randomUUID } from "node:crypto";

import { APIError, request } from "../client.ts";
import { stringFlag } from "./args.ts";
import { fetchCanvas } from "./canvas.ts";
import { newCollaborationPlan, runCollaborationPlan } from "./collaboration-plan.ts";
import { CollaborationError, type CollaborationChange } from "./collaboration.ts";
import { askYes, baseOf, emit, requireInteractive, requireToken } from "./shared.ts";
import type { Args, CanvasNode, Ctx, FlagValue, Graph, OpsResult } from "../types.ts";

/** 命令行支持新建的节点种类。其余种类要填内嵌结构，适合在画面上建。 */
const CLI_NODE_KINDS = ["script", "image", "video", "audio"];

const KIND_NAMES: Record<string, string> = {
  script: "文本",
  image: "图片",
  video: "视频",
  audio: "音频",
  storyboard: "分镜表",
  compose: "成片",
  character: "角色",
  director: "导演台",
};

export const kindName = (kind?: string): string => (kind ? KIND_NAMES[kind] || kind : "节点");

/** 解析 `--at 100,200`。 */
function parsePoint(raw: FlagValue): { x: number; y: number } {
  const parts = String(raw).split(",");
  const x = Number(parts[0]);
  const y = Number(parts[1]);
  if (parts.length !== 2 || !Number.isFinite(x) || !Number.isFinite(y))
    throw new APIError(-1, "--at 需要 x,y 两个数字，例如 --at 100,200", 2);
  return { x, y };
}

/** 按 id 找节点；找不到直接报错，别把无效 id 提交给服务端。 */
function nodeById(graph: Graph, id: string): CanvasNode {
  const node = (graph.nodes || []).find((n) => n && n.id === id);
  if (!node) throw new APIError(-1, `画布上没有节点 ${id}`, 2);
  return node;
}

/** 把服务端返回的差异排成人能读的几行。 */
function formatChanges(res: OpsResult): string {
  const lines = [`已保存，版本 ${res.revision}`];
  for (const c of res.changes || []) {
    const mark = c.op === "create" ? "+" : c.op === "update" ? "~" : "-";
    const what = c.kind === "edge" ? "连线" : "节点";
    const fields = c.op === "update" && c.after ? `（${Object.keys(c.after).join("、")}）` : "";
    lines.push(`${mark} ${what} ${c.id}${fields}`);
  }
  if (lines.length === 1) lines.push("（没有实际改动）");
  for (const w of res.warnings || []) lines.push(`提醒：${w}`);
  return lines.join("\n");
}

/** 节点级改动要提交的内容：ops 与 deleteNodeIds 至少有一个，外加给人看的说明。 */
interface NodeOps {
  ops?: Array<Record<string, unknown>>;
  deleteNodeIds?: string[];
  changes: CollaborationChange[];
  lines: string[];
}

async function commitNodeOps(
  ctx: Ctx,
  code: string,
  build: (graph: Graph) => NodeOps,
): Promise<void> {
  requireInteractive(
    ctx,
    "节点级修改",
    "协作批量编辑请用 canvas plan + canvas apply。个人画布请用 canvas graph + canvas save --revision。",
  );

  const canvas = await fetchCanvas(ctx, code);
  const { collaboration_role: role, collaboration_epoch: epoch } = canvas;
  if (role === "viewer") {
    throw new CollaborationError(-1, "仅查看成员不能修改画布", 403, "permission_denied");
  }
  const graph: Graph = canvas.payload_json || { nodes: [], edges: [] };
  const { ops, deleteNodeIds, changes, lines } = build(graph);
  if (canvas.collaboration_active && (!role || !epoch)) {
    throw new CollaborationError(30018, "请重新读取当前协作会话", 409, "epoch_expired");
  }
  const plan =
    canvas.collaboration_active && role && epoch
      ? await newCollaborationPlan(
          ctx,
          { role, epoch, canvas },
          changes,
          Boolean(deleteNodeIds?.length),
        )
      : null;

  ctx.stderr(`画布：${canvas.title}（${code}）当前版本 ${canvas.revision}`);
  ctx.stderr("将提交以下改动：");
  for (const line of lines) ctx.stderr(`  ${line}`);
  await askYes(ctx, "确认执行？输入 yes 回车，其它任意输入取消：", "已取消，画布未修改");
  if (plan) {
    const commit = await runCollaborationPlan(ctx, plan);
    emit(ctx, commit, `已确认操作 ${plan.opId}，版本 ${commit.revision}`);
    return;
  }

  const body: Record<string, unknown> = { revision: canvas.revision };
  if (ops?.length) body.ops = ops;
  if (deleteNodeIds?.length) body.deleteNodeIds = deleteNodeIds;
  const res = await request<OpsResult>({
    base: baseOf(ctx),
    token: requireToken(ctx),
    method: "POST",
    path: `/canvas/${encodeURIComponent(code)}/ops`,
    body,
    fetchImpl: ctx.fetchImpl,
  });
  emit(ctx, res, formatChanges(res));
}

/** canvas node add：新建一个普通节点。 */
export async function canvasNodeAdd(ctx: Ctx, args: Args): Promise<void> {
  const code = args.positional[0];
  const usage =
    "canvas node add <画布编号> --kind <种类> [--label 标题] [--text 正文] [--prompt 提示词] [--model 模型] [--at x,y]";
  if (!code) throw new APIError(-1, `用法：${usage}`, 2);
  const kind = stringFlag(args.flags, "kind", usage);
  if (!CLI_NODE_KINDS.includes(kind))
    throw new APIError(
      -1,
      `--kind 只能是 ${CLI_NODE_KINDS.map((k) => `${k}（${kindName(k)}）`).join(" / ")}；` +
        "角色卡、分镜表、成片、导演台要在画面上创建",
      2,
    );

  const label = args.flags.label === undefined ? "" : stringFlag(args.flags, "label", usage);
  const model = args.flags.model === undefined ? undefined : stringFlag(args.flags, "model", usage);
  const at = args.flags.at === undefined ? undefined : parsePoint(args.flags.at);
  if (kind === "script" && args.flags.prompt !== undefined)
    throw new APIError(-1, "文本节点的正文用 --text 传（服务端写进节点正文），不要用 --prompt", 2);
  if (kind !== "script" && args.flags.text !== undefined)
    throw new APIError(-1, `--text 只用于文本节点；${kindName(kind)}节点请用 --prompt`, 2);
  const content =
    kind === "script"
      ? args.flags.text === undefined
        ? undefined
        : stringFlag(args.flags, "text", usage)
      : args.flags.prompt === undefined
        ? undefined
        : stringFlag(args.flags, "prompt", usage);

  await commitNodeOps(ctx, code, () => {
    const op: Record<string, unknown> = { op: "create_node", tempId: "n1", kind };
    if (label) op.label = label;
    if (content !== undefined) op.prompt = content;
    if (model !== undefined) op.model = model;
    if (at) op.position = at;
    const lines = [`+ 新建${kindName(kind)}节点${label ? `「${label}」` : ""}`];
    if (content !== undefined)
      lines.push(`  ${kind === "script" ? "正文" : "提示词"} ${content.length} 字`);
    if (model !== undefined) lines.push(`  模型 ${model}`);
    if (at) lines.push(`  位置 ${at.x},${at.y}`);
    const data: Record<string, unknown> = { kind, label: label || kindName(kind) };
    if (content !== undefined) data[kind === "script" ? "text" : "prompt"] = content;
    if (model !== undefined) data.model = model;
    const changes: CollaborationChange[] = [
      {
        kind: "node",
        id: randomUUID(),
        action: "create",
        value: { position: at ?? { x: 0, y: 0 }, data },
      },
    ];
    return { ops: [op], changes, lines };
  });
}

/** canvas node set：改标题、正文、提示词或模型。 */
export async function canvasNodeSet(ctx: Ctx, args: Args): Promise<void> {
  const code = args.positional[0];
  const id = args.positional[1];
  const usage =
    "canvas node set <画布编号> <节点id> [--label 标题] [--text 正文] [--prompt 提示词] [--model 模型]";
  if (!code || !id) throw new APIError(-1, `用法：${usage}`, 2);
  if (!["label", "text", "prompt", "model"].some((k) => args.flags[k] !== undefined))
    throw new APIError(-1, "至少给一个要改的字段：--label / --text / --prompt / --model", 2);

  await commitNodeOps(ctx, code, (graph) => {
    const node = nodeById(graph, id);
    const kind = node.data?.kind;
    const op: Record<string, unknown> = { op: "update_node", id };
    const fields: Record<string, unknown> = {};
    const lines = [
      `~ 修改${kindName(kind)}节点 ${id}${node.data?.label ? `「${node.data.label}」` : ""}`,
    ];

    if (args.flags.label !== undefined) {
      const value = stringFlag(args.flags, "label", usage);
      op.label = value;
      fields["data.label"] = value;
      lines.push(`  标题 → ${value}`);
    }
    if (args.flags.text !== undefined) {
      if (kind !== "script")
        throw new APIError(-1, `--text 只用于文本节点；${kindName(kind)}节点请用 --prompt`, 2);
      const value = stringFlag(args.flags, "text", usage);
      op.prompt = value;
      fields["data.text"] = value;
      lines.push(`  正文 → ${value.length} 字`);
    }
    if (args.flags.prompt !== undefined) {
      if (kind === "script") throw new APIError(-1, "文本节点的正文请用 --text", 2);
      const value = stringFlag(args.flags, "prompt", usage);
      op.prompt = value;
      fields["data.prompt"] = value;
      lines.push(`  提示词 → ${value.length} 字`);
    }
    if (args.flags.model !== undefined) {
      const value = stringFlag(args.flags, "model", usage);
      op.model = value;
      fields["data.model"] = value;
      lines.push(`  模型 → ${value}`);
    }
    return { ops: [op], changes: [{ kind: "node", id, fields }], lines };
  });
}

/** canvas node rm：删节点，连带清理连线、分镜挂接与成片顺序（由服务端统一处理）。 */
export async function canvasNodeRm(ctx: Ctx, args: Args): Promise<void> {
  const code = args.positional[0];
  const ids = [...new Set(args.positional.slice(1))];
  if (!code || !ids.length)
    throw new APIError(-1, "用法：canvas node rm <画布编号> <节点id> [更多节点id...]", 2);

  await commitNodeOps(ctx, code, (graph) => {
    const drop = new Set(ids);
    const lines: string[] = [];
    for (const id of ids) {
      const node = nodeById(graph, id);
      if (node.type === "group")
        throw new APIError(-1, `不能删组 ${id}：先把成员移出组，或在画面上删除`, 2);
      lines.push(
        `- 删除${kindName(node.data?.kind)}节点 ${id}${node.data?.label ? `「${node.data.label}」` : ""}`,
      );
    }
    const edges = (graph.edges || []).filter((e) => drop.has(e.source) || drop.has(e.target));
    if (edges.length) lines.push(`  同时删除 ${edges.length} 条连线`);
    let refs = 0;
    for (const n of graph.nodes || []) {
      const shots = n?.data?.storyboard?.shots;
      if (Array.isArray(shots))
        for (const shot of shots) {
          const video = shot?.videoNodeId;
          const image = shot?.imageNodeId;
          if (video && drop.has(video)) refs++;
          if (image && drop.has(image)) refs++;
        }
      const order = n?.data?.composeOrder;
      if (Array.isArray(order)) for (const v of order) if (drop.has(String(v))) refs++;
    }
    if (refs) lines.push(`  同时清掉 ${refs} 处分镜 / 成片引用`);
    return {
      deleteNodeIds: ids,
      changes: ids.map((id) => ({ kind: "node", id, action: "delete" })),
      lines,
    };
  });
}
