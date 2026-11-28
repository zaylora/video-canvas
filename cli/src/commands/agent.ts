// 小镜（画布里的 Agent）：复杂操作交给它；需要人拍板的动作它停下来等审批，命令行只负责把条目摊开、把人的决定送回去。

import { APIError, request } from "../client.ts";
import { retryNotice, withRetry } from "../retry.ts";
import { boolFlag, intFlag, listFlag, stringFlag, toInt } from "./args.ts";
import { CollaborationError, tryCollaboration } from "./collaboration.ts";
import { askYes, baseOf, emit, requireToken } from "./shared.ts";
import type { Args, Ctx } from "../types.ts";
import { encodeWireVersion } from "./collaboration-version.ts";

interface AgentSessionView {
  id: string;
  canvas_id: string;
  title: string;
  mode: string;
  last_seq: number;
}

interface AgentRunView {
  id: string;
  session_id: string;
  status: string;
}

interface AgentEventView {
  session_id: string;
  run_id: string | null;
  seq: number;
  type: string;
  data: unknown;
  created_at: string;
}

const AGENT_USAGE =
  'agent run <画布编号> --message "<要它做什么>" [--selection id,id] [--budget 积分] ' +
  "[--session 会话编号] [--watch] [--interval 2] [--timeout 600]\n" +
  "      agent events <会话编号> [--after 序号]\n" +
  "      agent approve <审批编号> [--session 会话编号] [--items 0,1] [--reject] [--answer 文本]\n" +
  "      agent cancel <运行编号>";

/** 小镜事件里的审批条目：一行一条，approval.created 的展示与 approve 前的摘要共用。 */
function describeApprovalItems(payload: Record<string, unknown>): string[] {
  const items = Array.isArray(payload.items) ? payload.items : [];
  return items.map((raw, i) => {
    const item = (raw ?? {}) as Record<string, unknown>;
    const label =
      (typeof item.label === "string" && item.label) ||
      (typeof item.node_id === "string" ? item.node_id : "") ||
      "（未命名动作）";
    const model =
      (typeof item.model_name === "string" && item.model_name) ||
      (typeof item.model_key === "string" ? item.model_key : "");
    const count = typeof item.count === "number" ? item.count : 1;
    const price = typeof item.price === "number" ? item.price : 0;
    const cost = price > 0 ? ` · 约 ${price * count} 积分` : "";
    return `[${i}] ${label}${model ? ` · ${model}` : ""} ×${count}${cost}`;
  });
}

/** 会话事件里最新的序号（不打印事件）：拿不到 last_seq 时用来定位「从现在开始看」的游标。 */
async function latestAgentSeq(ctx: Ctx, sid: string): Promise<number> {
  let cursor = 0;
  for (let page = 0; page < 500; page++) {
    const { events, next } = await pullAgentEvents(ctx, sid, cursor);
    if (!events.length) break;
    cursor = next;
  }
  return cursor;
}

/**
 * 决定这次对话用哪条会话，以及从哪个事件序号往后看。
 *
 * 给了 --session 就**先校验**它属于这张画布：不存在直接报用法错误，既不新建会话、也不提交运行。
 * 没给就复用画布上最近的一条，一条都没有才新建。游标永远是「会话当前的位置」，
 * 历史事件已经发生过，--watch 里不能重放一遍。
 */
async function resolveAgentSession(
  ctx: Ctx,
  code: string,
  requested: string | undefined,
): Promise<{ sid: string; cursor: number }> {
  const token = requireToken(ctx);
  const base = baseOf(ctx);
  const path = `/canvas/${encodeURIComponent(code)}/agent/sessions`;
  const sessions = await listAgentSessions(ctx, code);
  let session: AgentSessionView | undefined;
  if (requested) {
    session = sessions.find((item) => item.id === requested);
    if (!session) {
      const known = sessions.map((item) => item.id).join("、") || "（还没有会话）";
      throw new APIError(-1, `画布 ${code} 上没有会话 ${requested}；这张画布上有：${known}`, 2);
    }
  } else {
    session =
      sessions[sessions.length - 1] ??
      (await request<AgentSessionView>({
        base,
        token,
        method: "POST",
        path,
        body: { title: "命令行" },
        fetchImpl: ctx.fetchImpl,
      }));
  }
  const cursor =
    typeof session.last_seq === "number" ? session.last_seq : await latestAgentSeq(ctx, session.id);
  return { sid: session.id, cursor };
}

