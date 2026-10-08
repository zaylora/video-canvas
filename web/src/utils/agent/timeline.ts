import type {
  AgentApprovalDto,
  AgentDeletePayload,
  AgentEventDto,
  AgentRunStatus,
} from "@/api/agent/type";
import { TOOL_LABELS } from "@/constants/agent";
import {
  ACTIVE_RUN_STATUSES,
  activeRunId,
  type AgentPlanStep,
  type AgentRunLive,
  type AgentSessionState,
} from "./session-state";

/** 活动块里的一次工具调用 */
export type ToolCall = {
  key: string;
  /** 工具名 */
  name: string;
  /** 进行中 / 完成 / 失败 / 已中止（运行结束时它还没有结果） */
  status: "running" | "done" | "error" | "aborted";
  /** 一行摘要 */
  summary: string;
  /** 它涉及的节点，点击后定位 */
  nodeIds: string[];
};

/** 消息流里的一项：已经按展示的样子排好，组件只管画 */
export type TimelineItem =
  | {
      /** 用户的消息 */
      type: "user";
      key: string;
      /** 原文，行内 chip 是 @[名字](类型:id) */
      text: string;
      /** 是不是运行中的插话 */
      steer: boolean;
    }
  | {
      /** 助手的回复 */
      type: "assistant";
      key: string;
      /** 正文 */
      text: string;
      /** 思考过程，没有为空串 */
      thinking: string;
      /** 还在流式输出 */
      streaming: boolean;
    }
  | {
      /** 活动块：同一轮里连续的工具调用合成一块，运行中展开、结束后收成一行摘要 */
      type: "activity";
      key: string;
      runId: string;
      tools: ToolCall[];
      /** 有调用还在进行、或运行还在跑时的最后一块就是 running；有被中止的就是 aborted；否则 done */
      status: "running" | "done" | "aborted";
      /** 失败的调用数 */
      errors: number;
      /** 第一次调用开始的时间（毫秒），事件没带时间时为 null */
      startedAt: number | null;
      /** 最后一次调用结束的时间（毫秒），还没结束或没带时间时为 null */
      endedAt: number | null;
    }
  | {
      /** 审批或提问，内容以 approvals 里的最新状态为准 */
      type: "approval";
      key: string;
      approvalId: string;
    }
  | {
      /** 运行的非正常收尾：失败、停止、暂停等 */
      type: "status";
      key: string;
      runId: string;
      status: AgentRunStatus;
      /** 给用户看的原因，没有为空串 */
      error: string;
    }
  | {
      /** 一轮运行改过画布并已收尾：改动摘要（新建、修改、删除了哪些节点）和撤销本轮 */
      type: "run-summary";
      key: string;
      runId: string;
      /** 新建的节点 id */
      created: string[];
      /** 改过字段的节点 id（不含本轮新建的） */
      updated: string[];
      /** 删除的节点，名字取自删除审批（节点已经不在画布上了） */
      deleted: { id: string; label: string }[];
    }
  | {
      /** 计划全部完成 */
      type: "plan-done";
      key: string;
      /** 完成的步数 */
      total: number;
    };

type ActivityItem = Extract<TimelineItem, { type: "activity" }>;

/** 值得在消息流里留一张卡片的运行状态：用户需要知道发生了什么、能做什么 */
const NOTABLE_STATUSES: readonly AgentRunStatus[] = [
  "failed",
  "timeout",
  "interrupted",
  "budget_exhausted",
  "step_limit",
  "canceled",
  "expired",
];

/** 会改画布内容、「撤销本轮」能倒回去的工具（生成不在其中：已提交的任务撤销不了） */
const WRITE_TOOLS = ["canvas_apply_ops", "canvas_arrange", "canvas_delete"];

const str = (v: unknown) => (typeof v === "string" ? v : "");
const strs = (v: unknown) =>
  Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : [];
/** 事件时间转毫秒，没有或不合法为 null */
const time = (ev: AgentEventDto) => {
  const t = Date.parse(ev.created_at);
  return Number.isNaN(t) ? null : t;
};

/** 计划是否全部完成 */
export const planFinished = (steps: AgentPlanStep[] | undefined) =>
  !!steps && steps.length > 0 && steps.every((s) => s.status === "done");

