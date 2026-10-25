/**
 * 前端用的 REST 接口背后的业务：会话、事件回放、聊天记录、运行的发起和控制、撤销、审批决定。
 * 对应 Go 的 AgentService 里这些方法，规则一一照搬；运行和审批的状态机在引擎里，这里是它的「入口」：
 * 校验归属和参数、向 Go 要它才有的东西（画布归属、模型清单、消息里引用的有效性、撤销）、把结果变成对外的视图。
 *
 * 所有 id 参数都是十进制字符串；路径里的编码串由 HTTP 层先解码。
 */
import type { AgentEngine, ApprovalView } from "../engine/engine.ts";
import { fromPlatform } from "../engine/engine.ts";
import { AgentError, Err } from "../engine/errors.ts";
import { isActive } from "../engine/status.ts";
import type { IdCodec } from "../ids.ts";
import type { AgentModel, ApprovalDecision, Platform, UndoResult } from "../platform-client.ts";
import { NotFoundError } from "../store/errors.ts";
import type { AgentStore } from "../store/store.ts";
import type { AgentEventRecord, Run, Session } from "../store/types.ts";
import { eventView, runView, sessionView, type EventView, type HistoryView, type RunView, type SessionView } from "./views.ts";

const DEFAULT_TITLE = "新对话";
const MAX_SESSIONS_PER_CANVAS = 50;
const MAX_EVENT_PAGE = 200;
const DEFAULT_HISTORY_PAGE = 100;
const MAX_HISTORY_CHUNKS = 20;
const MAX_TITLE = 40;

export interface ApiOptions {
  store: AgentStore;
  engine: AgentEngine;
  platform: Platform;
  codec: IdCodec;
  now?: () => number;
}

export interface CreateSessionRequest {
  title?: string;
  mode?: string;
  model_key?: string;
}

export interface StartRunRequest {
  message: string;
  mode?: string;
  selection?: string[];
  viewport?: unknown;
  budget_credits?: number | null;
  agent_model_key?: string;
}

/** 首条消息 / 标题：空白压成一个空格，最多 40 字；清空后返回 def */
export function cleanTitle(raw: string, def: string): string {
  const t = Array.from(raw.split(/\s+/).filter(Boolean).join(" ")).slice(0, MAX_TITLE).join("");
  return t || def;
}

export class AgentApiService {
  readonly #o: ApiOptions;

  constructor(o: ApiOptions) {
    this.#o = o;
  }

  // ---- 会话 ----

