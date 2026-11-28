import { randomUUID } from "node:crypto";

import { APIError } from "../client.ts";
import type { Args, CanvasNode, Ctx, Graph } from "../types.ts";
import { intFlag, stringFlag } from "./args.ts";
import { versionFlag } from "./collaboration-version.ts";
import { fetchCanvas } from "./canvas.ts";
import { newCollaborationPlan, requireEditor, runCollaborationPlan } from "./collaboration-plan.ts";
import {
  CollaborationError,
  getCollaboration,
  type CollaborationChange,
  type CollaborationSnapshot,
} from "./collaboration.ts";
import { askYes, emit, readJSONFile, requireInteractive } from "./shared.ts";

const referenceTypes = ["images", "videos", "audios"] as const;
const maxAssetId = 18_446_744_073_709_551_615n;

function referenceId(value: unknown): string | undefined {
  const id = typeof value === "number" && Number.isSafeInteger(value) ? String(value) : value;
  if (typeof id === "string" && /^[1-9]\d{0,19}$/.test(id) && BigInt(id) <= maxAssetId) return id;
}

function manualReferences(value: unknown) {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new APIError(-1, "参考素材输入需要 JSON 对象", 2);
  }
  return Object.fromEntries(
    Object.entries(value).map(([kind, items]) => {
      if (
        !referenceTypes.some((key) => key === kind) ||
        !Array.isArray(items) ||
        items.length > 64
      ) {
        throw new APIError(-1, "参考素材类型须为 images、videos 或 audios，每类最多 64 项", 2);
      }
      return [
        kind,
        items.map((item: unknown) => {
          if (
            !item ||
            typeof item !== "object" ||
            Object.keys(item).length !== 1 ||
            !("assetId" in item)
          ) {
            throw new APIError(-1, "参考素材每项只接受 assetId", 2);
          }
          const assetId = referenceId(item.assetId);
          if (!assetId) throw new APIError(-1, "参考素材 ID 须为正整数，长编号请使用字符串", 2);
          return { assetId };
        }),
      ];
    }),
  );
}

function currentManualReferences(node: CanvasNode) {
  const params = node.data?.params;
  if (!params || typeof params !== "object") return {};
  const fields = params as Record<string, unknown>;
  return Object.fromEntries(
    referenceTypes.flatMap((key) => {
      const items = fields[key];
      if (!Array.isArray(items) || !items.length) return [];
      const references = items.map((item: unknown) => {
        const assetId = referenceId(item);
        if (!assetId) throw new APIError(-1, "画布包含无法精确读取的参考素材 ID", 409);
        return { assetId };
      });
      return [[key, references]];
    }),
  );
}

function namedNode(graph: Graph, id: string): CanvasNode {
  const node = graph.nodes?.find((entry) => entry.id === id);
  if (!node) throw new APIError(-1, `画布上没有节点 ${id}`, 2);
  return node;
}

function position(args: Args) {
  const raw = stringFlag(args.flags, "at", "请用 --at x,y 指定节点坐标");
  const parts = raw.split(",");
  const [x, y] = parts.map(Number);
  if (
    parts.length !== 2 ||
    parts.some((part) => !part.trim()) ||
    x === undefined ||
    y === undefined ||
    ![x, y].every((coordinate) => Number.isFinite(coordinate) && Math.abs(coordinate) <= 1e6)
  ) {
    throw new APIError(-1, "--at 需要两个在 ±1000000 范围内的坐标", 2);
  }
  return { x, y };
}

async function loadEdit(ctx: Ctx, code: string, args: Args): Promise<CollaborationSnapshot> {
  const epoch = versionFlag(args.flags, "epoch");
  const revision = versionFlag(args.flags, "revision");
  if (epoch === undefined || revision === undefined) {
    throw new APIError(-1, "请先读取当前画布，再指定 --epoch 和 --revision", 2);
  }
  const snapshot = await getCollaboration(ctx, code);
  requireEditor(snapshot);
  if (snapshot.epoch !== epoch)
    throw new CollaborationError(30018, "协作会话已变更", 409, "epoch_expired");
  if (snapshot.canvas.revision !== revision)
    throw new CollaborationError(30002, "画布观察版本已变化", 409, "conflict");
  return snapshot;
}

