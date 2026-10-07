/**
 * 画布改动的合并：后端 Agent 改了画布（canvas.patch），或保存撞上 409 后拉回了最新画布，
 * 前端把这些别处的改动并进用户正在看的画布，而不是整张重挂。
 *
 * 做法是带「改动前的值」的字段级合并：一项更新只有在本地的当前值还等于它的「改动前」时才写入，
 * 用户在这之间改过的字段保持用户的值（下次保存时会覆盖服务端，所以两边最终一致）。
 * 与后端 internal/canvasgraph 的 Change 同构：节点字段拍平成「路径 → 值」，data 展开一层。
 */

/** 一次改动里某个节点或连线的变化 */
export type GraphChange = {
  /** 节点或连线 */
  kind: "node" | "edge";
  /** 新建、修改、删除 */
  op: "create" | "update" | "delete";
  /** 节点或连线 id */
  id: string;
  /** 改动前的字段（拍平路径）；新建时没有 */
  before?: Record<string, unknown>;
  /** 改动后的字段（拍平路径）；删除时没有 */
  after?: Record<string, unknown>;
};

/** 合并只关心的节点形状：id、父组、业务数据，其余字段按拍平路径读写 */
export type MergeNode = { id: string; parentId?: string; data?: object };

/** 合并只关心的连线形状 */
export type MergeEdge = { id: string; source: string; target: string };

/** 要合并进去的画布 */
export type MergeGraph<N extends MergeNode, E extends MergeEdge> = { nodes: N[]; edges: E[] };

/** 合并的结果 */
export type MergeResult<N extends MergeNode, E extends MergeEdge> = {
  /** 合并后的节点 */
  nodes: N[];
  /** 合并后的连线 */
  edges: E[];
  /** 实际改到画布上的改动项数（节点或连线，一项算一个） */
  applied: number;
  /** 因为用户在这之间改过而保持用户值的字段数 */
  keptLocal: number;
  /** 这批改动写进节点的生成任务 id，调用方要对账 */
  taskIds: string[];
};

/** 合并的选项 */
export type MergeOptions<N extends MergeNode, E extends MergeEdge> = {
  /** 为 true 时只覆盖「还等于改动前」的字段（用户的优先）；false 是直接套用（对付后端存档的基准图） */
  guard: boolean;
  /** 由拍平的字段造出一个新节点（带上前端的运行时默认值） */
  makeNode: (fields: Record<string, unknown>) => N;
  /** 由拍平的字段造出一条新连线 */
  makeEdge: (fields: Record<string, unknown>) => E;
  /** 合并后整理节点顺序（组排在成员之前、清掉指向不存在的组的 parentId） */
  normalize?: (nodes: N[]) => N[];
};

/** null 和缺失视为同一种「没有」：后端不存空字段，前端有时写 null */
const nil = (v: unknown) => v === undefined || v === null;

/** 结构相等（JSON 形状的值） */
export function sameValue(a: unknown, b: unknown): boolean {
  if (nil(a) || nil(b)) return nil(a) && nil(b);
  if (a === b) return true;
  if (typeof a !== "object" || typeof b !== "object") return false;
  if (Array.isArray(a) !== Array.isArray(b)) return false;
  const ao = a as Record<string, unknown>;
  const bo = b as Record<string, unknown>;
  const keys = new Set([...Object.keys(ao), ...Object.keys(bo)]);
  for (const k of keys) if (!sameValue(ao[k], bo[k])) return false;
  return true;
}

const DATA_PREFIX = "data.";

/** 读节点的拍平路径 */
function readPath(node: MergeNode, path: string): unknown {
  if (path.startsWith(DATA_PREFIX))
    return (node.data as Record<string, unknown> | undefined)?.[path.slice(DATA_PREFIX.length)];
  return (node as Record<string, unknown>)[path];
}

/** 写（value 为 undefined 表示删除）节点的拍平路径，返回新节点，不改入参 */
function writePath<N extends MergeNode>(node: N, path: string, value: unknown): N {
  if (path.startsWith(DATA_PREFIX)) {
    const key = path.slice(DATA_PREFIX.length);
    const data = { ...(node.data as Record<string, unknown> | undefined) };
    if (value === undefined) delete data[key];
    else data[key] = value;
    return { ...node, data };
  }
  const next = { ...node } as Record<string, unknown>;
  if (value === undefined) delete next[path];
  else next[path] = value;
  return next as N;
}

/** 把拍平的字段还原成节点形状（"data.x" 放回 data） */
export function unflatten(fields: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  const data: Record<string, unknown> = {};
  for (const [path, value] of Object.entries(fields)) {
    if (path.startsWith(DATA_PREFIX)) data[path.slice(DATA_PREFIX.length)] = value;
    else out[path] = value;
  }
  out.data = data;
  return out;
}

/** 把节点拍平成「路径 → 值」，data 展开一层 */
export function flattenNode(node: MergeNode): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(node)) {
    if (k === "data" && v && typeof v === "object") {
      for (const [dk, dv] of Object.entries(v)) out[DATA_PREFIX + dk] = dv;
    } else out[k] = v;
  }
  return out;
}

