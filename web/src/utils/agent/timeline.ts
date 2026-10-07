import type { AgentApprovalDto, AgentEventDto, AgentRunStatus } from "@/api/agent/type";
import {
  ACTIVE_RUN_STATUSES,
  type AgentPlanStep,
  type AgentRunLive,
  type AgentSessionState,
} from "./session-state";

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
      /** 一次工具调用 */
      type: "tool";
      key: string;
      /** 工具名 */
      name: string;
      /** 进行中 / 完成 / 失败 / 已中止（运行结束时它还没有结果） */
      status: "running" | "done" | "error" | "aborted";
      /** 一行摘要 */
      summary: string;
      /** 它涉及的节点，点击后定位 */
      nodeIds: string[];
    }
  | {
      /** 审批或提问卡片，内容以 approvals 里的最新状态为准 */
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
      /** 一轮运行结束后的操作条：撤销本轮（这一轮改过画布才有） */
      type: "run-footer";
      key: string;
      runId: string;
    }
  | {
      /** 计划全部完成 */
      type: "plan-done";
      key: string;
      /** 完成的步数 */
      total: number;
    };

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

/** 计划是否全部完成 */
export const planFinished = (steps: AgentPlanStep[] | undefined) =>
  !!steps && steps.length > 0 && steps.every((s) => s.status === "done");

/**
 * 把会话事件排成消息流：用户消息、助手回复、工具行、审批卡片、运行状态卡片、计划完成卡片。
 * 工具行由 tool.start 开头、tool.end 补全；运行已经结束而工具还没有结果的标为已中止。
 * 流式文字（还没落成 message.done）放在最后一项。
 */
export function buildTimeline(state: AgentSessionState): TimelineItem[] {
  const items: TimelineItem[] = [];
  const toolAt = new Map<string, number>();
  const seenStatus = new Set<string>();
  const doneRuns = new Set<string>();
  /** 每轮运行最后一项的位置，和这一轮有没有改过画布 */
  const lastAt = new Map<string, number>();
  const wrote = new Set<string>();

  for (const ev of state.events) {
    const d = ev.data ?? {};
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
          name: str(d.name),
          status: "running",
          summary: "",
          nodeIds: [],
        });
        break;
      }
      case "tool.end": {
        const id = str(d.id) || `e${ev.seq}`;
        const status = d.is_error === true ? "error" : "done";
        const nodeIds = Array.isArray(d.node_ids)
          ? (d.node_ids as unknown[]).filter((x): x is string => typeof x === "string")
          : [];
        const patch = { name: str(d.name), status, summary: str(d.summary), nodeIds } as const;
        const at = toolAt.get(id);
        if (at !== undefined)
          items[at] = { ...(items[at] as Extract<TimelineItem, { type: "tool" }>), ...patch };
        else items.push({ type: "tool", key: `t${id}`, ...patch });
        break;
      }
      case "approval.created": {
        const id = str(d.id);
        if (id) items.push({ type: "approval", key: `a${id}`, approvalId: id });
        break;
      }
      case "run.status": {
        const status = str(d.status) as AgentRunStatus;
        const runId = ev.run_id ?? "";
        const dedupe = `${runId}:${status}`;
        if (NOTABLE_STATUSES.includes(status) && !seenStatus.has(dedupe)) {
          seenStatus.add(dedupe);
          items.push({ type: "status", key: `s${ev.seq}`, runId, status, error: str(d.error) });
        }
        break;
      }
      case "plan.updated": {
        const steps = Array.isArray(d.steps) ? (d.steps as AgentPlanStep[]) : [];
        const runId = ev.run_id ?? "";
        if (planFinished(steps) && !doneRuns.has(runId)) {
          doneRuns.add(runId);
          items.push({ type: "plan-done", key: `p${ev.seq}`, total: steps.length });
        }
        break;
      }
    }
    if (ev.run_id) {
      if (items.length > before) lastAt.set(ev.run_id, items.length - 1);
      if (ev.type === "tool.end" && d.is_error !== true && WRITE_TOOLS.includes(str(d.name)))
        wrote.add(ev.run_id);
    }
  }

  // 运行已经不在进行了，还没有结果的工具调用不会再有结果：标为已中止
  const runOf = toolRunIds(state.events);
  for (const [id, at] of toolAt) {
    const item = items[at];
    if (item.type !== "tool" || item.status !== "running") continue;
    const live: AgentRunLive | undefined = state.runs[runOf.get(id) ?? ""];
    if (live && !ACTIVE_RUN_STATUSES.includes(live.status))
      items[at] = { ...item, status: "aborted" };
  }

  // 改过画布且已经收尾的运行，在它最后一项后面放操作条；从后往前插，前面的位置不会错位
  const footers = [...wrote]
    .filter(
      (id) =>
        state.runs[id] && !ACTIVE_RUN_STATUSES.includes(state.runs[id].status) && lastAt.has(id),
    )
    .sort((a, b) => (lastAt.get(b) ?? 0) - (lastAt.get(a) ?? 0));
  for (const runId of footers)
    items.splice((lastAt.get(runId) ?? 0) + 1, 0, { type: "run-footer", key: `f${runId}`, runId });

  if (state.stream.text || state.stream.thinking) {
    items.push({
      type: "assistant",
      key: "stream",
      text: state.stream.text,
      thinking: state.stream.thinking,
      streaming: true,
    });
  }
  return items;
}