async function confirmEdit(
  ctx: Ctx,
  snapshot: CollaborationSnapshot,
  change: CollaborationChange,
  destructive = false,
): Promise<void> {
  return commitStructure(ctx, snapshot, [change], destructive);
}

async function commitStructure(
  ctx: Ctx,
  snapshot: CollaborationSnapshot,
  changes: CollaborationChange[],
  requireBaseRevision = false,
): Promise<void> {
  const plan = await newCollaborationPlan(ctx, snapshot, changes, requireBaseRevision);
  const result = await runCollaborationPlan(ctx, plan);
  emit(ctx, result, `已确认操作 ${plan.opId}，版本 ${result.revision}`);
}

export async function canvasNodeRead(ctx: Ctx, args: Args, action: "list" | "get"): Promise<void> {
  const [code, id] = args.positional;
  if (!code || (action === "get" && !id))
    throw new APIError(-1, "用法：canvas node list|get <画布> [节点]", 2);
  const canvas = await fetchCanvas(ctx, code);
  const graph = canvas.payload_json ?? {};
  const version = {
    revision: String(canvas.revision),
    epoch: canvas.collaboration_epoch?.toString(),
  };
  if (action === "get" && id) {
    const node = namedNode(graph, id);
    return emit(ctx, { ...version, node }, JSON.stringify(node, null, 2));
  }
  const kind =
    args.flags.kind === undefined ? undefined : stringFlag(args.flags, "kind", "--kind <节点类型>");
  const group =
    args.flags.group === undefined
      ? undefined
      : stringFlag(args.flags, "group", "--group <组编号>");
  const keyword =
    args.flags.keyword === undefined
      ? undefined
      : stringFlag(args.flags, "keyword", "--keyword <节点名称>");
  const nodes = (graph.nodes ?? []).filter(
    (node) =>
      (!kind || kind === (node.type === "group" ? "group" : node.data?.kind)) &&
      (!group || node.parentId === group) &&
      (!keyword || node.data?.label?.toLowerCase().includes(keyword.toLowerCase())),
  );
  emit(
    ctx,
    { ...version, nodes },
    nodes
      .map((node) => `${node.id}\t${node.data?.kind ?? node.type ?? ""}\t${node.data?.label ?? ""}`)
      .join("\n") || "没有符合条件的节点",
  );
}

export async function canvasNodeMove(ctx: Ctx, args: Args): Promise<void> {
  const [code, id] = args.positional;
  if (!code || !id)
    throw new APIError(
      -1,
      "用法：canvas node move <画布> <节点> --at x,y --epoch N --revision N",
      2,
    );
  const snapshot = await loadEdit(ctx, code, args);
  const graph = snapshot.canvas.payload_json ?? {};
  const node = namedNode(graph, id);
  const fields: Record<string, unknown> = { position: position(args) };
  if (args.flags.group !== undefined) {
    if (node.type === "group") throw new APIError(-1, "分组不能嵌套", 2);
    const group = stringFlag(args.flags, "group", "--group <分组|none>");
    if (group !== "none" && namedNode(graph, group).type !== "group")
      throw new APIError(-1, "目标节点不是分组", 2);
    fields.parentId = group === "none" ? null : group;
  }
  return confirmEdit(ctx, snapshot, { kind: node.type === "group" ? "group" : "node", id, fields });
}

function canvasPosition(node: CanvasNode | undefined) {
  return { x: node?.position?.x ?? 0, y: node?.position?.y ?? 0 };
}

function nodeSize(value: unknown, fallback: number) {
  return typeof value === "number" && Number.isFinite(value) && value > 0 ? value : fallback;
}