/**
 * 比较两张画布，列出 b 相对 a 的新建、修改、删除；用来把拉回来的最新画布变成一批改动。
 * 连线只有新建和删除（连线没有可改的字段）。
 */
export function diffGraph<N extends MergeNode, E extends MergeEdge>(
  a: MergeGraph<N, E>,
  b: MergeGraph<N, E>,
): GraphChange[] {
  const out: GraphChange[] = [];
  const before = new Map(a.nodes.map((n) => [n.id, n]));
  const after = new Map(b.nodes.map((n) => [n.id, n]));
  for (const n of b.nodes) {
    const old = before.get(n.id);
    const fa = flattenNode(n);
    if (!old) {
      out.push({
        kind: "node",
        op: "create",
        id: n.id,
        after: fa,
      });
      continue;
    }
    const fb = flattenNode(old);
    const bf: Record<string, unknown> = {};
    const af: Record<string, unknown> = {};
    for (const p of new Set([...Object.keys(fa), ...Object.keys(fb)])) {
      if (sameValue(fa[p], fb[p])) continue;
      if (p in fa) af[p] = fa[p];
      if (p in fb) bf[p] = fb[p];
    }
    if (Object.keys(af).length || Object.keys(bf).length)
      out.push({ kind: "node", op: "update", id: n.id, before: bf, after: af });
  }
  for (const n of a.nodes) if (!after.has(n.id)) out.push({ kind: "node", op: "delete", id: n.id });
  const edgesA = new Set(a.edges.map((e) => e.id));
  const edgesB = new Set(b.edges.map((e) => e.id));
  for (const e of b.edges)
    if (!edgesA.has(e.id))
      out.push({
        kind: "edge",
        op: "create",
        id: e.id,
        after: { id: e.id, source: e.source, target: e.target, ...pickHandles(e) },
      });
  for (const e of a.edges)
    if (!edgesB.has(e.id)) out.push({ kind: "edge", op: "delete", id: e.id });
  return out;
}

/** 连线的连接点字段（有才带） */
function pickHandles(e: MergeEdge) {
  const { sourceHandle, targetHandle } = e as {
    sourceHandle?: string | null;
    targetHandle?: string | null;
  };
  return {
    ...(nil(sourceHandle) ? {} : { sourceHandle }),
    ...(nil(targetHandle) ? {} : { targetHandle }),
  };
}

/**
 * 把一批改动并进画布，返回新画布，不改入参。
 * - 新建：节点或连线已存在就跳过；连线两端有一端不存在也跳过（不留悬空的线）
 * - 修改：节点不存在就跳过；guard 时每个字段只有本地值等于「改动前」才写，否则保持用户的值
 * - 删除：直接删，并带走连着它的线
 */
export function applyChanges<N extends MergeNode, E extends MergeEdge>(
  graph: MergeGraph<N, E>,
  changes: GraphChange[],
  opt: MergeOptions<N, E>,
): MergeResult<N, E> {
  let nodes = graph.nodes.slice();
  let edges = graph.edges.slice();
  let applied = 0;
  let keptLocal = 0;
  const taskIds: string[] = [];
  const noteTask = (fields?: Record<string, unknown>) => {
    const t = fields?.["data.taskId"];
    if (typeof t === "string" && t) taskIds.push(t);
  };

  for (const c of changes) {
    if (c.kind === "edge") {
      if (c.op === "create" && c.after && !edges.some((e) => e.id === c.id)) {
        const { source, target } = c.after as { source?: string; target?: string };
        const ok = nodes.some((n) => n.id === source) && nodes.some((n) => n.id === target);
        if (ok && !edges.some((e) => e.source === source && e.target === target)) {
          edges.push(opt.makeEdge(c.after));
          applied++;
        }
      } else if (c.op === "delete" && edges.some((e) => e.id === c.id)) {
        edges = edges.filter((e) => e.id !== c.id);
        applied++;
      }
      continue;
    }
    const index = nodes.findIndex((n) => n.id === c.id);
    if (c.op === "create") {
      if (index < 0 && c.after) {
        nodes.push(opt.makeNode(c.after));
        noteTask(c.after);
        applied++;
      }
    } else if (c.op === "delete") {
      if (index >= 0) {
        nodes.splice(index, 1);
        applied++;
      }
    } else if (index >= 0) {
      let node = nodes[index];
      let touched = false;
      const apply = (path: string, value: unknown) => {
        if (opt.guard && !sameValue(readPath(node, path), c.before?.[path])) {
          keptLocal++;
          return;
        }
        node = writePath(node, path, value);
        touched = true;
      };
      for (const [path, value] of Object.entries(c.after ?? {})) apply(path, value);
      for (const path of Object.keys(c.before ?? {}))
        if (!(path in (c.after ?? {}))) apply(path, undefined);
      if (touched) {
        nodes[index] = node;
        noteTask(c.after);
        applied++;
      }
    }
  }

  // 被删节点连着的线一并带走
  const ids = new Set(nodes.map((n) => n.id));
  edges = edges.filter((e) => ids.has(e.source) && ids.has(e.target));
  if (opt.normalize) nodes = opt.normalize(nodes);
  return { nodes, edges, applied, keptLocal, taskIds };
}
