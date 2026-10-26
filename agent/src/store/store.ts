import { existsSync } from "node:fs";
import { DatabaseSync, type StatementSync } from "node:sqlite";
import { SNOWFLAKE_EPOCH_MS, createSnowflake, parseDecimal, type Snowflake } from "../ids.ts";
import { DuplicateError, NotFoundError, StateConflictError } from "./errors.ts";
import { MIGRATIONS, migrate } from "./migrations.ts";
import type {
  AgentEventRecord, Approval, ApprovalPatch, Feedback, NewApproval, NewFeedback, NewRun, NewSession, Run, RunPatch, Session, SessionPatch,
} from "./types.ts";

export interface StoreOptions {
  /** 雪花 ID 的工作节点号，默认 1（AGENT_WORKER_ID） */
  workerId?: number;
  /** 时钟（毫秒），默认 Date.now，测试注入 */
  now?: () => number;
  /** 注入 ID 分配器（测试用）；不给就用 workerId 创建，并保证不早于库里已有的最大 ID */
  ids?: Pick<Snowflake, "next">;
}

/** 列表接口不传 limit 时的默认条数，和 Go 的 defaultListLimit 一致 */
const DEFAULT_LIST_LIMIT = 50;

type Row = Record<string, bigint | number | string | null>;
type SqlValue = bigint | number | string | null;

const num = (v: unknown): number => Number(v);
const id = (v: unknown): string => String(v);
const dec = (v: unknown): bigint => parseDecimal(String(v));
const json = (v: unknown): string => JSON.stringify(v === undefined ? {} : v);

function sessionOf(r: Row): Session {
  return {
    id: id(r.id), userId: id(r.user_id), canvasId: id(r.canvas_id), title: String(r.title), titleSource: String(r.title_source) as Session["titleSource"], mode: String(r.mode),
    modelKey: String(r.model_key), lastSeq: num(r.last_seq), conversationId: String(r.conversation_id), createdAt: num(r.created_at), updatedAt: num(r.updated_at),
  };
}

function runOf(r: Row): Run {
  return {
    id: id(r.id), sessionId: id(r.session_id), canvasId: id(r.canvas_id), userId: id(r.user_id), status: String(r.status),
    mode: String(r.mode), budgetCredits: num(r.budget_credits), spentCredits: num(r.spent_credits),
    contextTokens: num(r.context_tokens), steps: num(r.steps), maxSteps: num(r.max_steps), errorCode: String(r.error_code),
    errorMessage: String(r.error_message), promptVersion: num(r.prompt_version), toolsHash: String(r.tools_hash),
    buildSha: String(r.build_sha), harnessKey: String(r.harness_key), leaseUntil: num(r.lease_until),
    createdAt: num(r.created_at), updatedAt: num(r.updated_at), endedAt: r.ended_at === null ? null : num(r.ended_at),
  };
}

function eventOf(r: Row): AgentEventRecord {
  return {
    id: id(r.id), sessionId: id(r.session_id), runId: id(r.run_id), seq: num(r.seq), type: String(r.type),
    payload: JSON.parse(String(r.payload)), createdAt: num(r.created_at),
  };
}

function approvalOf(r: Row): Approval {
  return {
    id: id(r.id), runId: id(r.run_id), sessionId: id(r.session_id), canvasId: id(r.canvas_id), userId: id(r.user_id),
    kind: String(r.kind), toolCallId: String(r.tool_call_id), payload: JSON.parse(String(r.payload)),
    quoteCredits: num(r.quote_credits), status: String(r.status), decision: JSON.parse(String(r.decision)),
    result: JSON.parse(String(r.result)), decidedAt: r.decided_at === null ? null : num(r.decided_at),
    expiresAt: num(r.expires_at), createdAt: num(r.created_at), updatedAt: num(r.updated_at),
  };
}

function feedbackOf(r: Row): Feedback {
  return {
    runId: id(r.run_id), userId: id(r.user_id), rating: num(r.rating), reasons: JSON.parse(String(r.reasons)) as string[],
    comment: String(r.comment), createdAt: num(r.created_at), updatedAt: num(r.updated_at),
  };
}