/** 每个工具调用属于哪一轮运行 */
function toolRunIds(events: AgentEventDto[]) {
  const out = new Map<string, string>();
  for (const ev of events) {
    if (ev.type === "tool.start" && ev.run_id) out.set(str(ev.data?.id) || `e${ev.seq}`, ev.run_id);
  }
  return out;
}

/** 置顶运行条的内容 */
export type RunStrip = {
  /** 哪一轮运行的计划 / 状态 */
  runId: string;
  /** 进度：已完成 / 总步数；没有计划时为 null */
  progress: { done: number; total: number } | null;
  /** 当前正在做的一步的标题（进行中的第一步，没有就取第一个待做的） */
  current: string;
  /** 运行状态 */
  status: AgentRunStatus;
  /** 运行还占着画布 */
  active: boolean;
  /** 状态前缀：等你确认 / 等你回答，没有为空串 */
  prefix: "等你确认" | "等你回答" | "";
  /** 完整的计划步骤，没有为空数组 */
  steps: AgentPlanStep[];
  /** 计划没做完但运行已经结束：条上显示「未完成」和 ✕ */
  unfinished: boolean;
};

/**
 * 置顶运行条要显示什么，不该显示返回 null：
 * - 最新那一轮运行还在进行：显示（有计划显示进度，没有计划显示状态）
 * - 运行已结束但计划没做完：显示「未完成」，用户点 ✕ 隐藏（hiddenRuns）
 * - 计划全部完成、没有计划且运行已结束：不显示
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
  const active = ACTIVE_RUN_STATUSES.includes(live.status);
  const finished = planFinished(steps);
  const unfinished = !active && steps.length > 0 && !finished;
  if (!active && !unfinished) return null;
  if (!active && hiddenRuns.has(runId)) return null;

  const done = steps.filter((s) => s.status === "done").length;
  const current =
    (steps.find((s) => s.status === "doing") ?? steps.find((s) => s.status === "todo"))?.title ??
    "";
  const prefix =
    live.status === "waiting_approval"
      ? "等你确认"
      : live.status === "waiting_input"
        ? "等你回答"
        : "";
  return {
    runId,
    progress: steps.length > 0 ? { done, total: steps.length } : null,
    current,
    status: live.status,
    active,
    prefix,
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

/** 审批是否还在等用户：未决定且没过期 */
export const isApprovalPending = (a: AgentApprovalDto | undefined) => a?.status === "pending";
