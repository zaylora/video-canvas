import type { CanvasVersion } from "@/utils/canvas/collaboration-version";

import type { CanvasGraphDto } from "@/api/canvas/type";
import type { CanvasNodeData } from "@/types";
import { activeOutputIdOf, readOutputs, selectOutput } from "./outputs";

/** 一个服务端确认或可提交的记录变更。 */
export type CollaborationChange = {
  /** 记录类型。 */
  kind: "node" | "group" | "edge";
  /** 稳定的记录 ID。 */
  id: string;
  /** 缺省为字段编辑。 */
  action?: "create" | "delete";
  /** 新建记录的内容。 */
  value?: Record<string, unknown>;
  /** 按语义字段更新的值。 */
  fields?: Record<string, unknown>;
  /** 此字段的独立确认版本，例如手势起点或本地前序提交的版本。 */
  fieldBases?: Record<string, CanvasVersion>;
  /** 显式删除所依赖的记录版本。 */
  recordBase?: CanvasVersion;
  /** 本人的确认收据中，服务端实际修改前的记录版本。 */
  previousRevision?: CanvasVersion;
};

const commonFields = ["position", "data.label"];
const outputField = "data.activeOutputId";
const referenceField = "data.manualReferences";
const referenceKinds = { images: "image", videos: "video", audios: "audio" } as const;
type ReferenceKey = keyof typeof referenceKinds;
type ManualReferences = Partial<Record<ReferenceKey, { assetId: string; url?: string }[]>>;
type ReferenceData = Pick<CanvasNodeData, "params" | "paramAssets">;
const nodeFields = [
  ...commonFields,
  "data.prompt",
  "data.model",
  "data.params",
  referenceField,
  "data.text",
  "parentId",
];
const groupFields = [...commonFields, "width", "height", "data.color", "data.labelColor"];
const structuredFields = {
  character: "data.character",
  director: "data.scene",
  storyboard: "data.storyboard",
} as const;
type GraphNode = CanvasGraphDto["nodes"][number];
type GraphEdge = CanvasGraphDto["edges"][number];

function collaborationKind(node: GraphNode) {
  return node.type === "group" ? "group" : "node";
}

function structuredField(node: GraphNode) {
  if (node.type !== "canvas") return undefined;
  return structuredFields[node.data.kind as keyof typeof structuredFields];
}

function editableFields(node: GraphNode) {
  if (node.type === "group") return groupFields;
  const field = structuredField(node);
  return field ? [...nodeFields, field] : nodeFields;
}

export function canonicalStructuredValue(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(canonicalStructuredValue);
  if (value === null || typeof value !== "object") return value;
  return Object.fromEntries(
    Object.entries(value)
      .filter(([key]) => key !== "src" && key !== "url")
      .sort(([left], [right]) => left.localeCompare(right))
      .map(([key, child]) => [key, canonicalStructuredValue(child)]),
  );
}

/** 为本地草稿恢复媒体预览，提交仍使用仅含素材 ID 的字段。 */
export function collaborationPreviewChanges(
  changes: CollaborationChange[],
  local: Map<string, GraphNode>,
): CollaborationChange[] {
  return changes.map((change) => {
    const node = local.get(change.id);
    if (node?.type !== "canvas" || change.kind !== "node") return change;
    const field = structuredField(node);
    const key = field?.slice(5) as "character" | "storyboard" | "scene" | undefined;
    const preview: Record<string, unknown> = {};
    if (key && field && (change.action === "create" || change.fields?.[field] !== undefined))
      preview[field] = node.data[key];
    if (change.action === "create" || change.fields?.[referenceField] !== undefined)
      preview[referenceField] = manualReferences(node.data, true);
    if (change.action === "create") {
      const data = Object.fromEntries(
        Object.entries(preview).map(([name, value]) => [name.slice(5), value]),
      );
      return {
        ...change,
        value: { ...change.value, data: { ...(change.value?.data as object), ...data } },
      };
    }
    return Object.keys(preview).length
      ? { ...change, fields: { ...change.fields, ...preview } }
      : change;
  });
}

/** 只比较协作协议支持的内容字段。视角、任务与素材结果由各自的链路处理。 */
export function diffCollaborationGraph(
  before: CanvasGraphDto,
  after: CanvasGraphDto,
): CollaborationChange[] {
  const changes: CollaborationChange[] = [];
  const remaining = new Map(before.nodes.map((node) => [node.id, node]));
  for (const node of after.nodes) {
    const previous = remaining.get(node.id);
    if (previous) remaining.delete(node.id);
  }
  for (const node of remaining.values())
    changes.push({ kind: collaborationKind(node), id: node.id, action: "delete" });
  const previousNodes = new Map(before.nodes.map((node) => [node.id, node]));
  for (const node of after.nodes) {
    const previous = previousNodes.get(node.id);
    if (previous === node) continue;
    const kind = collaborationKind(node);
    if (!previous) {
      changes.push({ kind, id: node.id, action: "create", value: newCollaborationNode(node) });
      continue;
    }
    const fields = changedNodeFields(previous, node);
    if (Object.keys(fields).length > 0) changes.push({ kind, id: node.id, fields });
  }
  diffCollaborationEdges(before.edges, after.edges, changes);
  return changes;
}