/** 约束违反翻译成业务错误；别的错误原样抛出 */
function translate(e: unknown, missing: string): never {
  const err = e as { errcode?: number; message?: string };
  if (err && typeof err.errcode === "number" && (err.errcode & 0xff) === 19) {
    const msg = err.message ?? "";
    if (/FOREIGN KEY/i.test(msg)) throw new NotFoundError(missing);
    if (/UNIQUE|PRIMARY KEY/i.test(msg)) throw new DuplicateError();
  }
  throw e;
}

/** 按白名单把补丁变成 SET 子句；字段名只能来自 columns，不会把调用方给的字符串拼进 SQL */
function setClause(patch: object, columns: Record<string, string>, encode: (key: string, v: unknown) => SqlValue): { sql: string; values: SqlValue[] } {
  const sets: string[] = [];
  const values: SqlValue[] = [];
  for (const [key, v] of Object.entries(patch)) {
    if (v === undefined) continue;
    const col = Object.hasOwn(columns, key) ? columns[key] : undefined;
    if (!col) throw new Error(`不能更新字段 ${JSON.stringify(key)}`);
    sets.push(`${col} = ?`);
    values.push(encode(key, v));
  }
  if (sets.length === 0) throw new Error("没有要更新的字段");
  return { sql: sets.join(", "), values };
}

const SESSION_COLUMNS = { title: "title", titleSource: "title_source", mode: "mode", modelKey: "model_key" };
const RUN_COLUMNS = {
  status: "status", errorCode: "error_code", errorMessage: "error_message", endedAt: "ended_at", leaseUntil: "lease_until",
  mode: "mode", budgetCredits: "budget_credits", spentCredits: "spent_credits", steps: "steps", contextTokens: "context_tokens",
  maxSteps: "max_steps", promptVersion: "prompt_version", toolsHash: "tools_hash", buildSha: "build_sha", harnessKey: "harness_key",
};
const APPROVAL_COLUMNS = { status: "status", decision: "decision", result: "result", decidedAt: "decided_at", quoteCredits: "quote_credits" };

const plain = (_key: string, v: unknown): SqlValue => v as SqlValue;

/**
 * Agent 服务的应用数据：会话、运行、事件、审批（SQLite，和 Pi Durable 共用一个文件）。
 *
 * 全部同步（node:sqlite 是同步的）：每个操作要么整体成功要么没发生，没有「await 之间状态被别人改了」的问题。
 * 语义对应 Go 的 AgentRepository：按用户隔离、软删除会话、状态迁移是 CAS（UPDATE … WHERE status IN …）、
 * 同一画布同时只有一个活跃运行（部分唯一索引）、事件序号在同一事务里原子分配。
 *
 * 持久性：WAL + synchronous=NORMAL。进程被杀（容器重启、OOM、崩溃）不丢已提交的事务；
 * 整机断电可能丢掉最后几个事务，但不会损坏文件。
 */
export class AgentStore {
  readonly #db: DatabaseSync;
  readonly #now: () => number;
  readonly #ids: Pick<Snowflake, "next">;
  readonly #stmts = new Map<string, StatementSync>();
  #inTx = false;

  constructor(db: DatabaseSync, now: () => number, ids: Pick<Snowflake, "next">) {
    this.#db = db;
    this.#now = now;
    this.#ids = ids;
  }