/** 收集中的一次工具调用：比 ToolCall 多了归属的运行和起止时间，合并成活动块时用 */
type RawTool = ToolCall & {
  type: "tool";
  runId: string;
  startedAt: number | null;
  endedAt: number | null;
};

/** 一轮运行的画布改动，按节点去重 */
type RunChanges = { created: Set<string>; updated: Set<string> };

/**
 * 从会话事件推出消息流。
 * 先按事件顺序排出用户消息、助手回复、工具调用、审批、状态卡片，
 * 再把同一轮里连续的工具调用合成活动块，最后在改过画布且已收尾的运行后面放改动摘要。
 */
export function buildTimeline(state: AgentSessionState): TimelineItem[] {
  const items: (TimelineItem | RawTool)[] = [];
  const toolAt = new Map<string, number>();
  const seenStatus = new Set<string>();
  const doneRuns = new Set<string>();
  /** 每轮运行最后一项的位置，和这一轮有没有改过画布 */
  const lastAt = new Map<string, number>();
  const wrote = new Set<string>();
  const changes = new Map<string, RunChanges>();

  for (const ev of state.events) {
    const d = ev.data ?? {};
    const runId = ev.run_id ?? "";
    const before = items.length;
    switch (ev.type) {
      case "message.user":
        items.push({ type: "user", key: `e${ev.seq}`, text: str(d.text), steer: d.steer === true });
        break;
      case "message.done":
        if (str(d.text) || str(d.thinking)) {
          items.push({
            type: "assistant",
            key: `e${ev.seq}`,
            text: str(d.text),
            thinking: str(d.thinking),
            streaming: false,
          });
        }
        break;
      case "tool.start": {
        const id = str(d.id) || `e${ev.seq}`;
        toolAt.set(id, items.length);
        items.push({
          type: "tool",
          key: `t${id}`,
          runId,
          name: str(d.name),
          status: "running",
          summary: "",
          nodeIds: [],
          startedAt: time(ev),
          endedAt: null,
        });
        break;
      }
      case "tool.end": {
        const id = str(d.id) || `e${ev.seq}`;
        const status = d.is_error === true ? "error" : "done";
        const patch = {
          name: str(d.name),
          status,
          summary: str(d.summary),
          nodeIds: strs(d.node_ids),
        } as const;
        const at = toolAt.get(id);
        const prev = at === undefined ? undefined : items[at];
        if (prev?.type === "tool") items[at!] = { ...prev, ...patch, endedAt: time(ev) };
        else
          items.push({
            type: "tool",
            key: `t${id}`,
            runId,
            ...patch,
            startedAt: time(ev),
            endedAt: time(ev),
          });
        if (status === "done" && runId) recordChanges(changes, runId, d);
        break;
      }
      case "approval.created": {
        const id = str(d.id);
        if (id) items.push({ type: "approval", key: `a${id}`, approvalId: id });
        break;
      }
      case "run.status": {
        const status = str(d.status) as AgentRunStatus;
        const dedupe = `${runId}:${status}`;
        if (NOTABLE_STATUSES.includes(status) && !seenStatus.has(dedupe)) {
          seenStatus.add(dedupe);
          items.push({ type: "status", key: `s${ev.seq}`, runId, status, error: str(d.error) });
        }
        break;
      }
      case "plan.updated": {
        const steps = Array.isArray(d.steps) ? (d.steps as AgentPlanStep[]) : [];
        if (planFinished(steps) && !doneRuns.has(runId)) {
          doneRuns.add(runId);
          items.push({ type: "plan-done", key: `p${ev.seq}`, total: steps.length });
        }
        break;
      }
    }
    if (runId) {
      if (items.length > before) lastAt.set(runId, items.length - 1);
      if (ev.type === "tool.end" && d.is_error !== true && WRITE_TOOLS.includes(str(d.name)))
        wrote.add(runId);
    }
  }

  // 运行已经不在进行了，还没有结果的工具调用不会再有结果：标为已中止
  for (const at of toolAt.values()) {
    const item = items[at];
    if (item.type !== "tool" || item.status !== "running") continue;
    const live: AgentRunLive | undefined = state.runs[item.runId];
    if (live && !ACTIVE_RUN_STATUSES.includes(live.status))
      items[at] = { ...item, status: "aborted" };
  }

  // 改过画布且已经收尾的运行，在它最后一项后面放改动摘要；从后往前插，前面的位置不会错位
  const summaries = [...wrote]
    .filter(
      (id) =>
        state.runs[id] && !ACTIVE_RUN_STATUSES.includes(state.runs[id].status) && lastAt.has(id),
    )
    .sort((a, b) => (lastAt.get(b) ?? 0) - (lastAt.get(a) ?? 0));
  for (const runId of summaries)
    items.splice((lastAt.get(runId) ?? 0) + 1, 0, runSummary(state, runId, changes.get(runId)));

  const out = groupTools(items);
  keepLastActivityOpen(out, state);
  if (state.stream.text || state.stream.thinking) {
    out.push({
      type: "assistant",
      key: "stream",
      text: state.stream.text,
      thinking: state.stream.thinking,
      streaming: true,
    });
  }
  return out;
}