async function listAgentSessions(ctx: Ctx, code: string): Promise<AgentSessionView[]> {
  const list = await request<AgentSessionView[]>({
    base: baseOf(ctx),
    token: requireToken(ctx),
    path: `/canvas/${encodeURIComponent(code)}/agent/sessions`,
    fetchImpl: ctx.fetchImpl,
  });
  return Array.isArray(list) ? list : [];
}

function agentAttachments(args: Args) {
  const ids = listFlag(args.flags, "attach");
  if (
    args.flags.attach === true ||
    ids.length > 12 ||
    ids.some((id) => !/^[1-9]\d{0,19}$/.test(id))
  )
    throw new APIError(-1, "--attach 需要素材 ID，每条消息最多传入 12 个 ID", 2);
  return ids.length ? { attachments: ids.map((asset_id) => ({ asset_id })) } : {};
}

/** 一条事件的人类可读形态：回复原文输出，审批把 payload 原样摊开，工具调用给一行摘要。 */
function formatAgentEvent(e: AgentEventView): string {
  const data = (e.data ?? {}) as Record<string, unknown>;
  if (e.type === "approval.created") {
    const id = typeof data.id === "string" ? data.id : "";
    const payload = (data.payload ?? {}) as Record<string, unknown>;
    const items = Array.isArray(payload.items) ? payload.items : [];
    const lines = [`⚠ 需要你确认${id ? `（审批编号 ${id}）` : ""}`];
    if (typeof payload.reason === "string" && payload.reason)
      lines.push(`  理由：${payload.reason}`);
    for (const line of describeApprovalItems(payload)) lines.push(`  ${line}`);
    if (id) {
      // 带上 --session：批准前才能把条目内容再摊开给人看一遍
      const at = e.session_id ? ` --session ${e.session_id}` : "";
      const all = items.map((_, i) => i).join(",");
      lines.push(`  全部批准：vc agent approve ${id}${at}${items.length ? ` --items ${all}` : ""}`);
      if (items.length > 1) lines.push(`  只批一条：vc agent approve ${id}${at} --items 0`);
      lines.push(`  全部驳回：vc agent approve ${id} --reject`);
    }
    return lines.join("\n");
  }
  // 同一次审批的「已处理」：别把它再渲染成一遍「需要确认」，那会让人以为有两件事要批。
  if (e.type === "approval.decided") {
    const status = typeof data.status === "string" ? data.status : "";
    const kind = typeof data.kind === "string" ? data.kind : "";
    const label =
      status === "rejected" ? "已驳回" : status === "approved" ? "已批准" : status || "已处理";
    return `  · 审批${label}${kind ? `（${kind}）` : ""}`;
  }
  // 助手的回答就是这轮要的东西，原样输出，不截断。
  const text = typeof data.text === "string" ? data.text.trim() : "";
  if (e.type === "message.done") return text ? `\n${text}\n` : "";
  // 用量只报花了多少积分，其余状态不刷屏。
  if (e.type === "run.usage") {
    const spent = typeof data.spent_credits === "number" ? data.spent_credits : 0;
    return spent > 0 ? `  · 本轮花费 ${spent} 积分` : "";
  }
  if (e.type === "run.status" || e.type === "message.user") return "";
  const name = typeof data.name === "string" ? data.name : "";
  const summary = typeof data.summary === "string" ? data.summary : "";
  const label = typeof data.label === "string" ? data.label : "";
  const tail = (summary || label || name).slice(0, 120);
  return tail ? `  · ${e.type}  ${tail}` : `  · ${e.type}`;
}

/** 运行已经不会再往前走的状态（成功，或需要人介入才能继续的失败 / 暂停）。 */
const AGENT_TERMINAL_STATUSES = new Set<string>([
  "succeeded",
  "failed",
  "timeout",
  "interrupted",
  "budget_exhausted",
  "step_limit",
  "canceled",
  "expired",
]);

/**
 * --watch 的收尾判断，一批批事件喂进来：
 * - 只有 approval.created 才算「在等人批准」；approval.decided 是已经处理完的，不能当成等待；
 * - run.status 进入终态就是结束；
 * - 回复了文字不等于结束：它后面常常还有工具调用。所以文字只在「没有未结束的工具调用」且
 *   「之后连续两轮拉取都没有新事件」时才当作收尾。
 */
class AgentWatch {
  waiting = false;
  finished: string | null = null;
  private readonly openTools = new Set<string>();
  private sawText = false;
  private quietPolls = 0;