function arrangeNodes(graph: Graph, ids: string[], mode: "row" | "column" | "grid") {
  const entries = ids.map((id) => {
    const node = namedNode(graph, id);
    if (node.type === "group") throw new APIError(-1, "请指定组内要整理的普通节点", 2);
    const local = canvasPosition(node);
    const parent = canvasPosition(graph.nodes?.find((item) => item.id === node.parentId));
    return {
      id,
      parent,
      x: local.x + parent.x,
      y: local.y + parent.y,
      width: nodeSize(node.width, 576),
      height: nodeSize(node.height, 324),
    };
  });
  const left = Math.min(...entries.map(({ x }) => x));
  const top = Math.min(...entries.map(({ y }) => y));
  entries.sort((a, b) => (mode === "row" ? a.x - b.x || a.y - b.y : a.y - b.y || a.x - b.x));
  const columns =
    mode === "row" ? entries.length : mode === "column" ? 1 : Math.ceil(Math.sqrt(entries.length));
  const widths: number[] = [];
  const heights: number[] = [];
  const placements = entries.map((node, index) => {
    const col = index % columns;
    const row = Math.floor(index / columns);
    widths[col] = Math.max(widths[col] ?? 0, node.width);
    heights[row] = Math.max(heights[row] ?? 0, node.height);
    return { ...node, col, row };
  });
  const starts = (sizes: number[], start: number, gap: number) =>
    sizes.map((size) => {
      const coordinate = start;
      start += size + gap;
      return coordinate;
    });
  const xs = starts(widths, left, 120);
  const ys = starts(heights, top, 80);
  return placements.map(({ id, parent, col, row }): CollaborationChange => ({
    kind: "node",
    id,
    fields: { position: { x: xs[col]! - parent.x, y: ys[row]! - parent.y } },
  }));
}

export async function canvasNodeArrange(ctx: Ctx, args: Args): Promise<void> {
  const [code, ...selection] = args.positional;
  const ids = [...new Set(selection)];
  const usage =
    "用法：canvas node arrange <画布> <节点...> --mode row|column|grid --epoch N --revision N";
  if (!code || ids.length < 2 || ids.length > 100) throw new APIError(-1, usage, 2);
  const mode = stringFlag(args.flags, "mode", usage);
  if (mode !== "row" && mode !== "column" && mode !== "grid") throw new APIError(-1, usage, 2);
  const snapshot = await loadEdit(ctx, code, args);
  return commitStructure(
    ctx,
    snapshot,
    arrangeNodes(snapshot.canvas.payload_json ?? {}, ids, mode),
    true,
  );
}

function duplicateData(source: CanvasNode, graph: Graph) {
  const { kind, text, prompt, model, params } = source.data ?? {};
  if (!kind || !["script", "image", "video", "audio"].includes(kind)) {
    throw new APIError(-1, "复制支持文本、图片、视频和音频节点", 2);
  }
  const labels = new Set(graph.nodes?.map((node) => node.data?.label));
  const base = (source.data?.label || kind).replace(/\s副本[一二三四五六七八九十\d]*$/, "");
  let label = `${[...base].slice(0, 37).join("")} 副本`;
  for (let number = 2; labels.has(label); number++) {
    const suffix = ` 副本${number}`;
    label = `${[...base].slice(0, 40 - suffix.length).join("")}${suffix}`;
  }
  const options = Object.fromEntries(
    Object.entries(params ?? {}).filter(([key, value]) => {
      const name = key.toLowerCase();
      if (
        Object.hasOwn(source.data?.paramAssets ?? {}, key) ||
        [
          "prompt",
          "images",
          "videos",
          "audios",
          "src",
          "__proto__",
          "constructor",
          "prototype",
        ].includes(name) ||
        /url$|assetid|storagekey|secret|token/.test(name)
      )
        return false;
      return (
        typeof value === "boolean" ||
        typeof value === "number" ||
        (typeof value === "string" &&
          !value.includes("/files/") &&
          !/^(asset:\/\/|blob:|data:)/.test(value))
      );
    }),
  );
  const references = currentManualReferences(source);
  return {
    kind,
    label,
    text,
    prompt,
    model,
    ...(Object.keys(options).length ? { params: options } : {}),
    ...(Object.keys(references).length ? { manualReferences: references } : {}),
  };
}