/** 记下 canvas_apply_ops 报告的新建、修改节点；本轮新建过的节点再被修改仍只算新建 */
function recordChanges(map: Map<string, RunChanges>, runId: string, d: Record<string, unknown>) {
  const created = strs(d.created);
  const updated = strs(d.updated);
  if (created.length === 0 && updated.length === 0) return;
  const c = map.get(runId) ?? { created: new Set<string>(), updated: new Set<string>() };
  for (const id of created) c.created.add(id);
  for (const id of updated) if (!c.created.has(id)) c.updated.add(id);
  map.set(runId, c);
}

/** 一轮的改动摘要：删除取自这一轮已执行的删除审批，被删的节点不再算新建或修改 */
function runSummary(
  state: AgentSessionState,
  runId: string,
  c: RunChanges | undefined,
): TimelineItem {
  const deleted: { id: string; label: string }[] = [];
  for (const a of Object.values(state.approvals)) {
    if (a.run_id !== runId || a.kind !== "delete" || a.status !== "executed") continue;
    const p = a.payload as AgentDeletePayload | undefined;
    for (const id of strs(a.decision?.node_ids)) {
      const at = p?.node_ids?.indexOf(id) ?? -1;
      deleted.push({ id, label: (at >= 0 ? p?.labels?.[at] : undefined) || id });
    }
  }
  const gone = new Set(deleted.map((x) => x.id));
  return {
    type: "run-summary",
    key: `f${runId}`,
    runId,
    created: [...(c?.created ?? [])].filter((id) => !gone.has(id)),
    updated: [...(c?.updated ?? [])].filter((id) => !gone.has(id)),
    deleted,
  };
}

/** 把同一轮里连续的工具调用合成活动块 */
function groupTools(items: (TimelineItem | RawTool)[]): TimelineItem[] {
  const out: TimelineItem[] = [];
  let block: ActivityItem | null = null;
  for (const item of items) {
    if (item.type !== "tool") {
      block = null;
      out.push(item);
      continue;
    }
    const { type: _type, runId, startedAt, endedAt, ...call } = item;
    if (!block || block.runId !== runId) {
      block = {
        type: "activity",
        key: `g${item.key}`,
        runId,
        tools: [],
        status: "done",
        errors: 0,
        startedAt,
        endedAt: null,
      };
      out.push(block);
    }
    block.tools.push(call);
    if (call.status === "error") block.errors++;
    if (call.status === "running") block.status = "running";
    else if (call.status === "aborted" && block.status !== "running") block.status = "aborted";
    if (block.startedAt === null) block.startedAt = startedAt;
    if (endedAt !== null) block.endedAt = Math.max(block.endedAt ?? endedAt, endedAt);
  }
  return out;
}

/**
 * 运行还在进行时，最后一个活动块在工具之间的空档（模型思考中）也算进行中，保持展开；
 * 后面已经有正文输出，或运行已经结束，就按各自的调用结果收起。
 */
function keepLastActivityOpen(out: TimelineItem[], state: AgentSessionState) {
  for (let i = out.length - 1; i >= 0; i--) {
    const item = out[i];
    if (item.type === "assistant" && item.text) return;
    if (item.type !== "activity") continue;
    const live: AgentRunLive | undefined = state.runs[item.runId];
    if (live && ACTIVE_RUN_STATUSES.includes(live.status) && item.status === "done")
      out[i] = { ...item, status: "running" };
    return;
  }
}