function changedNodeFields(before: GraphNode, after: GraphNode) {
  const fields: Record<string, unknown> = {};
  if (before.type === "canvas" && after.type === "canvas") {
    const output = after.data.activeOutputId;
    const asset = after.data.assetId;
    if (
      output &&
      output !== activeOutputIdOf(before.data) &&
      readOutputs(before.data).some(({ id }) => id === output)
    ) {
      fields[outputField] = output;
    } else if (asset && asset !== before.data.assetId) {
      fields["data.assetId"] = asset;
    }
  }
  for (const field of editableFields(after)) {
    if (
      field === "data.params" &&
      before.type === "canvas" &&
      after.type === "canvas" &&
      before.data.params === after.data.params &&
      before.data.paramAssets === after.data.paramAssets
    )
      continue;
    const [previous, next] =
      field === "data.prompt"
        ? comparePrompts(before, after)
        : [readField(before, field), readField(after, field)];
    if (previous === next || JSON.stringify(previous) === JSON.stringify(next)) continue;
    if (next !== undefined && next !== null) fields[field] = next;
    else if (field === "parentId") fields[field] = null;
    else if (field === "data.prompt" || field === "data.text") fields[field] = "";
  }
  return fields;
}

function comparePrompts(before: GraphNode, after: GraphNode) {
  if (before.type !== "canvas" || after.type !== "canvas") return [];
  const previous = before.data.params?.prompt;
  const next = after.data.params?.prompt;
  if (previous !== next && typeof next === "string") return [previous ?? before.data.prompt, next];
  return [before.data.prompt, after.data.prompt];
}

function newCollaborationNode(node: GraphNode) {
  const value: Record<string, unknown> = { position: node.position };
  const data: Record<string, unknown> = node.type === "canvas" ? { kind: node.data.kind } : {};
  if (node.type === "canvas" && node.data.assetId) data.assetId = node.data.assetId;
  if (node.type === "canvas" && node.data.outpaintFrom) data.outpaintFrom = node.data.outpaintFrom;
  for (const field of editableFields(node)) {
    const prompt =
      node.type === "canvas" && field === "data.prompt" ? node.data.params?.prompt : undefined;
    const next = typeof prompt === "string" ? prompt : readField(node, field);
    if (next === undefined || next === null) continue;
    if (
      (field === "data.params" || field === referenceField) &&
      Object.keys(next as Record<string, unknown>).length === 0
    )
      continue;
    if (field.startsWith("data.")) data[field.slice(5)] = next;
    else value[field] = next;
  }
  return { ...value, data };
}

function readField(node: GraphNode, field: string) {
  if (node.type === "canvas" && field === referenceField) return manualReferences(node.data);
  if (node.type === "canvas" && field === "data.params")
    return Object.fromEntries(
      Object.entries(node.data.params ?? {})
        .filter(([name, value]) => isEditableOption(node, name, value))
        .sort(([left], [right]) => left.localeCompare(right)),
    );
  const record = field.startsWith("data.") ? node.data : node;
  const key = field.startsWith("data.") ? field.slice(5) : field;
  const value = (record as unknown as Record<string, unknown>)[key];
  return field === structuredField(node) ? canonicalStructuredValue(value) : value;
}

function manualReferences(data: ReferenceData, preview = false): ManualReferences {
  const result: ManualReferences = {};
  for (const key of Object.keys(referenceKinds) as ReferenceKey[]) {
    const ids = data.params?.[key];
    if (!Array.isArray(ids)) continue;
    for (const id of ids) {
      if ((typeof id !== "string" && typeof id !== "number") || !/^[1-9]\d*$/.test(String(id)))
        continue;
      const assetId = String(id);
      const url = preview ? data.paramAssets?.[assetId]?.url : undefined;
      (result[key] ??= []).push({ assetId, ...(url ? { url } : {}) });
    }
  }
  return result;
}

function applyManualReferences(data: ReferenceData, references: ManualReferences) {
  const params = { ...data.params };
  const paramAssets = { ...data.paramAssets };
  const previous = manualReferences(data);
  for (const key of Object.keys(referenceKinds) as ReferenceKey[]) {
    for (const { assetId } of previous[key] ?? []) delete paramAssets[assetId];
    delete params[key];
    const assets = references[key] ?? [];
    if (assets.length) params[key] = assets.map(({ assetId }) => assetId);
    for (const { assetId, url } of assets) {
      const asset = data.paramAssets?.[assetId];
      if (url) paramAssets[assetId] = { ...asset, url, mediaType: referenceKinds[key] };
      else if (asset) paramAssets[assetId] = asset;
    }
  }
  return { params, paramAssets };
}

const reservedOptions = new Set([
  "prompt",
  "images",
  "videos",
  "audios",
  "src",
  "__proto__",
  "constructor",
  "prototype",
]);

