/**
 * 运行引擎：会话、运行、审批的状态机和事件，加上 Pi Durable 驱动的对话。
 * 取代 Go 里 run.go / approval.go / bridge.go / runtime.go 的编排；钱、画布、生成任务、素材仍在 Go，经平台 API 调用。
 *
 * 一个会话对应一个 Pi 会话（对话历史归 Pi）；一次运行 = 向这个会话提交一条用户输入，直到 Pi 空闲。
 * 持久化的事件（run.status、message.done、tool.*、approval.*、run.usage、plan.updated、message.user）由引擎写进 AgentStore
 * 并通过 onEvent 交出去；文字增量（message.delta / thinking.delta）只交出去不落库。
 */
import { BACKGROUND_CONTEXT as context } from "@earendil-works/chord/context";
import type { AssistantMessage, Message } from "@earendil-works/pi-ai";
import {
  CompactionTask, GenerationTask, Harness, ProviderDoc, createRegistry, defineExtension, hook, watchEvents,
  type Conversation, type ConversationId, type Extension, type Storage, type Submission, type ToolRegistration,
} from "@earendil-works/pi-durable";
import { SUMMARY_SYSTEM_PROMPT, buildPrompt, compactionLimits, serializeMessages, summaryMaxTokens } from "../compaction.ts";
import { createCodemode } from "../codemode.ts";
import { createToolsExtension, type ToolHost, type ToolRun } from "../durable-tools.ts";
import type { IdCodec } from "../ids.ts";
import { PROVIDER_ID, createModelRegistry, createRunContexts, runContextExtension, type RunContexts } from "../model-provider.ts";
import { PlatformError, type ApprovalDecision, type ApprovalExecution, type ApprovalKind, type ApprovalOutcome, type Platform, type PlatformUsage, type RunContext, type RunSetup, type ToolDecl } from "../platform-client.ts";
import { DuplicateError, NotFoundError, StateConflictError } from "../store/errors.ts";
import type { AgentStore } from "../store/store.ts";
import type { Approval, Run, Session } from "../store/types.ts";
import { AgentError, Err, OVER_BUDGET_CODE } from "./errors.ts";
import { ACTIVE_STATUSES, isActive, statusEvent } from "./status.ts";

const DEFAULT_TITLE = "新对话";
const DEFAULT_MAX_STEPS = 40;
const APPROVAL_TTL_MS = 24 * 3600 * 1000;
const DELTA_FLUSH_MS = 50;
const OUTPUT_TRUNCATED = "模型的回答被截断：达到了单次输出的上限（思考内容也占用这部分额度）。请把任务拆小一点再试，或请管理员调大这个模型的输出上限。";
/** Pi 提交流式中间状态的最小间隔。每次提交是一次存储写入，所以不要太密；前端的增量就是按它出现的 */
const PARTIAL_INTERVAL_MS = 100;
const MAX_ERROR_CHARS = 200;
const SETTLE_POLL_MS = 25;
const SETTLE_POLL_MAX = 80;
/** 审批已决定、占用了额度的状态：本轮已花积分里要算上它们的预估 */
const SPENDING_APPROVAL = ["approved", "partially_approved", "executed", "failed"];
const WAITING = ["waiting_approval", "waiting_input"];
/** 可以点「继续」的状态 */
const RESUMABLE = ["interrupted", "budget_exhausted", "step_limit"];
const CONTINUE_TEXT = "请继续刚才的任务。";
/** 运行被中断时，最后一步的工具结果可能丢了，而那一步可能已经执行（Pi 补上的结果只说「被中止」，没说这一点） */
const INTERRUPTED_TEXT = "上一次运行被中断了，最后一步的工具结果丢失，那一步可能已经执行。请先读取画布或任务确认现状，再继续刚才的任务。";
const APPROVAL_NUDGE = "（用户已处理上面的请求，请根据结果继续）";
const DEFAULT_LEASE_INTERVAL_MS = 10_000;
const DEFAULT_LEASE_TTL_MS = 30_000;
const DEFAULT_RESUME_WINDOW_MS = 5 * 60_000;
const EXECUTE_ATTEMPTS = 3;
const EXECUTE_RETRY_MS = 100;

/** 引擎发出的事件。seq 为 0 表示临时事件（文字增量），不落库、不参与回放 */
export interface PublishedEvent {
  id: string;
  sessionId: string;
  runId: string;
  userId: string;
  canvasId: string;
  seq: number;
  type: string;
  payload: unknown;
  createdAt: number;
}

export interface EngineOptions {
  store: AgentStore;
  platform: Platform;
  /** Go 的桥地址（模型代理和平台接口） */
  baseUrl: string;
  serviceToken: string;
  /** Pi Durable 的存储（生产里是和应用数据同一个文件的 SQLite，表名带 pi_ 前缀） */
  storage: Storage;
  /** 对外 ID 的编码，事件内容里的审批 id、运行 id 用它（和 Go 的 idcodec 一致） */
  codec: IdCodec;
  onEvent?: (ev: PublishedEvent) => void;
  now?: () => number;
  approvalTtlMs?: number;
  defaultMaxSteps?: number;
  /** 运行期间每隔多久续一次租约（默认 10 秒）；启动恢复靠它判断停机多久 */
  leaseIntervalMs?: number;
  /** 租约有效期（默认 30 秒） */
  leaseTtlMs?: number;
  /** 启动时没跑完的运行，停机不超过这个时间就自动续跑，超过就标为中断（默认 5 分钟） */
  resumeWindowMs?: number;
}

export interface StartRunRequest {
  userId: string;
  sessionId: string;
  message: string;
  /** 不传沿用会话的 */
  mode?: string;
  /** 调用方（REST 层）已经选好的 Agent 模型 */
  modelKey: string;
  selection?: string[];
  /** 本轮积分预算，不传或 0 表示不限制 */
  budgetCredits?: number;
  maxSteps?: number;
}

/** 一个正在进行的运行片段 */
interface Active {
  run: Run;
  convId: string;
  rc: RunContext;
  conv: Conversation;
  paused: boolean;
  /** 已经决定收尾：之后到的插话不能再提交，否则 Pi 开始了新一轮而运行已经标成完成 */
  finishing: boolean;
  /** 插话和收尾互斥的锁 */
  lock: Promise<unknown>;
  steps: number;
  maxSteps: number;
  /** 模型调用 id（响应头），按请求顺序，assistant 消息结束时取走一个 */
  callIds: string[];
  /** 摘要调用的 id，写完摘要后单独结算 */
  summaryCallIds: string[];
  /** 摘要失败后这个运行片段里不再压缩，免得每个回合都白付一次钱 */
  compactionDisabled: boolean;
  /** 启动恢复后自动续跑中：已经发过「恢复中」，第一个别的事件到来时补一条 running */
  recovering: boolean;
  /** 恢复前就已经落库的 tool.start 的调用 id：续跑时被重放的工具不再发第二遍（只抑制一次，模型可能复用 id） */
  startedBefore: Set<string>;
  modelError?: { code?: number; status?: number; message?: string };
  drive?: Promise<void>;
}