  async models(): Promise<AgentModel[]> {
    return (await this.#platform(() => this.#o.platform.models())) ?? [];
  }

  async listSessions(userId: string, canvasId: string): Promise<SessionView[]> {
    await this.#platform(() => this.#o.platform.checkCanvas(userId, canvasId));
    return this.#o.store.listSessions(userId, canvasId).map((s) => sessionView(this.#o.codec, s));
  }

  async createSession(userId: string, canvasId: string, req: CreateSessionRequest): Promise<SessionView> {
    await this.#platform(() => this.#o.platform.checkCanvas(userId, canvasId));
    // 每个画布最多 50 个会话，避免历史无限增长
    if (this.#o.store.countSessions(userId, canvasId) >= MAX_SESSIONS_PER_CANVAS) throw new AgentError(60010, 409, "这个画布的会话数量已达上限");
    const s = this.#o.store.createSession({ userId, canvasId, title: cleanTitle(req.title ?? "", DEFAULT_TITLE), mode: req.mode || "all", modelKey: req.model_key ?? "" });
    return sessionView(this.#o.codec, s);
  }

  async renameSession(userId: string, sessionId: string, title: string): Promise<SessionView> {
    const t = cleanTitle(title, "");
    if (!t) throw Err.invalidParams("标题不能为空");
    const store = this.#o.store;
    this.#session(userId, sessionId);
    store.updateSession(userId, sessionId, { title: t });
    return sessionView(this.#o.codec, store.getSession(userId, sessionId));
  }

  /** 删除会话（软删除）。这个会话正占着画布时不允许，免得 Agent 往一个看不见的会话里继续写 */
  async deleteSession(userId: string, sessionId: string): Promise<void> {
    const sess = this.#session(userId, sessionId);
    const active = this.#o.store.activeRun(sess.canvasId);
    if (active && active.sessionId === sessionId) throw Err.runActive();
    this.#o.store.deleteSession(userId, sessionId);
  }

  // ---- 事件和历史 ----

  async events(userId: string, sessionId: string, after: number, limit: number): Promise<EventView[]> {
    this.#session(userId, sessionId);
    const n = limit <= 0 || limit > MAX_EVENT_PAGE ? MAX_EVENT_PAGE : limit;
    return this.#o.store.listEvents(sessionId, after, n).map((e) => eventView(this.#o.codec, e));
  }

  /**
   * 聊天记录，从最新往回翻页：before 是已有的最小序号（0 取最新一页）。一页的起点对齐到一轮的开头
   * （用户发起的 message.user，插话不算），不把一轮拦腰截断：前端按一轮的事件折叠运行状态，拦腰截断会让最旧那轮缺开头。
   * 所以一页可能比 limit 多（一轮比 limit 长时）。has_more 说明更早还有没有。
   */
  async history(userId: string, sessionId: string, before: number, limit: number): Promise<HistoryView> {
    this.#session(userId, sessionId);
    const n = limit <= 0 || limit > MAX_EVENT_PAGE ? DEFAULT_HISTORY_PAGE : limit;
    let [events, older] = this.#historyChunk(sessionId, before, n);
    for (let i = 0; i < MAX_HISTORY_CHUNKS; i++) {
      const at = firstTurnStart(events);
      if (at >= 0) {
        older = older || at > 0; // 起点之前的残余留给下一页（它们是更早一轮的结尾）
        events = events.slice(at);
        break;
      }
      if (!older || events.length === 0) break;
      const [more, moreOlder] = this.#historyChunk(sessionId, events[0].seq, n);
      events = [...more, ...events];
      older = moreOlder;
    }
    return { events: events.map((e) => eventView(this.#o.codec, e)), has_more: older };
  }

  /** 读 before 之前最新的 limit 条事件，并说明更早的还有没有（多读一条来判断） */
  #historyChunk(sessionId: string, before: number, limit: number): [AgentEventRecord[], boolean] {
    const items = this.#o.store.listEventsBefore(sessionId, before, limit + 1);
    return items.length > limit ? [items.slice(1), true] : [items, false];
  }

  // ---- 运行 ----

  async startRun(userId: string, sessionId: string, req: StartRunRequest): Promise<RunView> {
    const sess = this.#session(userId, sessionId);
    const message = req.message.trim();
    if (!message) throw Err.invalidParams("消息不能为空");
    // 消息里的引用（节点、模型、技能）必须有效；选中的节点里已不存在的去掉
    const selection = await this.#platform(() => this.#o.platform.checkMessage(userId, sess.canvasId, message, req.selection ?? []));
    const modelKey = await this.#pickModel(req.agent_model_key || sess.modelKey);
    const run = await this.#o.engine.startRun({
      userId, sessionId, message, mode: req.mode || undefined, modelKey, selection, budgetCredits: req.budget_credits ?? 0,
    });
    return runView(this.#o.codec, run);
  }

  async interject(userId: string, runId: string, message: string): Promise<void> {
    const run = this.#run(userId, runId);
    const msg = message.trim();
    if (!msg) throw Err.invalidParams("消息不能为空");
    if (run.status !== "running") throw Err.state();
    await this.#platform(() => this.#o.platform.checkMessage(userId, run.canvasId, msg, []));
    await this.#o.engine.interject(userId, runId, msg);
  }

  async cancel(userId: string, runId: string): Promise<RunView> {
    return runView(this.#o.codec, await this.#o.engine.cancel(userId, runId));
  }

  async resume(userId: string, runId: string, addBudget: number): Promise<RunView> {
    return runView(this.#o.codec, await this.#o.engine.resume(userId, runId, addBudget));
  }

  async decide(userId: string, approvalId: string, req: ApprovalDecision): Promise<ApprovalView> {
    return this.#o.engine.decide(userId, approvalId, req);
  }

  /**
   * 撤销本轮对画布的改动。运行得是自己的、已经结束（还在跑或在等用户时撤销，Agent 马上又会写进来）；
   * 改动记录在 Go，经平台撤销。真的写了撤销（revision 非零）才算用户撤销，记 run.undone：它是用户不满意的强信号。
   */
  async undo(userId: string, runId: string): Promise<UndoResult> {
    const run = this.#run(userId, runId);
    if (isActive(run.status)) throw Err.state();
    const sess = this.#session(userId, run.sessionId);
    const res = await this.#platform(() =>
      this.#o.platform.undo({ run_id: run.id, user_id: userId, session_id: run.sessionId, canvas_id: run.canvasId, mode: run.mode, model_key: sess.modelKey }),
    );
    if (res.revision !== 0) this.#o.engine.recordEvent(run, "run.undone", { reverted: res.reverted, skipped: res.skipped.length });
    return res;
  }

  // ---- 内部 ----

  /** 选本轮用的 Agent 模型：优先 want，其次清单第一个；want 不在已发布清单里是 60002 */
  async #pickModel(want: string): Promise<string> {
    const list = await this.models();
    if (list.length === 0) throw Err.modelNA();
    if (!want) return list[0].key;
    if (!list.some((m) => m.key === want)) throw Err.modelNA();
    return want;
  }

  #session(userId: string, sessionId: string): Session {
    try {
      return this.#o.store.getSession(userId, sessionId);
    } catch (e) {
      if (e instanceof NotFoundError) throw Err.sessionMissing();
      throw e;
    }
  }

  #run(userId: string, runId: string): Run {
    try {
      return this.#o.store.getRun(userId, runId);
    } catch (e) {
      if (e instanceof NotFoundError) throw Err.runMissing();
      throw e;
    }
  }

  /** 调平台：Go 的业务错误（画布不存在、引用不合法……）原样变成业务错误，连不上是「Agent 暂不可用」 */
  async #platform<T>(fn: () => Promise<T>): Promise<T> {
    try {
      return await fn();
    } catch (e) {
      throw fromPlatform(e);
    }
  }
}

/** 第一个「一轮开头」事件的下标，没有返回 -1：用户发起的 message.user，运行中的插话（steer）不算 */
function firstTurnStart(events: AgentEventRecord[]): number {
  return events.findIndex((e) => e.type === "message.user" && !(e.payload as { steer?: boolean } | null)?.steer);
}