function duplicatePosition(graph: Graph, source: CanvasNode) {
  const nodes = new Map((graph.nodes ?? []).map((node) => [node.id, node]));
  const bounds = (node: CanvasNode) => {
    const origin = canvasPosition(node);
    const parent = canvasPosition(
      typeof node.parentId === "string" ? nodes.get(node.parentId) : undefined,
    );
    return {
      x: origin.x + parent.x,
      y: origin.y + parent.y,
      width: nodeSize(node.width, 280),
      height: nodeSize(node.height, 220),
    };
  };
  const origin = bounds(source);
  const taken = Array.from(nodes.values(), bounds);
  for (let index = 0; index <= 200; index++) {
    const x = origin.x + ((index % 4) + 1) * (origin.width + 40);
    const y = origin.y + Math.floor(index / 4) * (origin.height + 40);
    if (
      index === 200 ||
      !taken.some(
        (node) =>
          x < node.x + node.width + 40 &&
          node.x < x + origin.width + 40 &&
          y < node.y + node.height + 40 &&
          node.y < y + origin.height + 40,
      )
    ) {
      return { x, y };
    }
  }
}

export async function canvasNodeDuplicate(ctx: Ctx, args: Args): Promise<void> {
  const [code, sourceId] = args.positional;
  const usage = "用法：canvas node duplicate <画布> <节点> --epoch N --revision N [--at x,y]";
  if (!code || !sourceId || args.positional.length !== 2) throw new APIError(-1, usage, 2);
  const snapshot = await loadEdit(ctx, code, args);
  const graph = snapshot.canvas.payload_json ?? {};
  const source = namedNode(graph, sourceId);
  const id = randomUUID();
  const data = duplicateData(source, graph);
  const at = args.flags.at === undefined ? duplicatePosition(graph, source) : position(args);
  const changes: CollaborationChange[] = [
    { kind: "node", id, action: "create", value: { position: at, data } },
  ];
  for (const edge of graph.edges ?? []) {
    if (edge.target !== source.id) continue;
    const { source: upstream, sourceHandle, targetHandle } = edge;
    changes.push({
      kind: "edge",
      id: randomUUID(),
      action: "create",
      value: { source: upstream, target: id, sourceHandle, targetHandle },
    });
  }
  return commitStructure(ctx, snapshot, changes, true);
}

export async function canvasNodeRefs(ctx: Ctx, args: Args): Promise<void> {
  const [code, id] = args.positional;
  const usage = "用法：canvas node refs <画布> <节点> [--input 文件 --epoch N --revision N]";
  if (!code || !id || args.positional.length !== 2) throw new APIError(-1, usage, 2);
  const writing = args.flags.input !== undefined;
  const snapshot = writing ? await loadEdit(ctx, code, args) : await getCollaboration(ctx, code);
  const graph = snapshot.canvas.payload_json ?? {};
  const node = namedNode(graph, id);
  if (node.type === "group") throw new APIError(-1, "请选择普通节点", 2);
  if (writing) {
    const manual = manualReferences(readJSONFile(stringFlag(args.flags, "input", usage)));
    return confirmEdit(ctx, snapshot, {
      kind: "node",
      id,
      fields: { "data.manualReferences": manual },
    });
  }
  const nodes = new Map((graph.nodes ?? []).map((item) => [item.id, item.data]));
  const linked = (graph.edges ?? []).flatMap((edge) => {
    if (edge.target !== id || !nodes.has(edge.source)) return [];
    const data = nodes.get(edge.source);
    return [{ edgeId: edge.id, nodeId: edge.source, kind: data?.kind, label: data?.label }];
  });
  const result = {
    epoch: String(snapshot.epoch),
    revision: String(snapshot.canvas.revision),
    linked,
    manual: currentManualReferences(node),
  };
  emit(ctx, result, JSON.stringify(result, null, 2));
}