function isEditableOption(
  node: Extract<GraphNode, { type: "canvas" }>,
  key: string,
  value: unknown,
) {
  const name = key.toLowerCase();
  if (
    !key ||
    [...key].length > 128 ||
    key.includes("\0") ||
    Object.hasOwn(node.data.paramAssets ?? {}, key) ||
    reservedOptions.has(name) ||
    name.endsWith("url") ||
    /assetid|storagekey|secret|token/.test(name)
  )
    return false;
  if (typeof value === "number") return Number.isFinite(value);
  if (typeof value === "boolean") return true;
  return (
    typeof value === "string" &&
    !value.includes("\0") &&
    !value.includes("/files/") &&
    !/^(?:asset:\/\/|blob:|data:)/.test(value)
  );
}

/** 读取冲突比较使用的字段起点。 */
export function collaborationFieldValue(node: GraphNode, field: string) {
  if (node.type === "canvas" && field === "data.prompt")
    return node.data.params?.prompt ?? node.data.prompt;
  if (node.type === "canvas" && field === outputField) return activeOutputIdOf(node.data);
  return readField(node, field);
}

function diffCollaborationEdges(
  before: GraphEdge[],
  after: GraphEdge[],
  changes: CollaborationChange[],
) {
  if (before === after) return;
  const oldIds = new Set(before.map((edge) => edge.id));
  const newIds = new Set(after.map((edge) => edge.id));
  for (const edge of before)
    if (!newIds.has(edge.id)) changes.push({ kind: "edge", id: edge.id, action: "delete" });
  for (const { id, ...value } of after)
    if (!oldIds.has(id)) changes.push({ kind: "edge", id, action: "create", value });
}

/** 应用一批增量，保留无变更记录和个人视角的引用。 */
export function applyCollaborationChanges(
  graph: CanvasGraphDto,
  changes: CollaborationChange[],
): CanvasGraphDto {
  if (changes.length === 0) return graph;
  let nodes: Map<string, GraphNode> | null = null;
  let edges: Map<string, GraphEdge> | null = null;
  for (const change of changes) {
    if (change.kind === "edge") {
      if (!change.action) continue;
      edges ??= new Map(graph.edges.map((edge) => [edge.id, edge]));
      if (change.action === "delete") edges.delete(change.id);
      else edges.set(change.id, { ...change.value, id: change.id } as GraphEdge);
      continue;
    }
    nodes ??= new Map(graph.nodes.map((node) => [node.id, node]));
    if (change.action === "delete") nodes.delete(change.id);
    else if (change.action === "create") {
      const node = {
        ...change.value,
        id: change.id,
        type: change.kind === "group" ? "group" : "canvas",
      } as GraphNode;
      const data = change.value?.data as Record<string, unknown> | undefined;
      if (node.type === "canvas" && data?.manualReferences) {
        const { manualReferences: references, ...rest } = data;
        node.data = {
          ...rest,
          ...applyManualReferences(node.data, references as ManualReferences),
        } as typeof node.data;
      }
      nodes.set(change.id, node);
    } else {
      const node = nodes.get(change.id);
      if (node) nodes.set(change.id, applyNodeFields(node, change.fields ?? {}));
    }
  }
  if (!nodes && !edges) return graph;
  const records = nodes ? [...nodes.values()] : graph.nodes;
  return {
    ...graph,
    nodes: nodes
      ? [
          ...records.filter((node) => node.type === "group"),
          ...records.filter((node) => node.type !== "group"),
        ]
      : graph.nodes,
    edges: edges ? [...edges.values()] : graph.edges,
  };
}

function applyNodeFields(node: GraphNode, fields: Record<string, unknown>): GraphNode {
  const next: Record<string, unknown> = { ...node };
  const data: Record<string, unknown> = { ...node.data };
  for (const [field, value] of Object.entries(fields)) {
    if (field === outputField && node.type === "canvas" && !Object.hasOwn(fields, "data.src")) {
      if (typeof value === "string") Object.assign(data, selectOutput(node.data, value));
    } else if (field === referenceField && node.type === "canvas") {
      Object.assign(
        data,
        applyManualReferences(data as CanvasNodeData, (value ?? {}) as ManualReferences),
      );
    } else if (field === "data.params" && node.type === "canvas") {
      const options = Object.entries((value as Record<string, unknown> | null) ?? {});
      const other = Object.entries((data.params as Record<string, unknown> | undefined) ?? {});
      data.params = Object.fromEntries([
        ...options.filter(([name, option]) => isEditableOption(node, name, option)),
        ...other.filter(([name, option]) => !isEditableOption(node, name, option)),
      ]);
    } else if (field.startsWith("data.")) {
      if (value === null) delete data[field.slice(5)];
      else data[field.slice(5)] = value;
      if (field === "data.prompt" && node.type === "canvas") {
        const params = { ...(data.params as Record<string, unknown> | undefined) };
        if (value === null) delete params.prompt;
        else params.prompt = value as string;
        data.params = params;
      }
    } else if (value === null) delete next[field];
    else next[field] = value;
  }
  return { ...next, data } as unknown as GraphNode;
}