const truncate = (s: string, n: number) => (Array.from(s).length > n ? Array.from(s).slice(0, n).join("") : s);
const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));

/** 审批的对外视图（事件和决定接口共用）：id 是 idcodec 编码串 */
export interface ApprovalView {
  id: string;
  run_id: string;
  kind: string;
  status: string;
  payload: unknown;
  quote_credits: number;
  decision: unknown;
  expires_at: string;
}

/** Go 平台的业务错误原样变成引擎的业务错误；连不上、5xx 是「Agent 暂不可用」 */
export function fromPlatform(e: unknown): Error {
  if (e instanceof PlatformError && e.code !== undefined && e.status >= 400 && e.status < 500) return new AgentError(e.code, e.status, e.message);
  if (e instanceof PlatformError) return Err.unavailable();
  return e as Error;
}

export class AgentEngine {
  readonly #o: EngineOptions;
  readonly #now: () => number;
  #harness!: Harness;
  readonly #contexts: RunContexts = createRunContexts();
  readonly #active = new Map<string, Active>(); // 会话 id（Pi）→ 片段
  readonly #byRun = new Map<string, Active>();
  readonly #decls = new Map<string, ToolDecl>();
  /** "提供方/模型" → 压缩的触发线 */
  readonly #compaction = new Map<string, { reserveTokens: number; keepRecentTokens: number }>();
  readonly #toolRegs = new Map<string, ToolRegistration>();
  readonly #registry = createRegistry();
  readonly #drives = new Set<Promise<void>>();
  readonly #watched = new Set<string>();
  /** 会话 → 已发过 message.done、但文字增量的 watcher 还没处理到它的 message_end 的回答数 */
  readonly #answered = new Map<string, number>();
  /** 会话 → 丢掉 watcher 里攒着的增量 */
  readonly #dropDeltas = new Map<string, () => void>();
  readonly #stops: Array<() => Promise<unknown>> = [];
  #models!: ReturnType<typeof createModelRegistry>;
  #toolsKey = "";
  #closing = false;
  #leaseTimer: ReturnType<typeof setInterval> | undefined;

  private constructor(o: EngineOptions) {
    this.#o = o;
    this.#now = o.now ?? Date.now;
  }

  static async open(o: EngineOptions): Promise<AgentEngine> {
    const e = new AgentEngine(o);
    await e.#init();
    return e;
  }