export async function canvasEdge(ctx: Ctx, args: Args): Promise<void> {
  const [action, code, id, source, target] = args.positional;
  if (!code || !id || (action !== "add" && action !== "rm")) {
    throw new APIError(
      -1,
      "用法：canvas edge add|rm <画布> <连线ID> [来源节点 目标节点] --epoch N --revision N",
      2,
    );
  }
  const snapshot = await loadEdit(ctx, code, args);
  const graph = snapshot.canvas.payload_json ?? {};
  if (action === "rm") {
    const edge = graph.edges?.find((entry) => entry.id === id);
    if (!edge) throw new APIError(-1, `画布上没有连线 ${id}`, 2);
    requireInteractive(ctx, "删除连线", "请在终端核对连线与端点。");
    ctx.stderr(`将删除连线 ${id}（${edge.source} → ${edge.target}）。`);
    await askYes(ctx, "输入 yes 确认删除：", "已取消删除连线");
    return confirmEdit(ctx, snapshot, { kind: "edge", id, action: "delete" }, true);
  }
  if (
    !source ||
    !target ||
    source === target ||
    [source, target].some((endpoint) => namedNode(graph, endpoint).type === "group")
  ) {
    throw new APIError(-1, "连线需要两个不同的普通节点", 2);
  }
  const value: Record<string, unknown> = { source, target };
  for (const [name, flag] of [
    ["sourceHandle", "source-handle"],
    ["targetHandle", "target-handle"],
  ] as const) {
    if (args.flags[flag] !== undefined)
      value[name] = stringFlag(args.flags, flag, `--${flag} <端口>`);
  }
  return confirmEdit(ctx, snapshot, { kind: "edge", id, action: "create", value });
}

export async function canvasGroup(ctx: Ctx, args: Args): Promise<void> {
  const [action, code, id] = args.positional;
  if (!code || !id || (action !== "add" && action !== "set" && action !== "rm")) {
    throw new APIError(
      -1,
      "用法：canvas group add|set|rm <画布> <分组ID> --epoch N --revision N",
      2,
    );
  }
  const snapshot = await loadEdit(ctx, code, args);
  const graph = snapshot.canvas.payload_json ?? {};
  if (action !== "add" && namedNode(graph, id).type !== "group")
    throw new APIError(-1, "目标不是分组", 2);
  if (action === "rm") {
    requireInteractive(ctx, "删除分组", "请在终端核对分组和成员。");
    const members = (graph.nodes ?? []).filter((node) => node.parentId === id);
    ctx.stderr(`将删除分组 ${id}，组内 ${members.length} 个节点会脱离分组并保留。`);
    await askYes(ctx, "输入 yes 确认删除：", "已取消删除分组");
    return confirmEdit(ctx, snapshot, { kind: "group", id, action: "delete" }, true);
  }
  const fields: Record<string, unknown> = {};
  if (args.flags.at !== undefined) fields.position = position(args);
  for (const dimension of ["width", "height"]) {
    const size = intFlag(args.flags, dimension, undefined, { min: 0, max: 1e6 });
    if (size !== undefined) fields[dimension] = size;
  }
  if (action === "add") {
    fields.position ??= { x: 0, y: 0 };
    fields.data = {
      label:
        args.flags.label === undefined
          ? "分组"
          : stringFlag(args.flags, "label", "--label <分组名称>"),
    };
    return confirmEdit(ctx, snapshot, { kind: "group", id, action: "create", value: fields });
  }
  if (args.flags.label !== undefined)
    fields["data.label"] = stringFlag(args.flags, "label", "--label <分组名称>");
  if (!Object.keys(fields).length)
    throw new APIError(-1, "请指定需要更新的分组名称、位置或大小", 2);
  return confirmEdit(ctx, snapshot, { kind: "group", id, fields });
}
