// 断言引擎：纯函数。输入一条断言和运行上下文，返回 {type, ok, expected, got, evidence}。
// 一期断言全集见设计文档 6.B.4。证据要具体：节点 id、相交的两个矩形坐标、越界的量、缺了哪种的组。
//
// 上下文 ctx：
//   canvas      最终画布 {nodes, edges}
//   run         {status, error, steps, spent_credits, tool_errors}，由 events.mjs 的 deriveRun 从事件推出
//   approvals   出现过的审批和提问（events.mjs 的 approvalFromEvent 规整过的结构）
//   taskOutputs 生成任务的产物数 {节点 id: 数量}。前端回填产物，没有浏览器的 harness 看不到，用任务接口补上
//   sizeOpts    传给 nodeSize 的选项（模型默认画幅），可省略

import { nodeSize } from "./size.mjs";
import { RUN_STATUSES } from "./events.mjs";
import { matchApproval } from "./answers.mjs";

export const ASSERTION_TYPES = new Set(["run_status", "no_overlap", "members_inside_group", "count", "every_group_has", "edge_exists", "nodes_have_prompt", "nodes_have_output", "asked", "not_asked", "max_tool_errors", "max_steps", "max_credits"]);

const EPS = 0.01; // 浮点误差容忍
const MAX_EVIDENCE = 20; // 证据最多列这么多条，其余折叠成一行

// ---------- 画布几何 ----------

const nodesOf = (canvas) => (Array.isArray(canvas?.nodes) ? canvas.nodes : []);
const edgesOf = (canvas) => (Array.isArray(canvas?.edges) ? canvas.edges : []);
const isGroup = (n) => n.type === "group";
const kindOf = (n) => n.data?.kind ?? "";
const fmtNum = (v) => String(Number(Number(v).toFixed(1)));

function indexNodes(canvas) {
  const byId = new Map();
  for (const n of nodesOf(canvas)) byId.set(n.id, n);
  return byId;
}

/** 节点矩形。位置是节点自己的 position（成员是相对组左上角的），尺寸按共享规则算。 */
function rectOf(n, sizeOpts) {
  const { w, h } = nodeSize(n, sizeOpts);
  return { x: Number(n.position?.x) || 0, y: Number(n.position?.y) || 0, w, h };
}

const fmtRect = (id, r) => `${id}(${fmtNum(r.x)},${fmtNum(r.y)},${fmtNum(r.w)}×${fmtNum(r.h)})`;

function overlapOf(a, b) {
  const ow = Math.min(a.x + a.w, b.x + b.w) - Math.max(a.x, b.x);
  const oh = Math.min(a.y + a.h, b.y + b.h) - Math.max(a.y, b.y);
  return ow > EPS && oh > EPS ? { w: ow, h: oh } : null;
}

function capEvidence(list) {
  if (list.length <= MAX_EVIDENCE) return list;
  return [...list.slice(0, MAX_EVIDENCE), `…另有 ${list.length - MAX_EVIDENCE} 条未列出`];
}

const result = (a, ok, expected, got, evidence = []) => ({ type: a.type, ok, expected, got, evidence: capEvidence(evidence) });

// ---------- 各断言 ----------

function noOverlap(a, ctx) {
  const byId = indexNodes(ctx.canvas);
  const nodes = nodesOf(ctx.canvas);
  const hits = [];
  const pairs = (items, scope) => {
    for (let i = 0; i < items.length; i++) {
      for (let j = i + 1; j < items.length; j++) {
        const ra = rectOf(items[i], ctx.sizeOpts);
        const rb = rectOf(items[j], ctx.sizeOpts);
        const o = overlapOf(ra, rb);
        if (o) hits.push(`${scope}：${fmtRect(items[i].id, ra)} ∩ ${fmtRect(items[j].id, rb)}，重叠 ${fmtNum(o.w)}×${fmtNum(o.h)}`);
      }
    }
  };
  // 顶层：不在任何组里的对象（含组本身）；parentId 指向不存在的组时当作顶层
  pairs(nodes.filter((n) => !n.parentId || !byId.has(n.parentId)), "顶层");
  // 同组成员：用组内相对坐标比较
  for (const g of nodes.filter(isGroup)) pairs(nodes.filter((n) => n.parentId === g.id), `组 ${g.id} 内（相对坐标）`);
  return result(a, hits.length === 0, "顶层对象两两之间、同组成员两两之间矩形不相交", `${hits.length} 处相交`, hits);
}