  /** 预编译并缓存语句；ID 列读成 bigint，不丢精度 */
  #q(sql: string): StatementSync {
    let st = this.#stmts.get(sql);
    if (!st) {
      st = this.#db.prepare(sql);
      st.setReadBigInts(true);
      this.#stmts.set(sql, st);
    }
    return st;
  }

  #get(sql: string, ...params: SqlValue[]): Row | undefined {
    return this.#q(sql).get(...params) as Row | undefined;
  }

  #all(sql: string, ...params: SqlValue[]): Row[] {
    return this.#q(sql).all(...params) as Row[];
  }

  #run(sql: string, ...params: SqlValue[]): number {
    return Number(this.#q(sql).run(...params).changes);
  }

  /** 在一个事务里执行；已经在事务里就直接执行。BEGIN IMMEDIATE 一开始就拿写锁，避免读后升级写时的死锁 */
  #tx<T>(fn: () => T): T {
    if (this.#inTx) return fn();
    this.#db.exec("BEGIN IMMEDIATE");
    this.#inTx = true;
    try {
      const r = fn();
      this.#db.exec("COMMIT");
      return r;
    } catch (e) {
      try {
        this.#db.exec("ROLLBACK");
      } catch {
        // 事务可能已经被 SQLite 自己回滚了
      }
      throw e;
    } finally {
      this.#inTx = false;
    }
  }

  #newId(): bigint {
    return this.#ids.next();
  }

  // ---- 会话 ----

  createSession(in_: NewSession): Session {
    const t = this.#now();
    const row = this.#get(
      `INSERT INTO agent_sessions (id, user_id, canvas_id, title, title_source, mode, model_key, last_seq, created_at, updated_at)
       VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?) RETURNING *`,
      this.#newId(), dec(in_.userId), dec(in_.canvasId), in_.title, in_.titleSource ?? "draft", in_.mode, in_.modelKey, t, t,
    );
    return sessionOf(row!);
  }

  /** 按 id + userId 读会话（不含已删除的）；别人的和不存在的一样抛 NotFoundError */
  getSession(userId: string, sessionId: string): Session {
    const row = this.#get("SELECT * FROM agent_sessions WHERE id = ? AND user_id = ? AND deleted_at IS NULL", dec(sessionId), dec(userId));
    if (!row) throw new NotFoundError("会话");
    return sessionOf(row);
  }

  /** 列出用户在某画布上的会话，最近更新的在前（同一时刻按 ID 倒序） */
  listSessions(userId: string, canvasId: string): Session[] {
    return this.#all(
      "SELECT * FROM agent_sessions WHERE user_id = ? AND canvas_id = ? AND deleted_at IS NULL ORDER BY updated_at DESC, id DESC",
      dec(userId), dec(canvasId),
    ).map(sessionOf);
  }

  countSessions(userId: string, canvasId: string): number {
    const row = this.#get("SELECT COUNT(*) AS n FROM agent_sessions WHERE user_id = ? AND canvas_id = ? AND deleted_at IS NULL", dec(userId), dec(canvasId));
    return num(row!.n);
  }

  updateSession(userId: string, sessionId: string, patch: SessionPatch): void {
    const { sql, values } = setClause(patch, SESSION_COLUMNS, plain);
    const changed = this.#run(
      `UPDATE agent_sessions SET ${sql}, updated_at = ? WHERE id = ? AND user_id = ? AND deleted_at IS NULL`,
      ...values, this.#now(), dec(sessionId), dec(userId),
    );
    if (changed === 0) throw new NotFoundError("会话");
  }

  /**
   * 模型给会话起的标题：只有标题还是草稿时才写（用户改过名、或已经命名过就不覆盖），判断和写入是一条语句，不会和手动改名竞争。
   * 不改更新时间：命名不该让会话在列表里跳到最前。
   * @returns 是否写入了
   */
  setLlmTitle(userId: string, sessionId: string, title: string): boolean {
    return this.#run(
      "UPDATE agent_sessions SET title = ?, title_source = 'llm' WHERE id = ? AND user_id = ? AND title_source = 'draft' AND deleted_at IS NULL",
      title, dec(sessionId), dec(userId),
    ) > 0;
  }

  /** 记下会话在 Pi 里的会话 id。内部动作，不改更新时间 */
  bindConversation(sessionId: string, conversationId: string): void {
    const changed = this.#run("UPDATE agent_sessions SET conversation_id = ? WHERE id = ? AND deleted_at IS NULL", conversationId, dec(sessionId));
    if (changed === 0) throw new NotFoundError("会话");
  }

  /** 软删除：行还在（运行、事件、审批仍然引用它），但读、列表、追加事件都当它不存在 */
  deleteSession(userId: string, sessionId: string): void {
    const t = this.#now();
    const changed = this.#run("UPDATE agent_sessions SET deleted_at = ?, updated_at = ? WHERE id = ? AND user_id = ? AND deleted_at IS NULL", t, t, dec(sessionId), dec(userId));
    if (changed === 0) throw new NotFoundError("会话");
  }

  // ---- 事件 ----

  /**
   * 给会话追加一条事件。序号在同一个事务里先加一再写事件：并发（和 Pi 的连接）不会拿到相同的序号，
   * 写事件失败整个事务回滚，序号不会留下空洞。会话不存在或已删除抛 NotFoundError。
   */
  appendEvent(sessionId: string, runId: string, type: string, payload: unknown): AgentEventRecord {
    const sid = dec(sessionId);
    return this.#tx(() => {
      const seqRow = this.#get("UPDATE agent_sessions SET last_seq = last_seq + 1 WHERE id = ? AND deleted_at IS NULL RETURNING last_seq", sid);
      if (!seqRow) throw new NotFoundError("会话");
      try {
        const row = this.#get(
          "INSERT INTO agent_events (id, session_id, run_id, seq, type, payload, created_at) VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING *",
          this.#newId(), sid, BigInt(runId), seqRow.last_seq as bigint, type, json(payload), this.#now(),
        );
        return eventOf(row!);
      } catch (e) {
        return translate(e, "会话");
      }
    });
  }

  /** 序号大于 afterSeq 的事件，升序，最多 limit 条（<=0 取默认值） */
  listEvents(sessionId: string, afterSeq: number, limit = 0): AgentEventRecord[] {
    return this.#all(
      "SELECT * FROM agent_events WHERE session_id = ? AND seq > ? ORDER BY seq ASC LIMIT ?",
      dec(sessionId), afterSeq, limit > 0 ? limit : DEFAULT_LIST_LIMIT,
    ).map(eventOf);
  }

  /** 一次运行的全部事件，按序号升序（观测导出用） */
  listRunEvents(runId: string): AgentEventRecord[] {
    return this.#all("SELECT * FROM agent_events WHERE run_id = ? ORDER BY seq ASC", dec(runId)).map(eventOf);
  }

  /** 同会话里比 runId 更早的 turns 轮的用户消息和最终回复，按序号升序：给读导出的人补上"这一轮是接着什么说的" */
  listContextEvents(sessionId: string, runId: string, turns: number): AgentEventRecord[] {
    return this.#all(
      `SELECT * FROM agent_events WHERE type IN ('message.user', 'message.done') AND run_id IN
         (SELECT id FROM agent_runs WHERE session_id = ? AND id < ? ORDER BY id DESC LIMIT ?)
       ORDER BY seq ASC`,
      dec(sessionId), dec(runId), turns,
    ).map(eventOf);
  }

  /** 会话是否已被删除（软删除）；不存在按已删除处理。观测里这本身是个信号 */
  isSessionDeleted(sessionId: string): boolean {
    const row = this.#get("SELECT deleted_at FROM agent_sessions WHERE id = ?", dec(sessionId));
    return !row || row.deleted_at !== null;
  }

  /** 序号小于 beforeSeq 的最新 limit 条，升序（beforeSeq<=0 不设上界，limit<=0 取默认值）；聊天记录从最新往回翻页用 */
  listEventsBefore(sessionId: string, beforeSeq: number, limit = 0): AgentEventRecord[] {
    const n = limit > 0 ? limit : DEFAULT_LIST_LIMIT;
    const rows = beforeSeq > 0
      ? this.#all("SELECT * FROM agent_events WHERE session_id = ? AND seq < ? ORDER BY seq DESC LIMIT ?", dec(sessionId), beforeSeq, n)
      : this.#all("SELECT * FROM agent_events WHERE session_id = ? ORDER BY seq DESC LIMIT ?", dec(sessionId), n);
    return rows.reverse().map(eventOf);
  }

  // ---- 运行 ----

  /** 创建运行。同一画布上已经有活跃运行抛 DuplicateError（部分唯一索引保证，并发不会漏判）；会话不存在抛 NotFoundError */
  createRun(in_: NewRun): Run {
    const t = this.#now();
    try {
      const row = this.#get(
        `INSERT INTO agent_runs (id, session_id, canvas_id, user_id, status, mode, budget_credits, max_steps, prompt_version,
           tools_hash, build_sha, harness_key, lease_until, created_at, updated_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING *`,
        this.#newId(), dec(in_.sessionId), dec(in_.canvasId), dec(in_.userId), in_.status, in_.mode, in_.budgetCredits ?? 0,
        in_.maxSteps ?? 40, in_.promptVersion ?? 0, in_.toolsHash ?? "", in_.buildSha ?? "", in_.harnessKey ?? "", in_.leaseUntil ?? 0, t, t,
      );
      return runOf(row!);
    } catch (e) {
      return translate(e, "会话");
    }
  }

  getRun(userId: string, runId: string): Run {
    const row = this.#get("SELECT * FROM agent_runs WHERE id = ? AND user_id = ?", dec(runId), dec(userId));
    if (!row) throw new NotFoundError("运行");
    return runOf(row);
  }

  /** 按 id 读运行，不看用户（Go 的后台只读用；面向用户的读取用 getRun） */
  getRunById(runId: string): Run {
    const row = this.#get("SELECT * FROM agent_runs WHERE id = ?", dec(runId));
    if (!row) throw new NotFoundError("运行");
    return runOf(row);
  }

  /** 更新时间晚于 afterMs 的运行，按更新时间升序（同一时刻按 id），最多 limit 条；Go 的观测同步用它找出变过的运行 */
  listRunsUpdatedAfter(afterMs: number, limit: number): Run[] {
    return this.#all("SELECT * FROM agent_runs WHERE updated_at > ? ORDER BY updated_at ASC, id ASC LIMIT ?", afterMs, limit).map(runOf);
  }

  /** 只刷新运行的更新时间：评价、撤销这类不改运行本身、但 Go 要重新汇总摘要的变化 */
  touchRun(runId: string): void {
    const changed = this.#run("UPDATE agent_runs SET updated_at = ? WHERE id = ?", this.#now(), dec(runId));
    if (changed === 0) throw new NotFoundError("运行");
  }

  /** 处于这些状态的所有运行（启动恢复用） */
  listRunsByStatus(statuses: readonly string[]): Run[] {
    if (statuses.length === 0) return [];
    return this.#all(`SELECT * FROM agent_runs WHERE status IN (${statuses.map(() => "?").join(",")}) ORDER BY id ASC`, ...statuses).map(runOf);
  }

  /** 续租约：服务还活着。不改更新时间，也不看状态以外的东西——只对仍在跑的运行续 */
  touchLease(runIds: readonly string[], leaseUntil: number): void {
    for (const id of runIds) this.#run("UPDATE agent_runs SET lease_until = ? WHERE id = ? AND status IN ('queued','running')", leaseUntil, dec(id));
  }

  /** 某画布上占着位置的活跃运行，没有返回 undefined */
  activeRun(canvasId: string): Run | undefined {
    const row = this.#get("SELECT * FROM agent_runs WHERE canvas_id = ? AND status IN ('queued','running','waiting_approval','waiting_input')", dec(canvasId));
    return row ? runOf(row) : undefined;
  }

  /**
   * 运行状态迁移的 CAS：UPDATE … WHERE id=? AND status IN (from) … RETURNING *。
   * 没命中（运行不存在，或状态已不在 from 里）抛 StateConflictError；迁回活跃状态时画布已被别的运行占用抛 DuplicateError。
   * 迁到终态时由调用方在 patch 里写 endedAt。
   */
  updateRunIf(runId: string, from: readonly string[], patch: RunPatch): Run {
    if (from.length === 0) throw new Error("updateRunIf 的 from 不能为空");
    const { sql, values } = setClause(patch, RUN_COLUMNS, plain);
    try {
      const row = this.#get(
        `UPDATE agent_runs SET ${sql}, updated_at = ? WHERE id = ? AND status IN (${from.map(() => "?").join(",")}) RETURNING *`,
        ...values, this.#now(), dec(runId), ...from,
      );
      if (!row) throw new StateConflictError("运行的状态已经变化");
      return runOf(row);
    } catch (e) {
      if (e instanceof StateConflictError) throw e;
      return translate(e, "运行");
    }
  }

  /** 把还停在 queued / running 的运行标为 interrupted，返回条数。服务启动时调用：等审批和等回答的状态在库里，不受影响 */
  interruptActiveRuns(): number {
    return this.#run("UPDATE agent_runs SET status = 'interrupted', updated_at = ? WHERE status IN ('queued','running')", this.#now());
  }

  /** 原子累加步数和已花积分 */
  addRunUsage(runId: string, steps: number, credits: number): void {
    const changed = this.#run("UPDATE agent_runs SET steps = steps + ?, spent_credits = spent_credits + ?, updated_at = ? WHERE id = ?", steps, credits, this.#now(), dec(runId));
    if (changed === 0) throw new NotFoundError("运行");
  }

  /** 记下运行的已花积分和上下文大小（覆盖旧值）。不看运行状态：取消后，被中止的那次调用结算出来的用量也要记上 */
  setRunUsage(runId: string, usage: { spentCredits: number; contextTokens?: number }): void {
    const changed = usage.contextTokens === undefined
      ? this.#run("UPDATE agent_runs SET spent_credits = ?, updated_at = ? WHERE id = ?", usage.spentCredits, this.#now(), dec(runId))
      : this.#run("UPDATE agent_runs SET spent_credits = ?, context_tokens = ?, updated_at = ? WHERE id = ?", usage.spentCredits, usage.contextTokens, this.#now(), dec(runId));
    if (changed === 0) throw new NotFoundError("运行");
  }

  /** 记下会话当前的上下文大小（最近一次对话调用的输入 Token 数），覆盖旧值 */
  setRunContextTokens(runId: string, tokens: number): void {
    const changed = this.#run("UPDATE agent_runs SET context_tokens = ?, updated_at = ? WHERE id = ?", tokens, this.#now(), dec(runId));
    if (changed === 0) throw new NotFoundError("运行");
  }

  // ---- 审批 ----

  createApproval(in_: NewApproval): Approval {
    const t = this.#now();
    try {
      const row = this.#get(
        `INSERT INTO agent_approvals (id, run_id, session_id, canvas_id, user_id, kind, tool_call_id, payload, quote_credits, status, expires_at, created_at, updated_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING *`,
        this.#newId(), dec(in_.runId), dec(in_.sessionId), dec(in_.canvasId), dec(in_.userId), in_.kind, in_.toolCallId,
        json(in_.payload), in_.quoteCredits, in_.status, in_.expiresAt, t, t,
      );
      return approvalOf(row!);
    } catch (e) {
      return translate(e, "运行");
    }
  }

  getApproval(userId: string, approvalId: string): Approval {
    const row = this.#get("SELECT * FROM agent_approvals WHERE id = ? AND user_id = ?", dec(approvalId), dec(userId));
    if (!row) throw new NotFoundError("审批");
    return approvalOf(row);
  }

  /** 某运行的审批，按创建先后 */
  listApprovals(runId: string): Approval[] {
    return this.#all("SELECT * FROM agent_approvals WHERE run_id = ? ORDER BY id ASC", dec(runId)).map(approvalOf);
  }

  /** 审批状态迁移的 CAS：只有当前状态在 from 里才更新，返回更新后的审批；没命中抛 StateConflictError（重复点击、已过期、本轮已停止） */
  updateApprovalIf(approvalId: string, from: readonly string[], patch: ApprovalPatch): Approval {
    if (from.length === 0) throw new Error("updateApprovalIf 的 from 不能为空");
    const { sql, values } = setClause(patch, APPROVAL_COLUMNS, (key, v) => (key === "decision" || key === "result" ? json(v) : (v as SqlValue)));
    const row = this.#get(
      `UPDATE agent_approvals SET ${sql}, updated_at = ? WHERE id = ? AND status IN (${from.map(() => "?").join(",")}) RETURNING *`,
      ...values, this.#now(), dec(approvalId), ...from,
    );
    if (!row) throw new StateConflictError("审批已经被处理");
    return approvalOf(row);
  }

  /** 把某运行所有待处理的审批置为已失效，返回条数；运行被停止或结束时调用 */
  expirePendingApprovals(runId: string): number {
    return this.#run("UPDATE agent_approvals SET status = 'expired', updated_at = ? WHERE run_id = ? AND status = 'pending'", this.#now(), dec(runId));
  }

  // ---- 评价 ----

  /** 记下评价：同一人对同一运行覆盖（保留创建时间）。运行不存在抛 NotFoundError */
  upsertFeedback(in_: NewFeedback): Feedback {
    const t = this.#now();
    try {
      const row = this.#get(
        `INSERT INTO agent_feedback (run_id, user_id, rating, reasons, comment, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)
         ON CONFLICT (run_id, user_id) DO UPDATE SET rating = excluded.rating, reasons = excluded.reasons, comment = excluded.comment, updated_at = excluded.updated_at
         RETURNING *`,
        dec(in_.runId), dec(in_.userId), in_.rating, JSON.stringify(in_.reasons), in_.comment, t, t,
      );
      return feedbackOf(row!);
    } catch (e) {
      return translate(e, "运行");
    }
  }

  getFeedback(runId: string, userId: string): Feedback | undefined {
    const row = this.#get("SELECT * FROM agent_feedback WHERE run_id = ? AND user_id = ?", dec(runId), dec(userId));
    return row ? feedbackOf(row) : undefined;
  }

  /** 取消评价，返回有没有删到 */
  deleteFeedback(runId: string, userId: string): boolean {
    return this.#run("DELETE FROM agent_feedback WHERE run_id = ? AND user_id = ?", dec(runId), dec(userId)) > 0;
  }

  // ---- 备份和关闭 ----

  /**
   * 把整个数据库（含同一个文件里 Pi Durable 的表）备份成一个一致的快照文件。用 VACUUM INTO：
   * 在线、不阻塞读写太久、结果是单个自包含的文件（不依赖 -wal）。目标文件已存在不覆盖。
   */
  backupTo(path: string): void {
    if (existsSync(path)) throw new Error(`备份文件已存在：${path}`);
    this.#db.prepare("VACUUM INTO ?").run(path);
  }

  close(): void {
    this.#stmts.clear();
    this.#db.close();
  }
}