  async #init(): Promise<void> {
    this.#models = createModelRegistry({
      baseUrl: this.#o.baseUrl, serviceToken: this.#o.serviceToken, contexts: this.#contexts,
      onCall: ({ sessionId, callId, purpose }) => {
        const a = this.#active.get(this.#contexts.conversationOf(sessionId ?? "") ?? "");
        // 摘要不是助手的话，没有对应的 message.done：它的调用 id 单独放，免得错配到下一条对话消息上
        if (a) (purpose === "summary" ? a.summaryCallIds : a.callIds).push(callId);
      },
      onError: ({ sessionId, status, code, purpose }) => {
        const a = this.#active.get(this.#contexts.conversationOf(sessionId ?? "") ?? "");
        if (a && purpose === "main") a.modelError = { ...a.modelError, status, code };
      },
    });
    this.#registry.install(runContextExtension(this.#contexts));
    this.#registry.install(this.#hooksExtension());
    this.#registry.install(this.#compactionExtension());
    const codemode = createCodemode(this.host);
    for (const t of codemode.tools ?? []) this.#toolRegs.set(t.name, t);
    this.#registry.install(codemode);
    this.#harness = await Harness.open(this.#o.storage, {
      models: this.#models.models, registry: this.#registry,
      // 重试策略在 Go；同一轮的工具调用一个个执行；插话用官方效果（排队，下一次请求前一起带上）
      // 压缩的触发线按模型的上下文窗口算（窗口小的模型收紧）；后台压缩不用：写摘要要有运行的上下文，运行结束后没有
      settings: () => ({
        retry: { enabled: false }, toolExecution: "sequential", steeringMode: "all", progress: { partialIntervalMs: PARTIAL_INTERVAL_MS },
        compaction: { enabled: true, backgroundTokens: 0, modelOverrides: Object.fromEntries(this.#compaction) },
      }),
    }, context);
    this.#leaseTimer = setInterval(() => this.#touchLeases(), this.#o.leaseIntervalMs ?? DEFAULT_LEASE_INTERVAL_MS);
    this.#leaseTimer.unref();
    await this.#recover();
  }

  // ---- 启动恢复（设计文档 20.6） ----

  /**
   * 服务启动时处理上次没跑完的运行（状态是 queued / running）。Pi 的调度是全局的，不能按会话选择性续跑，所以顺序固定：
   * 先把窗口内要续跑的都接好，再把过期的收掉，最后才启动调度；这之前不接受任何请求。
   *  - 停机不超过窗口、任务还在、没被重启上限挡住 → 自动续跑，先发「恢复中」（只是事件，库里仍是 running，画布没有被放走）；
   *  - 否则 → 收掉 Pi 里没跑完的任务，标为中断，用户点「继续」；
   *  - 等审批、等回答的运行没有活任务，状态在库里，不受影响。
   */
  async #recover(): Promise<void> {
    const store = this.#o.store;
    const runs = store.listRunsByStatus(["queued", "running"]);
    const insp = await this.#harness.inspect(context);
    const liveByConv = new Map<string, typeof insp.tasks[number][]>();
    for (const t of insp.tasks) {
      const c = String(t.record.conversationId);
      liveByConv.set(c, [...(liveByConv.get(c) ?? []), t]);
    }
    const window = this.#o.resumeWindowMs ?? DEFAULT_RESUME_WINDOW_MS;
    const ttl = this.#o.leaseTtlMs ?? DEFAULT_LEASE_TTL_MS;
    const claimed = new Set<string>();
    const resumed: Active[] = [];
    const stale: Array<{ run: Run; convId: string }> = [];

    for (const run of runs) {
      let sess: Session | undefined;
      try {
        sess = store.getSession(run.userId, run.sessionId);
      } catch {
        // 会话已经删了
      }
      const convId = sess?.conversationId ?? "";
      if (convId) claimed.add(convId);
      const tasks = liveByConv.get(convId) ?? [];
      const downtime = this.#now() - (run.leaseUntil ? run.leaseUntil - ttl : run.updatedAt);
      if (sess && convId && tasks.length > 0 && !tasks.some((t) => t.state.kind === "blocked") && downtime <= window) {
        try {
          const rc = this.#rc(run, sess, []);
          const a = await this.#attach(sess, run, rc, await this.#o.platform.prepare(rc, {}));
          a.recovering = true;
          for (const e of store.listEvents(run.sessionId, 0, 100_000)) if (e.runId === run.id && e.type === "tool.start") a.startedBefore.add(String((e.payload as { id: string }).id));
          this.#emit(this.#ev(a), "run.status", statusEvent("recovering"));
          resumed.push(a);
          continue;
        } catch {
          // 平台暂时不可用等：当作过期处理，用户稍后点继续
        }
      }
      stale.push({ run, convId });
    }

    // 过期的：收掉 Pi 里没跑完的任务，标为中断。注意 abort 会启动调度，所以放在窗口内的都接好之后
    for (const { run, convId } of stale) {
      if (convId && liveByConv.has(convId)) await (await this.#convOf(convId))?.abort(context).catch(() => {});
      try {
        store.updateRunIf(run.id, ["queued", "running"], { status: "interrupted" });
        this.#emit({ id: run.id, userId: run.userId, sessionId: run.sessionId, canvasId: run.canvasId }, "run.status", statusEvent("interrupted"));
      } catch {
        // 已经不在 queued / running：别处处理过了；会话删了写不了事件也不要紧
      }
    }
    // 不属于任何待恢复运行的活任务（运行早已结束）：收掉，免得调度一启动它们就自己跑起来
    for (const convId of liveByConv.keys()) if (!claimed.has(convId)) await (await this.#convOf(convId))?.abort(context).catch(() => {});

    this.#harness.resume();
    for (const a of resumed) {
      a.drive = this.#drive(a, async () => undefined);
      this.#drives.add(a.drive);
      void a.drive.finally(() => this.#drives.delete(a.drive!));
    }
  }

  #convOf(convId: string): Promise<Conversation | undefined> | undefined {
    return this.#harness.conversation(Number(convId) as unknown as ConversationId, context);
  }

  /** 给某个运行追加一条事件（撤销等不由引擎自己驱动的动作用），写库并交给 onEvent */
  recordEvent(run: Run, type: string, payload: unknown): void {
    this.#emit({ id: run.id, userId: run.userId, sessionId: run.sessionId, canvasId: run.canvasId }, type, payload);
  }

  /** Pi 里还没跑完的任务数（诊断和测试用）：启动恢复之后，没有对应进行中运行的任务应该是 0 */
  async liveTaskCount(): Promise<number> {
    return (await this.#harness.inspect(context)).tasks.length;
  }

  /** 等所有进行中的运行片段都停下（测试和停机用） */
  async idle(): Promise<void> {
    while (this.#drives.size > 0) await Promise.allSettled([...this.#drives]);
  }

  /**
   * 正常停机：只关闭存储，**不中止运行**，也不把运行标成中断或失败。运行留在原状态，下次启动按窗口判断：
   * 窗口内自动续跑，超过窗口标为中断。停机前最后续一次租约，重启后的停机时长从这一刻量起。
   */
  async close(): Promise<void> {
    if (this.#closing) return;
    this.#closing = true;
    if (this.#leaseTimer) clearInterval(this.#leaseTimer);
    this.#touchLeases();
    for (const stop of this.#stops) await stop().catch(() => {});
    await this.#harness.close(context);
    await Promise.allSettled([...this.#drives]);
  }

  /** 续租约：告诉「下次启动」这些运行一直活到现在 */
  #touchLeases(): void {
    if (this.#byRun.size === 0) return;
    try {
      this.#o.store.touchLease([...this.#byRun.keys()], this.#now() + (this.#o.leaseTtlMs ?? DEFAULT_LEASE_TTL_MS));
    } catch {
      // 租约只是恢复时的参考；写不进去不能让运行失败
    }
  }

  // ---- 事件 ----

  #emit(run: { id: string; userId: string; sessionId: string; canvasId: string }, type: string, payload: unknown): void {
    const resuming = this.#byRun.get(run.id);
    if (resuming?.recovering && type !== "run.status") {
      // 自动续跑接上了：第一件真正的事发生之前，先告诉前端不再是「恢复中」
      resuming.recovering = false;
      this.#emit(run, "run.status", statusEvent("running"));
    }
    const rec = this.#o.store.appendEvent(run.sessionId, run.id, type, payload);
    this.#o.onEvent?.({ id: rec.id, sessionId: run.sessionId, runId: run.id, userId: run.userId, canvasId: run.canvasId, seq: rec.seq, type, payload, createdAt: rec.createdAt });
  }

  /** 只推送不落库的临时事件 */
  #push(run: { id: string; userId: string; sessionId: string; canvasId: string }, type: string, payload: unknown): void {
    this.#o.onEvent?.({ id: "0", sessionId: run.sessionId, runId: run.id, userId: run.userId, canvasId: run.canvasId, seq: 0, type, payload, createdAt: this.#now() });
  }

  #approvalView(a: Approval): ApprovalView {
    const c = this.#o.codec;
    return { id: c.encode(BigInt(a.id)), run_id: c.encode(BigInt(a.runId)), kind: a.kind, status: a.status, payload: a.payload, quote_credits: a.quoteCredits, decision: a.decision, expires_at: new Date(a.expiresAt).toISOString() };
  }

  // ---- 发起运行 ----

  async startRun(req: StartRunRequest): Promise<Run> {
    if (this.#closing) throw Err.unavailable();
    const store = this.#o.store;
    const sess = this.#session(req.userId, req.sessionId);
    const message = req.message.trim();
    if (!message) throw Err.invalidParams("消息不能为空");
    if (!req.modelKey) throw Err.modelNA();
    const mode = req.mode || sess.mode || "all";
    const maxSteps = req.maxSteps ?? this.#o.defaultMaxSteps ?? DEFAULT_MAX_STEPS;

    // 同一画布同时只能有一个活跃运行，由数据库的部分唯一索引保证
    let run: Run;
    try {
      run = store.createRun({ sessionId: sess.id, canvasId: sess.canvasId, userId: req.userId, status: "queued", mode, budgetCredits: req.budgetCredits ?? 0, maxSteps });
    } catch (e) {
      if (e instanceof DuplicateError) throw Err.runActive();
      throw e;
    }
    const selection = req.selection ?? [];
    store.updateSession(req.userId, sess.id, { mode, modelKey: req.modelKey, ...(sess.title === DEFAULT_TITLE && { title: cleanTitle(message) }) });
    const ev = { id: run.id, userId: req.userId, sessionId: sess.id, canvasId: sess.canvasId };
    this.#emit(ev, "message.user", { text: message, mode, selection });
    this.#emit(ev, "run.status", statusEvent("queued"));

    await this.#launch(run, this.#session(req.userId, sess.id), {
      selection, prepare: { message, max_steps: maxSteps }, recordHarness: true,
      kickoff: (a, setup) => a.conv.submit({ type: "input", content: setup.first_message ?? message }, context),
    });
    return { ...run, status: "queued" };
  }

  #session(userId: string, sessionId: string): Session {
    try {
      return this.#o.store.getSession(userId, sessionId);
    } catch (e) {
      if (e instanceof NotFoundError) throw Err.sessionMissing();
      throw e;
    }
  }

  #failQueued(run: Run, sess: Session, cause: unknown): void {
    const msg = truncate(String((cause as Error)?.message ?? cause), MAX_ERROR_CHARS);
    try {
      this.#o.store.updateRunIf(run.id, [...ACTIVE_STATUSES], { status: "failed", errorCode: "60005", errorMessage: Err.unavailable().message, endedAt: this.#now() });
      this.#emit({ id: run.id, userId: run.userId, sessionId: sess.id, canvasId: sess.canvasId }, "run.status", statusEvent("failed", { error: msg }));
    } catch {
      // 已经不在活跃状态：不动
    }
  }

  /** 让这个运行有一个可用的 Pi 会话：创建或取出，配置模型、系统提示词和这一轮可用的工具 */
  async #attach(sess: Session, run: Run, rc: RunContext, setup: RunSetup): Promise<Active> {
    const limits = compactionLimits(setup.model.context_window);
    this.#compaction.set(`${PROVIDER_ID}/${rc.model_key}`, { reserveTokens: limits.reserve, keepRecentTokens: limits.keepRecent });
    this.#models.ensure({ key: rc.model_key!, name: setup.model.name, context_window: setup.model.context_window, max_tokens: setup.model.max_tokens, vision: setup.model.vision });
    this.#ensureTools(setup.tools);
    const enabled = [...setup.tools.map((t) => t.name), ...(setup.tools.some((t) => t.scriptable) && !setup.disabled_tools?.includes("codemode") ? ["codemode"] : [])]
      .map((n) => this.#toolRegs.get(n)).filter((t): t is ToolRegistration => !!t);
    const agent = { model: { provider: PROVIDER_ID, modelId: rc.model_key! }, instructions: setup.system_prompt, tools: enabled };
    let conv: Conversation;
    if (sess.conversationId) {
      conv = (await this.#harness.conversation(Number(sess.conversationId) as unknown as ConversationId /* Pi 的会话 id 是带品牌的数字，库里存的是它的十进制串 */, context))!;
      await conv.configure(agent, context);
    } else {
      conv = await this.#harness.createConversation({ ownership: { kind: "ownerless" }, agent }, context);
      this.#o.store.bindConversation(sess.id, String(conv.id));
    }
    const convId = String(conv.id);
    await this.#watch(conv, convId);
    this.#contexts.set(convId, rc);
    const a: Active = { run, convId, rc, conv, paused: false, finishing: false, steps: run.steps, maxSteps: run.maxSteps, callIds: [], summaryCallIds: [], compactionDisabled: false, recovering: false, startedBefore: new Set(), lock: Promise.resolve() };
    this.#active.set(convId, a);
    this.#answered.set(convId, 0);
    this.#byRun.set(run.id, a);
    this.#touchLeases();
    return a;
  }

  /** Go 下发的工具声明变成 Pi 的工具。声明有变化才重新安装 */
  #ensureTools(decls: ToolDecl[]): void {
    for (const d of decls) this.#decls.set(d.name, d);
    const key = JSON.stringify([...this.#decls.values()]);
    if (key === this.#toolsKey) return;
    this.#toolsKey = key;
    const ext = createToolsExtension({ platform: this.#o.platform, decls: [...this.#decls.values()], host: this.host });
    for (const t of ext.tools ?? []) this.#toolRegs.set(t.name, t);
    this.#registry.install(ext);
  }

  /**
   * 为一个运行启动一段（开始、审批后续跑、「继续」共用）：运行准备 → 配置 Pi → 标 running（从 queued 来的才有这一步）→ 后台驱动。
   * 启动失败说明 Agent 暂时不可用（模型下线、平台连不上）：运行标为失败，画布立刻空出来，对用户说 60005。
   */
  async #launch(run: Run, sess: Session, o: {
    selection: string[];
    prepare: { message?: string; max_steps?: number };
    /** 第一段才记版本；之后沿用，运行中途发版也不会让同一次运行有两个版本 */
    recordHarness?: boolean;
    kickoff: (a: Active, setup: RunSetup) => Promise<Submission>;
  }): Promise<void> {
    if (this.#closing) throw Err.unavailable();
    const store = this.#o.store;
    const rc = this.#rc(run, sess, o.selection);
    let a: Active;
    let kickoff: () => Promise<Submission | undefined>;
    try {
      const setup = await this.#o.platform.prepare(rc, o.prepare);
      if (o.recordHarness) store.updateRunIf(run.id, ["queued"], { promptVersion: setup.harness.prompt_version, toolsHash: setup.harness.tools_hash, buildSha: setup.harness.build_sha, harnessKey: setup.harness.key });
      a = await this.#attach(sess, run, rc, setup);
      kickoff = () => o.kickoff(a, setup);
    } catch (e) {
      this.#failQueued(run, sess, e);
      throw Err.unavailable();
    }
    if (run.status === "queued") {
      store.updateRunIf(run.id, ["queued"], { status: "running" });
      this.#emit(this.#ev(a), "run.status", statusEvent("running"));
    }
    a.drive = this.#drive(a, kickoff);
    this.#drives.add(a.drive);
    void a.drive.finally(() => this.#drives.delete(a.drive!));
  }

  /** 运行上下文：身份、模式、模型、选中的节点、本轮预算和已批准的生成占用的积分 */
  #rc(run: Run, sess: Session, selection: string[]): RunContext {
    const extras = this.#extras(run.id);
    return {
      run_id: run.id, user_id: run.userId, session_id: sess.id, canvas_id: sess.canvasId, mode: run.mode, model_key: sess.modelKey, selection,
      ...(run.budgetCredits ? { budget_credits: run.budgetCredits } : {}), ...(extras ? { spent_extra: extras } : {}),
    };
  }

  /** kickoff 返回 undefined 表示接手一个已经在 Pi 里的任务（启动恢复），没有新提交可等 */
  async #drive(a: Active, kickoff: () => Promise<Submission | undefined>): Promise<void> {
    try {
      const sub = await kickoff();
      await this.#untilQuiet(a);
      const rec = sub ? await sub.wait(context) : undefined;
      await this.#finalize(a, rec?.status === "unanswered" ? `运行中断：${rec.reason}` : undefined);
    } catch (e) {
      if (this.#closing) return; // 正常停机：运行留在原状态，下次启动按窗口判断
      await this.#finalize(a, String((e as Error)?.message ?? e));
    } finally {
      if (!this.#closing) await this.#release(a);
    }
  }

  /** 等 Pi 彻底空闲：空闲后队列里还有没处理的输入（插话落在收尾瞬间）就继续等；确认没有了才收尾，之后到的插话被拒绝 */
  async #untilQuiet(a: Active): Promise<void> {
    for (;;) {
      await a.conv.waitForIdle(context);
      const quiet = await this.#locked(a, async () => {
        const busy = (await this.#harness.inspect(context)).submissions.some((s) => s.conversationId === a.conv.id);
        if (!busy) a.finishing = true;
        return !busy;
      });
      if (quiet) return;
    }
  }

  /** 同一个运行的插话和收尾互斥 */
  #locked<T>(a: Active, fn: () => Promise<T>): Promise<T> {
    const next = a.lock.then(fn, fn);
    a.lock = next.catch(() => {});
    return next;
  }

  /** 一段运行停下之后定最终状态。运行已经被别的流程改成别的状态（等审批、步数用尽、取消）就不动 */
  async #finalize(a: Active, fallbackError?: string): Promise<void> {
    const store = this.#o.store;
    const cur = store.getRun(a.run.userId, a.run.id);
    if (cur.status !== "running") return;
    let fields: Parameters<AgentStore["updateRunIf"]>[2];
    let extra: Record<string, unknown> = { error: "" };
    if (a.modelError?.code === OVER_BUDGET_CODE) {
      fields = { status: "budget_exhausted", endedAt: this.#now() };
      extra = {};
    } else if (a.modelError || fallbackError) {
      const msg = truncate(a.modelError?.message || fallbackError || "模型调用失败", MAX_ERROR_CHARS);
      fields = { status: "failed", errorCode: "agent_error", errorMessage: msg, endedAt: this.#now() };
      extra = { error: msg };
    } else {
      fields = { status: "succeeded", endedAt: this.#now() };
    }
    try {
      const updated = store.updateRunIf(a.run.id, ["running"], fields);
      this.#emit(this.#ev(a), "run.status", statusEvent(updated.status, extra));
    } catch (e) {
      if (!(e instanceof StateConflictError)) throw e; // 刚好被取消了：保持取消
    }
  }

  /** 一段运行结束：释放内存里的状态，并告诉 Go 释放它那边这个运行的状态（联网次数、技能固定版本）；失败不要紧，Go 闲置过期会清 */
  async #release(a: Active): Promise<void> {
    this.#byRun.delete(a.run.id);
    if (this.#active.get(a.convId) === a) this.#active.delete(a.convId);
    this.#contexts.delete(a.convId);
    await this.#o.platform.endRun(a.rc).catch(() => {});
  }

  #ev(a: Active) {
    return { id: a.run.id, userId: a.run.userId, sessionId: a.run.sessionId, canvasId: a.run.canvasId };
  }

  // ---- 取消和插话 ----

  // ---- 审批的决定 ----

  /**
   * 用户对审批做了决定：批准、拒绝或回答提问。平台解析（逐项勾选、重新算价、检查预算）→ 记决定 → 运行回到 running →
   * 执行批准的内容 → 把结果交给模型让运行接着往下走。
   * 决定不合法（超预算、一项都没批准）时什么都不改，业务错误原样返回。
   */
  async decide(userId: string, approvalId: string, req: ApprovalDecision): Promise<ApprovalView> {
    const store = this.#o.store;
    let approval: Approval;
    try {
      approval = store.getApproval(userId, approvalId);
    } catch (e) {
      if (e instanceof NotFoundError) throw Err.approvalMissing();
      throw e;
    }
    const run = this.#run(userId, approval.runId);
    // 已处理（重复点击）、已过期、运行已被停止都按「审批已处理或已过期」
    if (approval.status !== "pending" || this.#now() >= approval.expiresAt || !WAITING.includes(run.status)) throw Err.approvalGone();
    const sess = this.#session(userId, run.sessionId);

    let outcome: ApprovalOutcome;
    try {
      outcome = await this.#o.platform.resolve(this.#rc(run, sess, []), approval.kind as ApprovalKind, approval.payload, req);
    } catch (e) {
      throw fromPlatform(e);
    }
    const rejected = outcome.status === "rejected";

    // 审批状态迁移是 CAS：重复点击、同时在另一个标签页点，只有一次生效
    try {
      store.updateApprovalIf(approval.id, ["pending"], { status: outcome.status, decision: outcome.decision, quoteCredits: outcome.quote_credits, decidedAt: this.#now() });
    } catch (e) {
      if (e instanceof StateConflictError) throw Err.approvalGone();
      throw e;
    }
    // 运行回到 running（追加的预算一并生效）
    try {
      store.updateRunIf(run.id, WAITING, { status: "running", ...(req.add_budget && !rejected && { budgetCredits: run.budgetCredits + req.add_budget }) });
    } catch (e) {
      if (e instanceof StateConflictError) throw Err.approvalGone();
      throw e;
    }
    const ev = { id: run.id, userId, sessionId: run.sessionId, canvasId: run.canvasId };
    // 只靠 run.status 事件驱动的客户端，决定之后要靠它才知道运行不再等待了
    this.#emit(ev, "run.status", statusEvent("running"));
    // 被批准的生成先记入已花积分，防止同一轮反复批准超预算
    if (outcome.quote_credits > 0 && !rejected) {
      store.addRunUsage(run.id, 0, outcome.quote_credits);
      const cur = store.getRun(userId, run.id);
      this.#emit(ev, "run.usage", { spent_credits: cur.spentCredits, context_tokens: cur.contextTokens });
    }

    // 执行批准的内容；失败不让这次请求失败（决定已生效），模型会从结果里知道
    const fresh = store.getRun(userId, run.id);
    const exec = await this.#execute(this.#rc(fresh, sess, []), approval, outcome);
    let final = store.getApproval(userId, approval.id);
    if (exec.status === "executed" || exec.status === "failed") {
      final = store.updateApprovalIf(approval.id, ["approved", "partially_approved"], { status: exec.status, result: exec.result ?? {} });
    }
    this.#emit(ev, "approval.decided", this.#approvalView(final));

    // 让运行接着往下走；起不来就标为中断，用户可以稍后点「继续」
    try {
      await this.#launch(fresh, sess, {
        selection: [], prepare: {},
        kickoff: (a) => this.#feedBack(a, approval, exec.outcome_text),
      });
    } catch {
      try {
        store.updateRunIf(run.id, ["running"], { status: "interrupted" });
        this.#emit(ev, "run.status", statusEvent("interrupted"));
      } catch {
        // 已经不在 running：别处处理过了
      }
    }
    return this.#approvalView(final);
  }

  /** 执行批准的内容。平台的执行是幂等的（键是审批 id），连不上就重试；仍不行就如实说「不知道有没有执行」 */
  async #execute(rc: RunContext, approval: Approval, outcome: ApprovalOutcome): Promise<ApprovalExecution> {
    let last: unknown;
    for (let i = 0; i < EXECUTE_ATTEMPTS; i++) {
      try {
        return await this.#o.platform.execute(rc, approval.kind as ApprovalKind, approval.id, approval.toolCallId, outcome);
      } catch (e) {
        last = e;
        if (i < EXECUTE_ATTEMPTS - 1) await sleep(EXECUTE_RETRY_MS * (i + 1));
      }
    }
    const reason = String((last as Error)?.message ?? last);
    return {
      status: "failed", result: { error: truncate(reason, MAX_ERROR_CHARS) },
      outcome_text: `用户已做出决定，但执行时出错：${truncate(reason, MAX_ERROR_CHARS)}。这次执行可能已经生效也可能没有，请先读取画布或任务确认现状，再决定要不要重做。`,
    };
  }

  /** 把审批的结果交给模型：用真实结果替换那次工具调用的占位结果，再提交一条输入让模型接着往下走 */
  async #feedBack(a: Active, approval: Approval, text: string): Promise<Submission> {
    const entries = (await a.conv.entries({}, 50, undefined, context)).items;
    const placeholder = entries.find((e) => e.kind === "pi.tool-result" && (e.model?.[0] as { toolCallId?: string } | undefined)?.toolCallId === approval.toolCallId);
    if (!placeholder?.model) return a.conv.submit({ type: "input", content: `${APPROVAL_NUDGE}\n${text}` }, context);
    const real = { ...placeholder.model[0], isError: false, content: [{ type: "text", text }] };
    const write = await a.conv.submit({ type: "write", entry: { kind: "app.approval", data: { approvalId: approval.id }, edits: [{ target: placeholder.id, action: "replace", messages: [real] }] } } as never, context);
    await write.wait(context);
    return a.conv.submit({ type: "input", content: APPROVAL_NUDGE }, context);
  }

  // ---- 继续 ----

  /** 让被中断、额度用完或步数用尽的运行继续，可以追加预算 */
  async resume(userId: string, runId: string, addBudget = 0): Promise<Run> {
    const store = this.#o.store;
    const run = this.#run(userId, runId);
    if (!RESUMABLE.includes(run.status)) throw Err.state();
    // 额度用完时必须追加，否则一继续又会立刻用尽
    if (run.status === "budget_exhausted" && addBudget <= 0) throw Err.invalidParams("预算已用尽，请追加预算");
    const sess = this.#session(userId, run.sessionId);
    if (!sess.conversationId) throw Err.state();
    // 回到 queued 重新占用画布；步数用尽时上限也放宽一轮，否则继续后马上又到上限
    let queued: Run;
    try {
      queued = store.updateRunIf(runId, RESUMABLE, {
        status: "queued", errorCode: "", errorMessage: "", endedAt: null,
        ...(addBudget > 0 && { budgetCredits: run.budgetCredits + addBudget }),
        ...(run.status === "step_limit" && { maxSteps: run.maxSteps + (this.#o.defaultMaxSteps ?? DEFAULT_MAX_STEPS) }),
      });
    } catch (e) {
      if (e instanceof DuplicateError) throw Err.runActive();
      if (e instanceof StateConflictError) throw Err.state();
      throw e;
    }
    this.#emit({ id: run.id, userId, sessionId: run.sessionId, canvasId: run.canvasId }, "run.status", statusEvent("queued", { resumed: true }));
    const interrupted = run.status === "interrupted";
    await this.#launch(queued, sess, {
      selection: [], prepare: {},
      kickoff: async (a) => {
        await a.conv.abort(context); // 被中断时 Pi 里可能还留着没跑完的任务：先收掉，从干净的状态接着走
        return a.conv.submit({ type: "input", content: interrupted ? INTERRUPTED_TEXT : CONTINUE_TEXT }, context);
      },
    });
    return queued;
  }

  async cancel(userId: string, runId: string): Promise<Run> {
    const store = this.#o.store;
    const run = this.#run(userId, runId);
    try {
      store.updateRunIf(runId, [...ACTIVE_STATUSES], { status: "canceled", endedAt: this.#now() });
    } catch (e) {
      if (e instanceof StateConflictError) throw Err.state(); // 刚好自己结束了：不覆盖真实结果
      throw e;
    }
    store.expirePendingApprovals(runId);
    const a = this.#byRun.get(runId);
    if (a) {
      a.paused = true;
      // 中止进行中的模型调用和工具；被中止的那次调用结算出来的用量、半截回答都在「已停止」之前落库
      await a.conv.abort(context).catch(() => {});
    }
    this.#emit({ id: run.id, userId: run.userId, sessionId: run.sessionId, canvasId: run.canvasId }, "run.status", statusEvent("canceled"));
    return store.getRun(userId, runId);
  }

  async interject(userId: string, runId: string, text: string): Promise<void> {
    const run = this.#run(userId, runId);
    const msg = text.trim();
    if (!msg) throw Err.invalidParams("消息不能为空");
    if (run.status !== "running") throw Err.state();
    const a = this.#byRun.get(runId);
    if (!a) throw Err.unavailable();
    await this.#locked(a, async () => {
      if (a.finishing) throw Err.state(); // 刚好在收尾
      await a.conv.submit({ type: "input", content: msg, whenBusy: "steer" }, context); // 官方效果：工具跑完后、下一次请求前一起带上；不打断正在进行的步骤
      this.#emit(this.#ev(a), "message.user", { text: msg, steer: true });
    });
  }

  #run(userId: string, runId: string): Run {
    try {
      return this.#o.store.getRun(userId, runId);
    } catch (e) {
      if (e instanceof NotFoundError) throw Err.runMissing();
      throw e;
    }
  }

  // ---- 模型回答结束 ----

  /** Pi 的 afterResponse 钩子：一次模型调用结束（含出错、被中止）。顺序和 Go 一致：先 run.usage，再 message.done */
  #hooksExtension(): Extension {
    return defineExtension({
      name: "engine-events",
      hooks: [hook(GenerationTask, { afterResponse: async (message, api) => this.#onAssistant(String(api.conversationId), message) })],
    });
  }

  /**
   * 压缩：历史超过触发线时 Pi 调这个钩子，我们用画布专用的提示词让模型写摘要（节点、任务、审批、模型 key 不能丢）。
   * 摘要请求走平台的模型代理（带运行上下文和摘要用途，Go 计费、检查额度）。出错就不压缩，运行照常继续。
   */
  #compactionExtension(): Extension {
    return defineExtension({
      name: "engine-compaction",
      hooks: [hook(CompactionTask, {
        beforeCompact: async (c, api, ctx) => {
          const a = this.#active.get(String(api.conversationId));
          if (!a || a.compactionDisabled) return { decline: true };
          const model = api.models.getModel(PROVIDER_ID, a.rc.model_key!);
          if (!model) return { decline: true };
          const window = model.contextWindow;
          try {
            const sessionId = (await api.snapshot(ProviderDoc, api.conversationId, ctx))?.sessionId;
            if (sessionId) this.#contexts.bindSession(sessionId, a.convId);
            const [first, ...rest] = c.messages;
            const previous = previousSummary(first);
            let conversation = serializeMessages((previous === undefined ? c.messages : rest) as never);
            const maxChars = Math.floor(window * 0.7); // 摘要请求本身也要装得进窗口：留出提示词和输出的位置
            if (conversation.length > maxChars) conversation = `…（更早的内容已省略）\n${conversation.slice(-maxChars)}`;
            const msg = await api.models.completeSimple(
              model,
              { systemPrompt: SUMMARY_SYSTEM_PROMPT, messages: [{ role: "user", content: [{ type: "text", text: buildPrompt(previous ?? "", conversation) }], timestamp: Date.now() }] },
              { signal: ctx.abortSignal, maxTokens: summaryMaxTokens(window), sessionId, headers: { "X-Agent-Purpose": "summary" } },
            );
            await this.#settleSummaries(a);
            if (msg.stopReason === "error" || msg.stopReason === "aborted") throw new Error(msg.errorMessage || "摘要请求失败");
            const summary = msg.content.map((p) => (p.type === "text" ? p.text : "")).join("").trim();
            if (!summary) throw new Error("摘要为空");
            return { summary };
          } catch {
            a.compactionDisabled = true;
            return { decline: true };
          }
        },
      })],
    });
  }

  /** 摘要调用也要计费：结算出来有扣费就推一条 run.usage（上下文大小只看对话调用，摘要不改它） */
  async #settleSummaries(a: Active): Promise<void> {
    for (const callId of a.summaryCallIds.splice(0)) {
      const u = await this.#settled(a, callId);
      if (!u?.call || u.call.charged <= 0) continue;
      const spent = u.spent_credits + this.#extras(a.run.id);
      this.#o.store.setRunUsage(a.run.id, { spentCredits: spent });
      this.#emit(this.#ev(a), "run.usage", { spent_credits: spent, context_tokens: this.#o.store.getRun(a.run.userId, a.run.id).contextTokens });
    }
  }

  async #onAssistant(convId: string, msg: AssistantMessage): Promise<void> {
    const a = this.#active.get(convId);
    if (!a) return;
    // 这条回答已经定稿：watcher 里还没处理完的它的增量作废（见 #watch），否则会排在 message.done 之后，前端残留成重复文字
    this.#answered.set(convId, (this.#answered.get(convId) ?? 0) + 1);
    this.#dropDeltas.get(convId)?.();
    const callId = a.callIds.shift();
    let credits: { charged: number; estimated: boolean } | undefined;
    if (callId) {
      const u = await this.#settled(a, callId);
      if (u?.call) {
        credits = { charged: u.call.charged, estimated: u.call.estimated };
        this.#o.store.setRunUsage(a.run.id, { spentCredits: u.spent_credits + this.#extras(a.run.id), contextTokens: u.context_tokens });
        this.#emit(this.#ev(a), "run.usage", { spent_credits: u.spent_credits + this.#extras(a.run.id), context_tokens: u.context_tokens });
      }
    }
    if (msg.stopReason === "error") a.modelError = { ...a.modelError, message: msg.errorMessage ?? "模型调用失败" };
    // 输出达到单次上限（思考内容也占额度）：回答是空的或半截的，工具调用的参数也可能被截断。不能当成功，否则用户什么提示都没有
    if (msg.stopReason === "length") a.modelError = { ...a.modelError, message: OUTPUT_TRUNCATED };
    const text = msg.content.filter((p) => p.type === "text").map((p) => (p as { text: string }).text).join("");
    const thinking = msg.content.filter((p) => p.type === "thinking").map((p) => (p as { thinking: string }).thinking).join("");
    const calls = msg.content.filter((p) => p.type === "toolCall").map((p) => ({ id: (p as { id: string }).id, name: (p as { name: string }).name }));
    if (!text && calls.length === 0) return;
    this.#emit(this.#ev(a), "message.done", {
      text, tool_calls: calls, partial: msg.stopReason === "aborted" || msg.stopReason === "error" || msg.stopReason === "length",
      ...(thinking && { thinking }), ...(credits && { credits: credits.charged, estimated: credits.estimated }),
    });
  }

  /** 等这次调用结算完（Go 在流结束后才结算），最多等约 2 秒；等不到就不带积分 */
  async #settled(a: Active, callId: string): Promise<PlatformUsage | undefined> {
    for (let i = 0; i < SETTLE_POLL_MAX; i++) {
      let u: PlatformUsage;
      try {
        u = await this.#o.platform.usage(a.rc, callId);
      } catch {
        return undefined;
      }
      if (u.call?.status === "settled") return u;
      await sleep(SETTLE_POLL_MS);
    }
    return undefined;
  }

  /** 已批准的生成占用的积分（模型调用的部分由 Go 按调用记录累加，不在这里） */
  #extras(runId: string): number {
    return this.#o.store.listApprovals(runId).filter((x) => SPENDING_APPROVAL.includes(x.status)).reduce((n, x) => n + x.quoteCredits, 0);
  }

  // ---- 文字增量 ----

  async #watch(conv: Conversation, convId: string): Promise<void> {
    if (this.#watched.has(convId)) return;
    this.#watched.add(convId);
    const stream = await watchEvents(this.#harness, conv.id, context);
    let text = "";
    let thinking = "";
    let timer: ReturnType<typeof setTimeout> | undefined;
    this.#dropDeltas.set(convId, () => {
      if (timer) clearTimeout(timer);
      timer = undefined;
      text = thinking = "";
    });
    // 已经定稿（发过 message.done）、但 watcher 还没看到 message_end 的回答数：它们的增量一律丢弃
    const stale = () => (this.#answered.get(convId) ?? 0) > 0;
    const flush = () => {
      timer = undefined;
      const a = this.#active.get(convId);
      if (a && !a.paused) {
        if (text) this.#push(this.#ev(a), "message.delta", { text });
        if (thinking) this.#push(this.#ev(a), "thinking.delta", { text: thinking });
      }
      text = thinking = "";
    };
    stream.start(async (events) => {
      for (const e of events) {
        if (stale() && (e.type === "message_start" || e.type === "message_update")) continue;
        if (e.type === "message_end" && e.entry.model?.some((m) => m.role === "assistant")) {
          const n = this.#answered.get(convId) ?? 0;
          if (n > 0) this.#answered.set(convId, n - 1); // 这条回答的收尾，它的增量上面已经丢掉了
          continue;
        }
        if (e.type === "message_start" && e.message.role === "assistant") {
          // 开始时已经有的文字（第一次中间提交之前流出的部分）也是增量
          for (const p of e.message.content) {
            if (p.type === "text") text += p.text;
            else if (p.type === "thinking") thinking += p.thinking;
          }
          if (text || thinking) timer ??= setTimeout(flush, DELTA_FLUSH_MS);
        } else if (e.type === "message_update") {
          for (const c of e.changes) {
            if (c.type === "text_delta") text += (c as unknown as { delta: string }).delta;
            else if (c.type === "thinking_delta") thinking += (c as unknown as { delta: string }).delta;
          }
          timer ??= setTimeout(flush, DELTA_FLUSH_MS);
        }
      }
    });
    this.#stops.push(async () => { if (timer) clearTimeout(timer); await stream.stop(); });
  }

  // ---- 工具层的宿主 ----

  readonly host: ToolHost = {
    runOf: (convId) => {
      const a = this.#active.get(convId);
      return a && ({ id: a.run.id, rc: a.rc } satisfies ToolRun);
    },
    isPaused: (run) => this.#byRun.get(run.id)?.paused ?? true,
    begin: async (run, call) => {
      const a = this.#byRun.get(run.id)!;
      // 步数上限：用到头就暂停运行并让循环停下，用户可以追加后继续。嵌套调用不受它管：脚本整体在开始时已经算过步数
      if (!call.parentId && a.steps >= a.maxSteps) {
        this.#pause(a, "step_limit");
        return { content: "已达到本轮的步数上限，运行已暂停，等待用户继续。", terminate: true };
      }
      if (a.startedBefore.delete(call.id)) return undefined; // 续跑时被重放的工具：tool.start 重启前已经发过
      this.#emit(this.#ev(a), "tool.start", { id: call.id, name: call.name, label: call.label, verb: call.verb, ...(call.parentId && { parent_id: call.parentId }) });
      return undefined;
    },
    end: async (run, call, event) => {
      const a = this.#byRun.get(run.id)!;
      this.#emit(this.#ev(a), "tool.end", event);
      if (!call.parentId) {
        a.steps++;
        this.#o.store.addRunUsage(run.id, 1, 0);
      }
    },
    plan: async (run, steps) => this.#emit(this.#ev(this.#byRun.get(run.id)!), "plan.updated", { steps }),
    approval: async (run, x) => this.#createApproval(this.#byRun.get(run.id)!, x),
  };

  /** 运行暂停（步数用尽等）：状态迁移成功才发事件，并让本轮后面的调用不再执行 */
  #pause(a: Active, status: string): void {
    a.paused = true;
    try {
      this.#o.store.updateRunIf(a.run.id, ["running"], { status });
      this.#emit(this.#ev(a), "run.status", statusEvent(status));
    } catch (e) {
      if (!(e instanceof StateConflictError)) throw e;
    }
  }

  /** 创建审批并让运行进入等待状态。按工具调用 id 幂等：崩溃后重放同一个调用不会出现两个审批 */
  #createApproval(a: Active, x: { kind: ApprovalKind; toolCallId: string; payload: unknown; quoteCredits: number; summary: string }): void {
    const store = this.#o.store;
    a.paused = true;
    if (store.listApprovals(a.run.id).some((p) => p.toolCallId === x.toolCallId)) return;
    const next = x.kind === "ask" ? "waiting_input" : "waiting_approval";
    // 先把运行置为等待状态（CAS）：运行已经被取消或结束时，不再产生审批。崩溃后重放时已经在等待状态，也算成功
    try {
      store.updateRunIf(a.run.id, ["running", "waiting_approval", "waiting_input"], { status: next });
    } catch (e) {
      if (e instanceof StateConflictError) throw Err.state();
      throw e;
    }
    const approval = store.createApproval({
      runId: a.run.id, sessionId: a.run.sessionId, canvasId: a.run.canvasId, userId: a.run.userId, kind: x.kind, toolCallId: x.toolCallId,
      payload: x.payload, quoteCredits: x.quoteCredits, status: "pending", expiresAt: this.#now() + (this.#o.approvalTtlMs ?? APPROVAL_TTL_MS),
    });
    this.#emit(this.#ev(a), "approval.created", this.#approvalView(approval));
    this.#emit(this.#ev(a), "run.status", statusEvent(next));
  }
}

/** Pi 把摘要放回上下文时套的说明：取出里面的摘要正文（下一次压缩时当作「之前的摘要」增量更新） */
function previousSummary(first: Message | undefined): string | undefined {
  if (first?.role !== "user") return undefined;
  const text = typeof first.content === "string" ? first.content : first.content.map((p) => (p.type === "text" ? p.text : "")).join("");
  return /^The conversation history before this point was compacted into the following summary:\n\n<summary>\n([\s\S]*)\n<\/summary>/.exec(text)?.[1];
}

/** 首条消息当会话标题：空白压成一个空格，最多 40 字 */
export function cleanTitle(raw: string, def = DEFAULT_TITLE): string {
  const t = truncate(raw.split(/\s+/).filter(Boolean).join(" "), 40);
  return t || def;
}

export { isActive };