function membersInsideGroup(a, ctx) {
  const byId = indexNodes(ctx.canvas);
  const bad = [];
  let checked = 0;
  for (const n of nodesOf(ctx.canvas)) {
    if (!n.parentId) continue;
    const g = byId.get(n.parentId);
    if (!g || !isGroup(g)) {
      bad.push(`${n.id}：parentId=${n.parentId} 指向的组不存在`);
      continue;
    }
    checked++;
    const r = rectOf(n, ctx.sizeOpts);
    const gs = nodeSize(g, ctx.sizeOpts);
    const sides = [];
    if (r.x < -EPS) sides.push(`左边超出组框 ${fmtNum(-r.x)}px`);
    if (r.y < -EPS) sides.push(`顶边超出组框 ${fmtNum(-r.y)}px`);
    if (r.x + r.w > gs.w + EPS) sides.push(`右边超出组框 ${fmtNum(r.x + r.w - gs.w)}px`);
    if (r.y + r.h > gs.h + EPS) sides.push(`底边超出组框 ${fmtNum(r.y + r.h - gs.h)}px`);
    if (sides.length) bad.push(`${n.id}(${fmtNum(r.w)}×${fmtNum(r.h)}) ${sides.join("，")}（组 ${g.id} 框 ${fmtNum(gs.w)}×${fmtNum(gs.h)}，成员相对坐标 ${fmtNum(r.x)},${fmtNum(r.y)}）`);
  }
  return result(a, bad.length === 0, "每个成员矩形完全落在组框内", bad.length === 0 ? (checked ? "全部在组框内" : "没有成员") : `${bad.length} 个成员越界`, bad);
}

function countOf(canvas, of) {
  const nodes = nodesOf(canvas);
  if (of === "group") return nodes.filter(isGroup).length;
  if (of === "node") return nodes.filter((n) => !isGroup(n)).length;
  if (of === "edge") return edgesOf(canvas).length;
  return nodes.filter((n) => !isGroup(n) && kindOf(n) === of).length;
}

function count(a, ctx) {
  const n = countOf(ctx.canvas, a.of);
  const okMin = a.min === undefined || n >= a.min;
  const okMax = a.max === undefined || n <= a.max;
  const range = `[${a.min ?? "0"}, ${a.max ?? "∞"}]`;
  const ev = [];
  if (!okMin) ev.push(`${a.of} 只有 ${n} 个，少于下限 ${a.min}`);
  if (!okMax) ev.push(`${a.of} 有 ${n} 个，多于上限 ${a.max}`);
  return result(a, okMin && okMax, `${a.of} 的数量在 ${range} 内`, `${a.of} 数量 ${n}`, ev);
}

function everyGroupHas(a, ctx) {
  const nodes = nodesOf(ctx.canvas);
  const groups = nodes.filter(isGroup);
  const expected = `每个组都包含 ${a.kinds.join(" / ")}`;
  if (groups.length === 0) return result(a, false, expected, "画布里没有组", ["画布里一个组都没有，无法判断"]);
  const bad = [];
  for (const g of groups) {
    const have = new Set(nodes.filter((n) => n.parentId === g.id).map(kindOf));
    const missing = a.kinds.filter((k) => !have.has(k));
    if (missing.length) bad.push(`${g.id} 缺 ${missing.join("、")}（现有：${[...have].join("、") || "无成员"}）`);
  }
  return result(a, bad.length === 0, expected, bad.length === 0 ? `${groups.length} 个组全部满足` : `${bad.length}/${groups.length} 个组不满足`, bad);
}

const nodeMatches = (spec, n) => n.id === spec || kindOf(n) === spec;