  /** 喂入一批事件（可能为空），返回现在能不能收尾。 */
  feed(events: AgentEventView[]): boolean {
    this.quietPolls = events.length ? 0 : this.quietPolls + 1;
    for (const e of events) this.observe(e);
    if (this.waiting || this.finished) return true;
    return this.sawText && this.openTools.size === 0 && this.quietPolls >= 2;
  }

  private observe(e: AgentEventView): void {
    const data = (e.data ?? {}) as Record<string, unknown>;
    const id = typeof data.id === "string" ? data.id : "";
    switch (e.type) {
      case "approval.created":
        this.waiting = true;
        break;
      case "tool.start":
        if (id) this.openTools.add(id);
        break;
      case "tool.end":
        if (id) this.openTools.delete(id);
        break;
      case "message.done":
        if (typeof data.text === "string" && data.text.trim() !== "") this.sawText = true;
        break;
      case "run.status":
        if (typeof data.status === "string" && AGENT_TERMINAL_STATUSES.has(data.status))
          this.finished = data.status;
        break;
      default:
        break;
    }
  }
}

/** 拉一页事件；返回下一页的游标与这一页。 */
async function pullAgentEvents(
  ctx: Ctx,
  sid: string,
  after: number,
): Promise<{ events: AgentEventView[]; next: number }> {
  const token = requireToken(ctx);
  const events = await request<AgentEventView[]>({
    base: baseOf(ctx),
    token,
    method: "GET",
    path: `/agent/sessions/${encodeURIComponent(sid)}/events?after=${after}&limit=100`,
    fetchImpl: ctx.fetchImpl,
  });
  const list = Array.isArray(events) ? events : [];
  const next = list.length > 0 ? list[list.length - 1]!.seq : after;
  return { events: list, next };
}

/**
 * 把一件事交给小镜做。默认只提交并打印运行编号；加 --watch 才跟到它有回复或需要审批。
 *
 * 为什么默认不跟：Agent 跑一轮可能几分钟、也可能停在审批等一整夜，脚本不该被它挂住。
 */
export async function agentRun(ctx: Ctx, args: Args): Promise<void> {
  const code = args.positional[0];
  const message = args.flags.message;
  if (!code || typeof message !== "string" || !message.trim()) {
    throw new APIError(-1, `用法：vc ${AGENT_USAGE}`, 2);
  }

  const requested =
    typeof args.flags.session === "string" && args.flags.session ? args.flags.session : undefined;
  const body: Record<string, unknown> = { message, ...agentAttachments(args) };
  const selection = listFlag(args.flags, "selection");
  if (selection.length) body.selection = selection;
  if (args.flags.budget !== undefined) {
    body.budget_credits = toInt(args.flags.budget, "budget", { min: 0 });
  }
  const interval = intFlag(args.flags, "interval", 2, { min: 1 });
  const timeout = intFlag(args.flags, "timeout", 600, { min: 1 });

  const snapshot = await tryCollaboration(ctx, code);
  if (snapshot?.role === "viewer") {
    throw new CollaborationError(-1, "仅查看成员不能启动小镜", 403, "permission_denied");
  }
  if (snapshot) body.collaboration_epoch = encodeWireVersion(snapshot.epoch);
  const { sid, cursor: startCursor } = await resolveAgentSession(
    ctx,
    snapshot?.canvas.id ?? code,
    requested,
  );
  let cursor = startCursor;
  const token = requireToken(ctx);
  const run = await request<AgentRunView>({
    base: baseOf(ctx),
    token,
    method: "POST",
    path: `/agent/sessions/${encodeURIComponent(sid)}/runs`,
    body,
    fetchImpl: ctx.fetchImpl,
  });

  if (!boolFlag(args.flags, "watch")) {
    emit(
      ctx,
      { session_id: sid, run },
      [
        `已交给小镜：运行 ${run.id}（会话 ${sid}）`,
        "加 --watch 可以跟到它跑完；停下来要你确认时，用 vc agent approve 决策。",
      ].join("\n"),
    );
    return;
  }

  const deadline = Date.now() + timeout * 1000;
  const printed: AgentEventView[] = [];
  const watch = new AgentWatch();

  while (true) {
    // 网络抖动与 5xx 退避重试到截止时间：运行在服务端照常继续，不该因为一次拉取失败就丢了跟踪
    const { events, next } = await withRetry(() => pullAgentEvents(ctx, sid, cursor), {
      deadline,
      onRetry: (err, n, wait) => ctx.stderr(retryNotice(err, n, wait)),
    });
    cursor = next;
    for (const e of events) {
      printed.push(e);
      const line = formatAgentEvent(e);
      if (line) ctx.stderr(line);
    }
    if (watch.feed(events)) break;
    if (Date.now() >= deadline) {
      emit(
        ctx,
        { session_id: sid, run, events: printed, waiting: false },
        `等到 ${timeout} 秒还没结束，运行 ${run.id} 仍在服务端继续。\n继续看：vc agent events ${sid} --after ${cursor}`,
      );
      process.exitCode = 3;
      return;
    }
    await new Promise((resolve) => setTimeout(resolve, interval * 1000));
  }

  const tail = watch.waiting
    ? "它停下来等你确认：按上面列出的编号批准，或 --reject 驳回。"
    : watch.finished
      ? `运行已结束（${watch.finished}）。`
      : "小镜已回复。它若还在后台收尾，用 vc agent events 接着看。";
  emit(ctx, { session_id: sid, run, events: printed, waiting: watch.waiting }, tail);
}