/** 库里所有表的最大 ID 的时间戳（毫秒，Unix 时间）；库是空的返回 0 */
function newestIdTimestamp(db: DatabaseSync): number {
  const st = db.prepare(`SELECT MAX(m) AS m FROM (
    SELECT MAX(id) AS m FROM agent_sessions UNION ALL SELECT MAX(id) FROM agent_runs
    UNION ALL SELECT MAX(id) FROM agent_events UNION ALL SELECT MAX(id) FROM agent_approvals)`);
  st.setReadBigInts(true);
  const row = st.get() as { m: bigint | null };
  return row.m === null ? 0 : Number(row.m >> 22n) + SNOWFLAKE_EPOCH_MS;
}

/** 打开（不存在就创建）数据库并应用迁移。path 可以是 ":memory:"（测试用） */
export function openStore(path: string, opts: StoreOptions = {}): AgentStore {
  const now = opts.now ?? Date.now;
  const db = new DatabaseSync(path);
  try {
    // 先设 busy_timeout：Pi 的连接和这里共用一个文件，对方持有写锁时等一会儿，而不是立刻 SQLITE_BUSY
    db.exec("PRAGMA busy_timeout = 5000");
    db.exec("PRAGMA journal_mode = WAL");
    db.exec("PRAGMA synchronous = NORMAL");
    db.exec("PRAGMA foreign_keys = ON");
    migrate(db, MIGRATIONS, now);
    const ids = opts.ids ?? createSnowflake({ workerId: opts.workerId ?? 1, now, notBefore: newestIdTimestamp(db) });
    return new AgentStore(db, now, ids);
  } catch (e) {
    db.close();
    throw e;
  }
}