function edgeExists(a, ctx) {
  const byId = indexNodes(ctx.canvas);
  const edges = edgesOf(ctx.canvas);
  const link = (e, within) => {
    const s = byId.get(e.source);
    const t = byId.get(e.target);
    if (!s || !t || !nodeMatches(a.from, s) || !nodeMatches(a.to, t)) return false;
    return within ? s.parentId === within && t.parentId === within : true;
  };
  if (a.scope === "group") {
    const groups = nodesOf(ctx.canvas).filter(isGroup);
    const expected = `每个组内都有 ${a.from}→${a.to} 的连线`;
    if (groups.length === 0) return result(a, false, expected, "画布里没有组", ["画布里一个组都没有，无法判断"]);
    const bad = groups.filter((g) => !edges.some((e) => link(e, g.id))).map((g) => `${g.id} 缺 ${a.from}→${a.to} 连线`);
    return result(a, bad.length === 0, expected, `${groups.length - bad.length}/${groups.length} 个组有连线`, bad);
  }
  const found = edges.filter((e) => link(e));
  return result(a, found.length > 0, `存在 ${a.from}→${a.to} 的连线`, `${found.length} 条`, found.length ? [] : [`没有从 ${a.from} 指向 ${a.to} 的连线（画布共 ${edges.length} 条线）`]);
}

const promptOf = (n) => {
  const d = n.data ?? {};
  for (const v of [d.prompt, d.params?.prompt]) if (typeof v === "string" && v.trim()) return v.trim();
  return "";
};

function hasOutput(n, ctx) {
  const d = n.data ?? {};
  return (Array.isArray(d.outputs) && d.outputs.length > 0) || (typeof d.src === "string" && d.src !== "") || (ctx.taskOutputs?.[n.id] ?? 0) > 0;
}

function nodesHave(a, ctx, label, test) {
  const targets = nodesOf(ctx.canvas).filter((n) => !isGroup(n) && a.kinds.includes(kindOf(n)));
  const expected = `${a.kinds.join(" / ")} 节点都${label}`;
  if (targets.length === 0) return result(a, false, expected, `没有 ${a.kinds.join(" / ")} 节点`, ["画布里没有这些种类的节点，无法判断"]);
  const bad = targets.filter((n) => !test(n)).map((n) => `${n.id}（${kindOf(n)}）${label === "有提示词" ? "没有提示词" : "还没有产物"}`);
  return result(a, bad.length === 0, expected, `${targets.length - bad.length}/${targets.length} 个节点满足`, bad);
}

const questionLines = (list) => list.map((x) => `${x.kind}${x.ask_kind ? `/${x.ask_kind}` : ""}：${x.text || "(无文本)"}`);
const filterText = (a) => ["kind", "ask_kind", "model_kind", "contains"].filter((k) => a[k] !== undefined).map((k) => `${k}=${a[k]}`).join(" ") || "任意";

function asked(a, ctx, want) {
  const hit = (ctx.approvals ?? []).filter((x) => matchApproval({ kind: a.kind, ask_kind: a.ask_kind, model_kind: a.model_kind, contains: a.contains }, x));
  const ok = want ? hit.length > 0 : hit.length === 0;
  const expected = `${want ? "出现过" : "没有出现过"}提问或审批（${filterText(a)}）`;
  if (want) return result(a, ok, expected, ok ? `出现 ${hit.length} 次` : "一次都没出现", ok ? questionLines(hit) : [`本轮共 ${(ctx.approvals ?? []).length} 次提问或审批：${questionLines(ctx.approvals ?? []).join("；") || "无"}`]);
  return result(a, ok, expected, ok ? "没有出现" : `出现 ${hit.length} 次`, questionLines(hit));
}

function maxOf(a, ctx, field, label) {
  const v = Number(ctx.run?.[field]) || 0;
  return result(a, v <= a.max, `${label} ≤ ${a.max}`, `${label} ${v}`, v <= a.max ? [] : [`${label}实际 ${v}，超过上限 ${a.max}`]);
}

function runStatus(a, ctx) {
  const st = ctx.run?.status ?? "unknown";
  const ok = a.in.includes(st);
  const ev = [];
  if (!ok) ev.push(`终态 ${st}，期望 ${a.in.join(" / ")}`);
  if (ctx.run?.error) ev.push(`错误：${ctx.run.error}`);
  return result(a, ok, `终态在 [${a.in.join(", ")}] 里`, st, ev);
}