/** 看一条会话里已经发生的事；--after 从某个序号之后接着看。 */
export async function agentEvents(ctx: Ctx, args: Args): Promise<void> {
  const sid = args.positional[0];
  if (!sid) throw new APIError(-1, `用法：vc ${AGENT_USAGE}`, 2);
  const after = args.flags.after === undefined ? 0 : toInt(args.flags.after, "after", { min: 0 });
  const { events, next } = await pullAgentEvents(ctx, sid, after);
  emit(
    ctx,
    { session_id: sid, events, next },
    events.length ? events.map(formatAgentEvent).join("\n") : "这段时间没有新事件。",
  );
}

/** 在会话事件里找这次审批的 approval.created，拿到条目内容；找不到返回 null。 */
async function findApprovalPayload(
  ctx: Ctx,
  sid: string,
  aid: string,
): Promise<Record<string, unknown> | null> {
  let cursor = 0;
  for (let page = 0; page < 200; page++) {
    const { events, next } = await pullAgentEvents(ctx, sid, cursor);
    if (!events.length) return null;
    for (const e of events) {
      const data = (e.data ?? {}) as Record<string, unknown>;
      if (e.type === "approval.created" && data.id === aid) {
        return (data.payload ?? {}) as Record<string, unknown>;
      }
    }
    cursor = next;
  }
  return null;
}

/**
 * 对一次审批下决定。默认整体通过；--items 只批准指定序号，--reject 整体驳回。
 *
 * 批准之前先把「将批准什么」摊开：后端没有按审批编号查详情的接口，条目只在会话事件里，
 * 所以要用 --session 指明会话才能展示（agent run --watch 打印的命令里已经带上了）。
 * 在终端里还要敲 yes 才提交；脚本 / --json 下只展示不拦——这是约定性的软闸门，不是安全边界。
 */
export async function agentApprove(ctx: Ctx, args: Args): Promise<void> {
  const aid = args.positional[0];
  if (!aid) throw new APIError(-1, `用法：vc ${AGENT_USAGE}`, 2);
  const reject = boolFlag(args.flags, "reject");
  const items = listFlag(args.flags, "items").map((raw) => toInt(raw, "items", { min: 0 }));
  const body: Record<string, unknown> = { decision: reject ? "reject" : "approve" };
  if (!reject && items.length) {
    body.items = items.map((index) => ({ index, approve: true }));
  }
  if (typeof args.flags.answer === "string") body.answer = args.flags.answer;

  const token = requireToken(ctx);
  if (!reject) {
    const sid = typeof args.flags.session === "string" ? args.flags.session : "";
    const payload = sid ? await findApprovalPayload(ctx, sid, aid) : null;
    if (payload) {
      const lines = describeApprovalItems(payload);
      const picked = items.length ? lines.filter((_, i) => items.includes(i)) : lines;
      ctx.stderr(`将批准审批 ${aid}：`);
      if (typeof payload.reason === "string" && payload.reason)
        ctx.stderr(`  理由：${payload.reason}`);
      for (const line of picked) ctx.stderr(`  ${line}`);
      if (!picked.length) ctx.stderr("  （没有对应的条目，请核对 --items 序号）");
    } else {
      ctx.stderr(
        sid
          ? `没有在会话 ${sid} 里找到审批 ${aid} 的条目内容，无法在批准前展示。`
          : "没有指定 --session，无法在批准前展示条目内容（加 --session <会话编号> 可以展示）。",
      );
    }
    // 人在终端里：再确认一次；脚本 / --json：已经把内容打印到 stderr，不拦
    if (!ctx.json && process.stdin.isTTY && process.stderr.isTTY) {
      await askYes(ctx, "确认批准？输入 yes 回车，其它任意输入取消：", "已取消，没有提交审批决定");
    }
  }

  const view = await request<Record<string, unknown>>({
    base: baseOf(ctx),
    token,
    method: "POST",
    path: `/agent/approvals/${encodeURIComponent(aid)}/decision`,
    body,
    fetchImpl: ctx.fetchImpl,
  });
  emit(
    ctx,
    view,
    `已提交决定：${reject ? "驳回" : "批准"}${items.length ? `（条目 ${items.join(",")}）` : ""}。`,
  );
}