/** 置顶计划条的内容 */
export type RunStrip = {
  /** 哪一轮运行的计划 */
  runId: string;
  /** 进度：已完成 / 总步数 */
  progress: { done: number; total: number };
  /** 当前正在做的一步的标题（进行中的第一步，没有就取第一个待做的） */
  current: string;
  /** 运行状态 */
  status: AgentRunStatus;
  /** 运行还占着画布 */
  active: boolean;
  /** 完整的计划步骤 */
  steps: AgentPlanStep[];
  /** 计划没做完但运行已经结束：条上显示「未完成」和 ✕ */
  unfinished: boolean;
};

/**
 * 置顶计划条要显示什么，不该显示返回 null。只管计划，运行状态由消息流末尾的状态行负责：
 * - 最新那一轮有计划且还在进行：显示进度
 * - 运行已结束但计划没做完：显示「未完成」，用户点 ✕ 隐藏（hiddenRuns）
 * - 没有计划、计划全部完成且运行已结束：不显示
 */
export function buildRunStrip(
  state: AgentSessionState,
  hiddenRuns: ReadonlySet<string>,
): RunStrip | null {
  const ids = Object.keys(state.runs);
  if (ids.length === 0) return null;
  const runId = latestRunId(state, ids);
  const live = state.runs[runId];
  const steps = state.plans[runId] ?? [];
  if (steps.length === 0) return null;
  const active = ACTIVE_RUN_STATUSES.includes(live.status);
  const unfinished = !active && !planFinished(steps);
  if (!active && !unfinished) return null;
  if (!active && hiddenRuns.has(runId)) return null;

  const done = steps.filter((s) => s.status === "done").length;
  const current =
    (steps.find((s) => s.status === "doing") ?? steps.find((s) => s.status === "todo"))?.title ??
    "";
  return {
    runId,
    progress: { done, total: steps.length },
    current,
    status: live.status,
    active,
    steps,
    unfinished,
  };
}

/** 最新的一轮运行：事件序号最大的 run.status 所属的那轮 */
function latestRunId(state: AgentSessionState, ids: string[]): string {
  let best = ids[ids.length - 1];
  let bestSeq = -1;
  for (const ev of state.events) {
    if (ev.type === "run.status" && ev.run_id && state.runs[ev.run_id] && ev.seq > bestSeq) {
      best = ev.run_id;
      bestSeq = ev.seq;
    }
  }
  return best;
}

/** 消息流末尾的状态行：运行中唯一的实时指示 */
export type StatusLine = {
  runId: string;
  /** 思考中… / 正在{工具}… / 等你确认 / 等你回答 */
  text: string;
  /** 在等用户：静态显示，不流光不计时 */
  waiting: boolean;
  /** 这一段运行开始的时间（毫秒），计时用；没带时间时为 null */
  since: number | null;
};

/**
 * 状态行显示什么，不该显示返回 null：
 * - 没有进行中的运行：不显示
 * - 等批准、等回答：静态提示
 * - 正文正在流式输出：不显示（有闪烁光标，避免重复）
 * - 其余：有进行中的工具就说「正在{工具}…」，否则「思考中…」
 */
export function buildStatusLine(state: AgentSessionState): StatusLine | null {
  const runId = activeRunId(state);
  if (!runId) return null;
  const status = state.runs[runId]?.status;
  if (status === "waiting_approval" || status === "waiting_input") {
    const text = status === "waiting_approval" ? "等你确认" : "等你回答";
    return { runId, text, waiting: true, since: null };
  }
  if (state.stream.text) return null;

  const open = new Map<string, string>();
  let since: number | null = null;
  let first: number | null = null;
  for (const ev of state.events) {
    if (ev.run_id !== runId) continue;
    const d = ev.data ?? {};
    if (ev.type === "tool.start") open.set(str(d.id) || `e${ev.seq}`, str(d.name));
    if (ev.type === "tool.end") open.delete(str(d.id) || `e${ev.seq}`);
    if (ev.type === "run.status") {
      first ??= time(ev);
      // 每次开始或继续都会先进 queued：计时从最近一次算起
      if (str(d.status) === "queued") since = time(ev);
    }
  }
  const running = [...open.values()].at(-1);
  const text = running ? `正在${TOOL_LABELS[running] ?? running}…` : "思考中…";
  return { runId, text, waiting: false, since: since ?? first };
}

/** 审批是否还在等用户：未决定且没过期 */
export const isApprovalPending = (a: AgentApprovalDto | undefined) => a?.status === "pending";