const IMPL = {
  run_status: runStatus,
  no_overlap: noOverlap,
  members_inside_group: membersInsideGroup,
  count,
  every_group_has: everyGroupHas,
  edge_exists: edgeExists,
  nodes_have_prompt: (a, c) => nodesHave(a, c, "有提示词", (n) => promptOf(n) !== ""),
  nodes_have_output: (a, c) => nodesHave(a, c, "已有产物", (n) => hasOutput(n, c)),
  asked: (a, c) => asked(a, c, true),
  not_asked: (a, c) => asked(a, c, false),
  max_tool_errors: (a, c) => maxOf(a, c, "tool_errors", "工具报错数"),
  max_steps: (a, c) => maxOf(a, c, "steps", "步数"),
  max_credits: (a, c) => maxOf(a, c, "spent_credits", "积分"),
};

/** 执行一条断言。未知类型判失败而不是抛错，让整批报告还能出来。 */
export function runAssertion(a, ctx) {
  const impl = IMPL[a?.type];
  if (!impl) return { type: String(a?.type), ok: false, expected: "已支持的断言类型", got: `未知断言类型：${a?.type}`, evidence: [`支持：${[...ASSERTION_TYPES].join("、")}`] };
  try {
    return impl(a, ctx);
  } catch (e) {
    return { type: a.type, ok: false, expected: "断言能正常执行", got: `断言执行出错：${e.message}`, evidence: [] };
  }
}

export const runAssertions = (list, ctx) => list.map((a) => runAssertion(a, ctx));

// ---------- 用例里写的断言是否合法 ----------

const isNum = (v) => typeof v === "number" && Number.isFinite(v);
const isStrList = (v) => Array.isArray(v) && v.length > 0 && v.every((x) => typeof x === "string" && x !== "");
const STATUS_HINT = { done: "succeeded", error: "failed", success: "succeeded" };

/** 返回问题列表，空数组表示合法。 */
export function validateAssertion(a) {
  if (!a || typeof a !== "object" || Array.isArray(a)) return ["断言必须是对象"];
  if (!ASSERTION_TYPES.has(a.type)) return [`未知断言类型 ${JSON.stringify(a.type)}，支持：${[...ASSERTION_TYPES].join("、")}`];
  const t = a.type;
  const errs = [];
  switch (t) {
    case "run_status":
      if (!isStrList(a.in)) errs.push("run_status 需要非空数组 in");
      else {
        for (const s of a.in) {
          if (RUN_STATUSES.includes(s)) continue;
          errs.push(`run_status.in 里的 ${JSON.stringify(s)} 不是运行状态${STATUS_HINT[s] ? `，应写 ${STATUS_HINT[s]}` : ""}（可选：${RUN_STATUSES.join("、")}）`);
        }
      }
      break;
    case "count":
      if (typeof a.of !== "string" || !a.of) errs.push("count 需要 of（group / node / edge / 节点种类）");
      if (a.min !== undefined && !isNum(a.min)) errs.push("count.min 必须是数字");
      if (a.max !== undefined && !isNum(a.max)) errs.push("count.max 必须是数字");
      if (a.min === undefined && a.max === undefined) errs.push("count 需要 min 或 max 至少一个");
      break;
    case "every_group_has":
    case "nodes_have_prompt":
    case "nodes_have_output":
      if (!isStrList(a.kinds)) errs.push(`${t} 需要非空数组 kinds`);
      break;
    case "edge_exists":
      if (typeof a.from !== "string" || !a.from) errs.push("edge_exists 需要 from");
      if (typeof a.to !== "string" || !a.to) errs.push("edge_exists 需要 to");
      if (a.scope !== undefined && a.scope !== "group") errs.push('edge_exists.scope 只能是 "group"');
      break;
    case "asked":
    case "not_asked":
      if (a.kind !== undefined && !["generate", "delete", "ask"].includes(a.kind)) errs.push(`${t}.kind 只能是 generate / delete / ask`);
      for (const k of ["ask_kind", "model_kind", "contains"]) if (a[k] !== undefined && typeof a[k] !== "string") errs.push(`${t}.${k} 必须是字符串`);
      break;
    case "max_tool_errors":
    case "max_steps":
    case "max_credits":
      if (!isNum(a.max) || a.max < 0) errs.push(`${t} 需要非负数字 max`);
      break;
  }
  return errs;
}