/** 停掉一轮运行：Agent 不再继续，已经提交的生成任务照常完成。 */
export async function agentCancel(ctx: Ctx, args: Args): Promise<void> {
  const rid = args.positional[0];
  if (!rid) throw new APIError(-1, `用法：vc ${AGENT_USAGE}`, 2);
  const token = requireToken(ctx);
  const view = await request<Record<string, unknown>>({
    base: baseOf(ctx),
    token,
    method: "POST",
    path: `/agent/runs/${encodeURIComponent(rid)}/cancel`,
    fetchImpl: ctx.fetchImpl,
  });
  emit(ctx, view, `已取消运行 ${rid}。`);
}

export async function agentSessions(ctx: Ctx, args: Args): Promise<void> {
  const [code] = args.positional;
  if (!code || args.positional.length !== 1)
    throw new APIError(-1, "用法：agent sessions <画布>", 2);
  const sessions = await listAgentSessions(ctx, code);
  emit(
    ctx,
    sessions,
    sessions.map(({ id, title }) => `${id}\t${title}`).join("\n") || "还没有小镜会话",
  );
}

export async function agentHistory(ctx: Ctx, args: Args): Promise<void> {
  const [sid] = args.positional;
  if (!sid || args.positional.length !== 1)
    throw new APIError(-1, "用法：agent history <会话> [--before N --limit N]", 2);
  const view = await request<{ events: AgentEventView[]; has_more: boolean }>({
    base: baseOf(ctx),
    token: requireToken(ctx),
    path: `/agent/sessions/${encodeURIComponent(sid)}/history`,
    query: {
      before: intFlag(args.flags, "before", undefined, { min: 0 }),
      limit: intFlag(args.flags, "limit", undefined, { max: 200 }),
    },
    fetchImpl: ctx.fetchImpl,
  });
  const next = view.has_more ? view.events[0]?.seq : undefined;
  const lines = view.events.map(formatAgentEvent).filter(Boolean);
  if (next !== undefined) lines.push(`查看更多：vc agent history ${sid} --before ${next}`);
  emit(ctx, { ...view, before: next }, lines.join("\n") || "没有聊天记录");
}

export async function agentControl(
  ctx: Ctx,
  args: Args,
  action: "interject" | "resume" | "undo",
): Promise<void> {
  const [rid] = args.positional;
  const usage = `用法：agent ${action} <运行编号>`;
  if (!rid || args.positional.length !== 1) throw new APIError(-1, usage, 2);
  const token = requireToken(ctx);
  let body: Record<string, unknown> = {};
  if (action === "interject") {
    const message = stringFlag(args.flags, "message", `${usage} --message <消息>`).trim();
    if (!message || message.length > 20_000) throw new APIError(-1, "消息长度须在 1–20000 之间", 2);
    body = { message, ...agentAttachments(args) };
  } else if (action === "resume") {
    body = { add_budget: intFlag(args.flags, "add-budget", 0, { min: 0, max: 100_000 }) };
  } else {
    await askYes(ctx, `撤销小镜运行 ${rid} 对画布的修改？输入 yes 确认：`, "已取消撤销");
  }
  const result = await request<unknown>({
    base: baseOf(ctx),
    token,
    method: "POST",
    path: `/agent/runs/${encodeURIComponent(rid)}/${action}`,
    body,
    fetchImpl: ctx.fetchImpl,
  });
  const messages = { interject: "已追加指令", resume: "已请求续跑", undo: "已执行撤销检查" };
  emit(
    ctx,
    result ?? {},
    `${messages[action]}：${rid}${result ? `\n${JSON.stringify(result, null, 2)}` : ""}`,
  );
}
