import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { existsSync, mkdtempSync, rmSync } from "node:fs";
import { DatabaseSync } from "node:sqlite";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { SNOWFLAKE_EPOCH_MS, createSnowflake } from "../src/ids.ts";
import { DuplicateError, NotFoundError, StateConflictError } from "../src/store/errors.ts";
import { MIGRATIONS, migrate } from "../src/store/migrations.ts";
import { openStore, type AgentStore } from "../src/store/store.ts";

function tmpDir(): string {
  return mkdtempSync(join(tmpdir(), "agent-store-"));
}

/** 一个可控的时钟 */
function clock(start = Date.UTC(2026, 9, 25)) { // 取接近现实的时间：这时的雪花 ID 约 10^17，已经超过 2^53
  const c = { t: start, now: () => c.t, tick: (ms = 1) => (c.t += ms) };
  return c;
}

function withStore(fn: (s: AgentStore, c: ReturnType<typeof clock>, dbPath: string) => void | Promise<void>) {
  return async () => {
    const dir = tmpDir();
    const c = clock();
    const dbPath = join(dir, "agent.sqlite");
    const s = openStore(dbPath, { workerId: 1, now: c.now });
    try {
      await fn(s, c, dbPath);
    } finally {
      s.close();
      rmSync(dir, { recursive: true, force: true });
    }
  };
}

const newSession = (s: AgentStore, over: Partial<Parameters<AgentStore["createSession"]>[0]> = {}) =>
  s.createSession({ userId: "1", canvasId: "7", title: "新会话", mode: "all", modelKey: "claude", ...over });
const newRun = (s: AgentStore, sessionId: string, over: Partial<Parameters<AgentStore["createRun"]>[0]> = {}) =>
  s.createRun({ sessionId, canvasId: "7", userId: "1", status: "running", mode: "all", ...over });

// ---- 迁移 ----

test("迁移：新库按顺序全部应用，再次打开不重复应用", () => {
  const dir = tmpDir();
  try {
    const p = join(dir, "a.sqlite");
    openStore(p, { workerId: 1 }).close();
    const raw = new DatabaseSync(p);
    const rows = raw.prepare("SELECT version FROM agent_schema_migrations ORDER BY version").all() as Array<{ version: number }>;
    raw.close();
    assert.deepEqual(rows.map((r) => r.version), MIGRATIONS.map((m) => m.version));
    openStore(p, { workerId: 1 }).close(); // 再开一次不报错
    const raw2 = new DatabaseSync(p);
    const n = raw2.prepare("SELECT COUNT(*) AS n FROM agent_schema_migrations").get() as { n: number };
    raw2.close();
    assert.equal(n.n, MIGRATIONS.length);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("迁移：库的版本比代码新（回退了镜像）要拒绝启动，并说清楚原因", () => {
  const db = new DatabaseSync(":memory:");
  migrate(db, MIGRATIONS, () => 1);
  db.prepare("INSERT INTO agent_schema_migrations (version, name, checksum, applied_at) VALUES (?, ?, ?, ?)").run(999, "from-the-future", "x", 1);
  assert.throws(() => migrate(db, MIGRATIONS, () => 2), /版本 999.*比当前代码.*新/s);
});

test("迁移：已应用过的迁移被改过要拒绝（校验和不一致）", () => {
  const db = new DatabaseSync(":memory:");
  migrate(db, [{ version: 1, name: "a", sql: "CREATE TABLE t (x INTEGER)" }], () => 1);
  assert.throws(() => migrate(db, [{ version: 1, name: "a", sql: "CREATE TABLE t (x TEXT)" }], () => 2), /被修改过/);
});

test("迁移：某一步失败整步回滚，版本不前进，下次修好后可以接着跑", () => {
  const db = new DatabaseSync(":memory:");
  const good = { version: 1, name: "a", sql: "CREATE TABLE a (x INTEGER)" };
  const bad = { version: 2, name: "b", sql: "CREATE TABLE b (x INTEGER); INSERT INTO nope VALUES (1)" };
  assert.throws(() => migrate(db, [good, bad], () => 1), /迁移 2.*b.*失败/s);
  const applied = db.prepare("SELECT version FROM agent_schema_migrations").all() as Array<{ version: number }>;
  assert.deepEqual(applied.map((r) => r.version), [1], "第 2 步的版本记录没写进去");
  assert.equal(db.prepare("SELECT name FROM sqlite_master WHERE name = 'b'").get(), undefined, "第 2 步建的表也回滚了");
  migrate(db, [good, { version: 2, name: "b", sql: "CREATE TABLE b (x INTEGER)" }], () => 3);
  assert.ok(db.prepare("SELECT name FROM sqlite_master WHERE name = 'b'").get());
});

test("迁移：版本号必须从 1 开始连续", () => {
  const db = new DatabaseSync(":memory:");
  assert.throws(() => migrate(db, [{ version: 2, name: "x", sql: "SELECT 1" }], () => 1), /连续/);
  assert.throws(() => migrate(db, [{ version: 1, name: "x", sql: "SELECT 1" }, { version: 3, name: "y", sql: "SELECT 1" }], () => 1), /连续/);
});

// ---- 会话 ----

test("会话：创建、读取、按用户隔离；ID 是雪花（十进制字符串，超过 2^53）", withStore((s) => {
  const a = newSession(s, { title: "角色设计" });
  assert.match(a.id, /^[1-9][0-9]{15,18}$/);
  assert.ok(BigInt(a.id) > 2n ** 53n, "精度不能丢");
  assert.equal(a.lastSeq, 0);
  const got = s.getSession("1", a.id);
  assert.deepEqual(got, a);
  assert.throws(() => s.getSession("2", a.id), NotFoundError, "别人的会话和不存在一样");
  assert.throws(() => s.getSession("1", "123"), NotFoundError);
}));

test("会话：列表按最近更新在前（同一时刻按 ID 倒序），只含这个用户在这个画布上的，不含已删除的", withStore((s, c) => {
  const first = newSession(s, { title: "一" });
  c.tick();
  const second = newSession(s, { title: "二" });
  c.tick();
  const third = newSession(s, { title: "三" });
  newSession(s, { canvasId: "8" });
  newSession(s, { userId: "2" });
  c.tick(10);
  s.updateSession("1", first.id, { title: "一（改名）" }); // 更新时间最新，排最前
  assert.deepEqual(s.listSessions("1", "7").map((x) => x.title), ["一（改名）", "三", "二"]);
  assert.equal(s.countSessions("1", "7"), 3);
  s.deleteSession("1", second.id);
  assert.deepEqual(s.listSessions("1", "7").map((x) => x.id), [first.id, third.id]);
  assert.equal(s.countSessions("1", "7"), 2, "软删除的不计数");
}));

test("会话：更新只改给出的字段并刷新更新时间；别人的、不存在的、已删除的是 NotFound", withStore((s, c) => {
  const a = newSession(s);
  c.tick(5);
  s.updateSession("1", a.id, { mode: "prompt", modelKey: "deepseek" });
  const got = s.getSession("1", a.id);
  assert.equal(got.mode, "prompt");
  assert.equal(got.modelKey, "deepseek");
  assert.equal(got.title, "新会话", "没给的字段不动");
  assert.equal(got.updatedAt, c.t);
  assert.throws(() => s.updateSession("2", a.id, { title: "x" }), NotFoundError);
  assert.throws(() => s.updateSession("1", "999", { title: "x" }), NotFoundError);
  s.deleteSession("1", a.id);
  assert.throws(() => s.getSession("1", a.id), NotFoundError);
  assert.throws(() => s.updateSession("1", a.id, { title: "x" }), NotFoundError);
  assert.throws(() => s.deleteSession("1", a.id), NotFoundError, "重复删除");
  assert.throws(() => s.deleteSession("2", a.id), NotFoundError);
}));

// ---- 事件 ----

test("事件：序号从 1 起连续递增，按会话独立计数，会话的 lastSeq 同步", withStore((s) => {
  const a = newSession(s);
  const b = newSession(s);
  const e1 = s.appendEvent(a.id, "0", "run.started", { x: 1 });
  const e2 = s.appendEvent(a.id, "5", "message.delta", { text: "你" });
  const f1 = s.appendEvent(b.id, "0", "run.started", {});
  assert.deepEqual([e1.seq, e2.seq, f1.seq], [1, 2, 1]);
  assert.equal(e2.runId, "5");
  assert.deepEqual(e2.payload, { text: "你" }, "中文和嵌套 JSON 原样存取");
  assert.ok(BigInt(e2.id) > BigInt(e1.id), "事件 ID 随时间递增");
  assert.equal(s.getSession("1", a.id).lastSeq, 2);
  assert.equal(s.getSession("1", b.id).lastSeq, 1);
  assert.throws(() => s.appendEvent("404", "0", "x", {}), NotFoundError, "会话不存在");
  s.deleteSession("1", b.id);
  assert.throws(() => s.appendEvent(b.id, "0", "x", {}), NotFoundError, "已删除的会话不再接收事件");
}));

test("事件：listEvents 按序号升序取 afterSeq 之后的，limit 默认 50；listEventsBefore 取更早的最新 N 条且升序", withStore((s) => {
  const a = newSession(s);
  for (let i = 1; i <= 120; i++) s.appendEvent(a.id, "0", "message.delta", { i });
  assert.deepEqual(s.listEvents(a.id, 0).map((e) => e.seq), Array.from({ length: 50 }, (_, i) => i + 1));
  assert.deepEqual(s.listEvents(a.id, 115, 100).map((e) => e.seq), [116, 117, 118, 119, 120]);
  assert.deepEqual(s.listEvents(a.id, 120), []);
  assert.deepEqual(s.listEventsBefore(a.id, 0, 3).map((e) => e.seq), [118, 119, 120], "beforeSeq<=0 不设上界");
  assert.deepEqual(s.listEventsBefore(a.id, 10, 3).map((e) => e.seq), [7, 8, 9]);
  assert.deepEqual(s.listEventsBefore(a.id, 3, 10).map((e) => e.seq), [1, 2]);
  assert.equal(s.listEventsBefore(a.id, 0, 0).length, 50, "limit<=0 取默认值");
}));

test("事件：追加中途失败整体回滚，序号不会留下空洞", () => {
  const dir = tmpDir();
  try {
    const c = clock();
    // 第二次生成的 ID 和第一次相同，让第二条事件的 INSERT 在序号已经加一之后失败
    const real = createSnowflake({ workerId: 1, now: c.now });
    let calls = 0;
    let dup = 0n;
    const ids = {
      ...real,
      next: () => {
        calls++;
        if (calls === 2) return (dup = real.next()); // 第 1 个 ID 给会话，第 2 个给第一条事件
        if (calls === 3) return dup; // 第二条事件撞了第一条事件的 ID
        return real.next();
      },
    };
    const s = openStore(join(dir, "a.sqlite"), { workerId: 1, now: c.now, ids });
    const a = s.createSession({ userId: "1", canvasId: "7", title: "t", mode: "all", modelKey: "m" }); // 第 1 个 ID
    s.appendEvent(a.id, "0", "run.started", {}); // 第 2 个 ID，seq=1
    assert.throws(() => s.appendEvent(a.id, "0", "x", {}), DuplicateError); // 第 3 个 ID 撞了：整体回滚
    assert.equal(s.getSession("1", a.id).lastSeq, 1, "序号没有前进");
    const next = s.appendEvent(a.id, "0", "y", {});
    assert.equal(next.seq, 2, "下一条接着用 2，没有空洞");
    assert.deepEqual(s.listEvents(a.id, 0).map((e) => e.seq), [1, 2]);
    s.close();
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

// ---- 运行 ----

test("运行：创建后能读出所有字段，别人的运行是 NotFound", withStore((s, c) => {
  const a = newSession(s);
  const r = newRun(s, a.id, { budgetCredits: 30, maxSteps: 25, promptVersion: 3, toolsHash: "abc123", buildSha: "d".repeat(40), harnessKey: "h1" });
  assert.equal(r.status, "running");
  assert.equal(r.budgetCredits, 30);
  assert.equal(r.maxSteps, 25);
  assert.equal(r.steps, 0);
  assert.equal(r.spentCredits, 0);
  assert.equal(r.contextTokens, 0);
  assert.equal(r.endedAt, null);
  assert.equal(r.createdAt, c.t);
  assert.deepEqual(s.getRun("1", r.id), r);
  assert.equal(newRun(s, a.id, { canvasId: "8" }).maxSteps, 40, "步数上限默认 40，和 Go 一致");
  assert.throws(() => s.getRun("2", r.id), NotFoundError);
  assert.throws(() => newRun(s, "404", { canvasId: "9" }), NotFoundError, "会话不存在");
}));

test("运行：同一画布同时只能有一个活跃运行（并发由唯一索引兜底），终态之后可以再开", withStore((s) => {
  const a = newSession(s);
  const r1 = newRun(s, a.id, { status: "queued" });
  assert.throws(() => newRun(s, a.id), DuplicateError);
  assert.equal(s.activeRun("7")?.id, r1.id);
  assert.equal(s.activeRun("8"), undefined);
  for (const active of ["queued", "running", "waiting_approval", "waiting_input"]) {
    s.updateRunIf(r1.id, ["queued", "running", "waiting_approval", "waiting_input"], { status: active });
    assert.equal(s.activeRun("7")?.id, r1.id, `${active} 占用画布`);
  }
  s.updateRunIf(r1.id, ["waiting_input"], { status: "succeeded", endedAt: 1234 });
  assert.equal(s.activeRun("7"), undefined);
  const r2 = newRun(s, a.id);
  assert.notEqual(r2.id, r1.id);
  // 终态的运行想回到活跃状态（继续运行），但画布已被占：Duplicate
  assert.throws(() => s.updateRunIf(r1.id, ["succeeded"], { status: "running" }), DuplicateError);
  for (const parked of ["budget_exhausted", "step_limit", "timeout", "interrupted", "expired", "failed", "canceled"]) {
    s.updateRunIf(r2.id, ["running"], { status: parked });
    assert.equal(s.activeRun("7"), undefined, `${parked} 不占用画布`);
    s.updateRunIf(r2.id, [parked], { status: "running" });
  }
}));

test("运行：updateRunIf 是 CAS，状态不在 from 里返回 StateConflict；成功返回更新后的整行", withStore((s, c) => {
  const a = newSession(s);
  const r = newRun(s, a.id);
  c.tick(7);
  const got = s.updateRunIf(r.id, ["running"], { status: "failed", errorCode: "E_X", errorMessage: "出错了", endedAt: c.t });
  assert.equal(got.status, "failed");
  assert.equal(got.errorCode, "E_X");
  assert.equal(got.errorMessage, "出错了");
  assert.equal(got.endedAt, c.t);
  assert.equal(got.updatedAt, c.t);
  assert.equal(got.steps, 0, "没给的字段不动");
  assert.throws(() => s.updateRunIf(r.id, ["running", "queued"], { status: "canceled" }), StateConflictError, "已经不是 running 了");
  assert.throws(() => s.updateRunIf("404", ["running"], { status: "canceled" }), StateConflictError, "不存在也是冲突（和 Go 一致：已被处理）");
  assert.equal(s.getRun("1", r.id).status, "failed", "冲突不改任何东西");
  assert.throws(() => s.updateRunIf(r.id, [], { status: "running" }), /from/, "from 为空是编码错误");
}));

test("运行：可更新的字段白名单；未知字段、空更新是编码错误", withStore((s) => {
  const a = newSession(s);
  const r = newRun(s, a.id);
  const ok = s.updateRunIf(r.id, ["running"], { leaseUntil: 99, mode: "prompt", budgetCredits: 50, spentCredits: 3, steps: 2, contextTokens: 4000 });
  assert.deepEqual([ok.leaseUntil, ok.mode, ok.budgetCredits, ok.spentCredits, ok.steps, ok.contextTokens], [99, "prompt", 50, 3, 2, 4000]);
  assert.throws(() => s.updateRunIf(r.id, ["running"], { id: "1" } as never), /不能更新/);
  assert.throws(() => s.updateRunIf(r.id, ["running"], { "status = 'x', user_id": "9" } as never), /不能更新/, "字段名不会被拼进 SQL");
  assert.throws(() => s.updateRunIf(r.id, ["running"], {}), /没有要更新的字段/);
}));

test("运行：addRunUsage 原子累加，setRunContextTokens 覆盖；运行不存在是 NotFound", withStore((s) => {
  const a = newSession(s);
  const r = newRun(s, a.id);
  s.addRunUsage(r.id, 1, 5);
  s.addRunUsage(r.id, 2, 7);
  s.setRunContextTokens(r.id, 12_000);
  s.setRunContextTokens(r.id, 9_000);
  const got = s.getRun("1", r.id);
  assert.deepEqual([got.steps, got.spentCredits, got.contextTokens], [3, 12, 9_000]);
  assert.throws(() => s.addRunUsage("404", 1, 1), NotFoundError);
  assert.throws(() => s.setRunContextTokens("404", 1), NotFoundError);
}));

test("运行：interruptActiveRuns 只处理 queued 和 running，等审批、等回答的保持原样", withStore((s) => {
  const a = newSession(s);
  const ids: Record<string, string> = {};
  for (const [i, status] of (["queued", "running", "waiting_approval", "waiting_input", "succeeded"] as const).entries()) {
    ids[status] = newRun(s, a.id, { canvasId: String(100 + i), status }).id;
  }
  assert.equal(s.interruptActiveRuns(), 2);
  const st = (k: string) => s.getRun("1", ids[k]).status;
  assert.deepEqual(["queued", "running", "waiting_approval", "waiting_input", "succeeded"].map(st), ["interrupted", "interrupted", "waiting_approval", "waiting_input", "succeeded"]);
  assert.equal(s.interruptActiveRuns(), 0);
}));

// ---- 审批 ----

test("审批：创建、读取、按用户隔离；JSON 字段默认空对象；按运行列出", withStore((s, c) => {
  const a = newSession(s);
  const r = newRun(s, a.id);
  const ap = s.createApproval({
    runId: r.id, sessionId: a.id, canvasId: "7", userId: "1", kind: "generate", toolCallId: "call_1",
    payload: { items: [{ nodeId: "n1" }] }, quoteCredits: 24, status: "pending", expiresAt: c.t + 86_400_000,
  });
  assert.deepEqual(ap.decision, {});
  assert.deepEqual(ap.result, {});
  assert.equal(ap.decidedAt, null);
  assert.deepEqual(s.getApproval("1", ap.id), ap);
  assert.throws(() => s.getApproval("2", ap.id), NotFoundError);
  c.tick();
  const ap2 = s.createApproval({ runId: r.id, sessionId: a.id, canvasId: "7", userId: "1", kind: "ask", toolCallId: "call_2", payload: {}, quoteCredits: 0, status: "pending", expiresAt: c.t });
  assert.deepEqual(s.listApprovals(r.id).map((x) => x.id), [ap.id, ap2.id], "按创建先后");
  assert.deepEqual(s.listApprovals("404"), []);
}));

test("审批：updateApprovalIf 是 CAS；重复点击、已过期的得到 StateConflict；成功返回整行", withStore((s, c) => {
  const a = newSession(s);
  const r = newRun(s, a.id);
  const ap = s.createApproval({ runId: r.id, sessionId: a.id, canvasId: "7", userId: "1", kind: "delete", toolCallId: "c", payload: {}, quoteCredits: 0, status: "pending", expiresAt: c.t + 1 });
  c.tick(3);
  const decided = s.updateApprovalIf(ap.id, ["pending"], { status: "approved", decision: { decision: "approve" }, decidedAt: c.t });
  assert.equal(decided.status, "approved");
  assert.deepEqual(decided.decision, { decision: "approve" });
  assert.equal(decided.decidedAt, c.t);
  assert.equal(decided.updatedAt, c.t);
  assert.throws(() => s.updateApprovalIf(ap.id, ["pending"], { status: "rejected" }), StateConflictError, "重复点击");
  const done = s.updateApprovalIf(ap.id, ["approved", "partially_approved"], { status: "executed", result: { changes: 1 } });
  assert.deepEqual(done.result, { changes: 1 });
  assert.deepEqual(done.decision, { decision: "approve" }, "没给的字段不动");
  assert.throws(() => s.updateApprovalIf(ap.id, ["pending"], { id: "1" } as never), /不能更新/);
}));

test("审批：expirePendingApprovals 只把某个运行待处理的置为已失效", withStore((s, c) => {
  const a = newSession(s);
  const r1 = newRun(s, a.id);
  const r2 = newRun(s, a.id, { canvasId: "8" });
  const mk = (runId: string, status: string) =>
    s.createApproval({ runId, sessionId: a.id, canvasId: "7", userId: "1", kind: "ask", toolCallId: "c", payload: {}, quoteCredits: 0, status, expiresAt: c.t });
  const p1 = mk(r1.id, "pending");
  const p2 = mk(r1.id, "pending");
  const done = mk(r1.id, "executed");
  const other = mk(r2.id, "pending");
  assert.equal(s.expirePendingApprovals(r1.id), 2);
  assert.deepEqual([p1, p2, done, other].map((x) => s.getApproval("1", x.id).status), ["expired", "expired", "executed", "pending"]);
  assert.equal(s.expirePendingApprovals(r1.id), 0);
}));

// ---- ID 分配器和重启 ----

test("重启：数据完好；时钟被拨回去之后新 ID 仍然比库里已有的大（不会重复）", async () => {
  const dir = tmpDir();
  try {
    const p = join(dir, "a.sqlite");
    const c = clock();
    const s1 = openStore(p, { workerId: 1, now: c.now });
    const a = newSession(s1);
    const r = newRun(s1, a.id);
    const ev = s1.appendEvent(a.id, r.id, "run.started", { ok: true });
    const lastId = BigInt(ev.id);
    s1.close();

    c.t -= 60_000; // 重启后系统时钟比之前慢了一分钟
    const s2 = openStore(p, { workerId: 1, now: c.now });
    assert.deepEqual(s2.getSession("1", a.id).lastSeq, 1);
    assert.deepEqual(s2.getRun("1", r.id), r);
    assert.deepEqual(s2.listEvents(a.id, 0)[0], ev);
    const next = s2.appendEvent(a.id, r.id, "message.done", {});
    assert.ok(BigInt(next.id) > lastId, "新 ID 比库里最大的还大");
    assert.equal(next.seq, 2);
    const fresh = newSession(s2);
    assert.ok(BigInt(fresh.id) > lastId);
    s2.close();
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("崩溃：进程被 SIGKILL 杀掉后重新打开，已经确认的事件一条不丢，序号没有空洞", async () => {
  const dir = tmpDir();
  try {
    const p = join(dir, "a.sqlite");
    const s = openStore(p, { workerId: 1 });
    const a = newSession(s);
    s.close();
    const child = spawn(process.execPath, ["--disable-warning=ExperimentalWarning", "test/fixtures/append-events.ts", p, a.id], { stdio: ["ignore", "pipe", "inherit"] });
    let confirmed = 0;
    let buf = "";
    await new Promise<void>((resolve, reject) => {
      child.stdout.on("data", (d: Buffer) => {
        buf += d.toString();
        const lines = buf.split("\n");
        buf = lines.pop() ?? "";
        for (const l of lines) confirmed = Number(l);
        if (confirmed >= 300) child.kill("SIGKILL");
      });
      child.on("exit", () => resolve());
      child.on("error", reject);
    });
    const reopened = openStore(p, { workerId: 1 });
    const events = reopened.listEvents(a.id, 0, 100_000);
    assert.ok(events.length >= confirmed, `已确认的 ${confirmed} 条都在（实际 ${events.length}）`);
    events.forEach((e, i) => assert.equal(e.seq, i + 1, "序号连续，没有空洞"));
    assert.equal(reopened.getSession("1", a.id).lastSeq, events.length, "lastSeq 和最后一条事件一致");
    const next = reopened.appendEvent(a.id, "0", "after.crash", {});
    assert.equal(next.seq, events.length + 1);
    reopened.close();
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("备份恢复：backupTo 是一致的快照，恢复后数据、序号和 ID 分配都接得上", withStore(async (s, c, dbPath) => {
  const a = newSession(s);
  const r = newRun(s, a.id);
  for (let i = 0; i < 20; i++) s.appendEvent(a.id, r.id, "message.delta", { i });
  const dir = tmpDir();
  try {
    const backup = join(dir, "backup.sqlite");
    s.backupTo(backup);
    assert.throws(() => s.backupTo(backup), /已存在/, "不覆盖已有的备份文件");
    // 备份之后原库继续写
    for (let i = 0; i < 5; i++) s.appendEvent(a.id, r.id, "message.delta", { late: i });
    assert.ok(existsSync(dbPath));

    c.tick(5); // 恢复发生在备份之后
    const restored = openStore(backup, { workerId: 1, now: c.now });
    const events = restored.listEvents(a.id, 0, 1000);
    assert.equal(events.length, 20, "备份是那一刻的快照，不含之后的写入");
    assert.equal(restored.getSession("1", a.id).lastSeq, 20);
    assert.deepEqual(restored.getRun("1", r.id), r);
    const next = restored.appendEvent(a.id, r.id, "message.delta", {});
    assert.equal(next.seq, 21, "序号接着备份时的继续");
    assert.ok(BigInt(next.id) > BigInt(events[19].id));
    restored.close();
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}));

test("打开：另一个进程占着写锁时会等一会儿而不是立刻报 busy（Pi 的连接和这里共用一个文件）", async () => {
  const dir = tmpDir();
  try {
    const p = join(dir, "a.sqlite");
    const s = openStore(p, { workerId: 1 });
    const a = newSession(s);
    // 子进程拿到写锁、通知父进程、300 毫秒后提交
    const holder = spawn(process.execPath, ["--disable-warning=ExperimentalWarning", "-e", `
      const { DatabaseSync } = require("node:sqlite");
      const db = new DatabaseSync(process.argv[1]);
      db.exec("PRAGMA busy_timeout = 5000");
      db.exec("BEGIN IMMEDIATE");
      process.stdout.write("locked\\n");
      Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, 300);
      db.exec("COMMIT");
    `, p], { stdio: ["ignore", "pipe", "inherit"] });
    await new Promise<void>((resolve) => holder.stdout.once("data", () => resolve()));
    const t0 = Date.now();
    const ev = s.appendEvent(a.id, "0", "x", {}); // 要等对方提交
    assert.ok(Date.now() - t0 >= 150, `确实等了（${Date.now() - t0}ms）`);
    assert.equal(ev.seq, 1);
    await new Promise((resolve) => holder.on("exit", resolve));
    s.close();
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

// ---- 引擎要用的几处 ----

test("会话绑定 Pi 的会话 id：绑定后能读出，不改更新时间（不影响列表顺序）；别人的、不存在的是 NotFound", withStore((s, c) => {
  const a = newSession(s);
  assert.equal(a.conversationId, "", "新会话还没有 Pi 会话");
  c.tick(50);
  s.bindConversation(a.id, "17");
  const got = s.getSession("1", a.id);
  assert.equal(got.conversationId, "17");
  assert.equal(got.updatedAt, a.updatedAt, "绑定是内部动作，不能让会话在列表里跳到最前");
  assert.throws(() => s.bindConversation("404", "1"), NotFoundError);
  s.deleteSession("1", a.id);
  assert.throws(() => s.bindConversation(a.id, "1"), NotFoundError, "已删除的会话");
}));

test("运行补丁可以写版本字段和步数上限（创建后由运行准备确定）；步数上限追加一轮", withStore((s) => {
  const a = newSession(s);
  const r = newRun(s, a.id, { status: "queued" });
  const got = s.updateRunIf(r.id, ["queued"], { promptVersion: 15, toolsHash: "04c13780543e", buildSha: "abc1234", harnessKey: "k1", maxSteps: 80 });
  assert.deepEqual([got.promptVersion, got.toolsHash, got.buildSha, got.harnessKey, got.maxSteps], [15, "04c13780543e", "abc1234", "k1", 80]);
}));

// ---- 评价 ----

test("评价：每个运行每人一条，改选覆盖（保留创建时间、更新更新时间）；取消返回有没有删到；原因按数组存取", withStore((s, c) => {
  const a = newSession(s);
  const r = newRun(s, a.id, { status: "succeeded" });
  assert.equal(s.getFeedback(r.id, "1"), undefined);
  const first = s.upsertFeedback({ runId: r.id, userId: "1", rating: -1, reasons: ["misread", "slow"], comment: "我要 6 个" });
  assert.deepEqual([first.rating, first.reasons, first.comment], [-1, ["misread", "slow"], "我要 6 个"]);
  c.tick(10);
  const again = s.upsertFeedback({ runId: r.id, userId: "1", rating: 1, reasons: [], comment: "" });
  assert.deepEqual([again.rating, again.reasons], [1, []]);
  assert.equal(again.createdAt, first.createdAt);
  assert.equal(again.updatedAt, c.t);
  assert.deepEqual(s.getFeedback(r.id, "1"), again);
  s.upsertFeedback({ runId: r.id, userId: "2", rating: -1, reasons: ["other"], comment: "" });
  assert.equal(s.getFeedback(r.id, "1")!.rating, 1, "别人的评价是另一条");
  assert.equal(s.deleteFeedback(r.id, "1"), true);
  assert.equal(s.deleteFeedback(r.id, "1"), false);
  assert.ok(s.getFeedback(r.id, "2"));
  assert.throws(() => s.upsertFeedback({ runId: "404", userId: "1", rating: 1, reasons: [], comment: "" }), NotFoundError, "运行不存在");
}));

// ---- 观测读取（Go 的后台只读，不带用户） ----

test("观测：按 id 读运行不看用户；按更新时间列出变过的运行（升序、不含游标本身、有上限）", withStore((s, c) => {
  const a = newSession(s, { canvasId: "7" });
  const b = newSession(s, { userId: "2", canvasId: "8" });
  const r1 = newRun(s, a.id);
  c.tick(10);
  const r2 = s.createRun({ sessionId: b.id, canvasId: "8", userId: "2", status: "running", mode: "all" });
  assert.equal(s.getRunById(r2.id).userId, "2", "不看用户");
  assert.throws(() => s.getRunById("999"), NotFoundError);

  c.tick(10);
  s.updateRunIf(r1.id, ["running"], { status: "succeeded", endedAt: c.now() }); // r1 最后变
  const all = s.listRunsUpdatedAfter(0, 10);
  assert.deepEqual(all.map((r) => [r.id, r.status]), [[r2.id, "running"], [r1.id, "succeeded"]], "按更新时间升序");
  assert.equal(all[0].updatedAt, r2.updatedAt);
  assert.deepEqual(s.listRunsUpdatedAfter(r2.updatedAt, 10).map((r) => r.id), [r1.id], "严格大于游标");
  assert.equal(s.listRunsUpdatedAfter(0, 1).length, 1, "有上限");
  assert.deepEqual(s.listRunsUpdatedAfter(c.now(), 10), []);
}));

test("观测：touchRun 只刷新更新时间（评价、撤销之后让 Go 重新汇总）", withStore((s, c) => {
  const sess = newSession(s);
  const run = newRun(s, sess.id);
  s.updateRunIf(run.id, ["running"], { status: "succeeded", endedAt: c.now() });
  const before = s.getRunById(run.id);
  c.tick(500);
  s.touchRun(run.id);
  const after = s.getRunById(run.id);
  assert.equal(after.updatedAt, before.updatedAt + 500);
  assert.deepEqual({ ...after, updatedAt: 0 }, { ...before, updatedAt: 0 }, "其他字段不变");
  assert.throws(() => s.touchRun("999"), NotFoundError);
}));

test("观测：一次运行的事件（升序）；会话是否被删；同会话更早几轮的用户消息和最终回复", withStore((s) => {
  const sess = newSession(s);
  const runs = [newRun(s, sess.id, { status: "succeeded" }), newRun(s, sess.id, { status: "succeeded" }), newRun(s, sess.id, { status: "succeeded" }), newRun(s, sess.id)];
  for (const [i, r] of runs.entries()) {
    s.appendEvent(sess.id, r.id, "message.user", { text: `问${i}` });
    s.appendEvent(sess.id, r.id, "tool.start", { id: `t${i}` });
    s.appendEvent(sess.id, r.id, "message.done", { text: `答${i}` });
  }
  s.appendEvent(sess.id, "0", "message.user", { text: "会话级，不属于任何运行" });
  assert.deepEqual(s.listRunEvents(runs[3].id).map((e) => [e.seq, e.type]), [[10, "message.user"], [11, "tool.start"], [12, "message.done"]]);
  assert.deepEqual(s.listRunEvents("999"), []);

  // 上下文：只取前面 2 轮（不含本轮、不含更早的），只要用户消息和最终回复
  const ctx = s.listContextEvents(sess.id, runs[3].id, 2);
  assert.deepEqual(ctx.map((e) => [e.runId === runs[1].id ? 1 : 2, e.type]), [[1, "message.user"], [1, "message.done"], [2, "message.user"], [2, "message.done"]]);
  assert.deepEqual(s.listContextEvents(sess.id, runs[0].id, 2), [], "第一轮没有更早的");

  assert.equal(s.isSessionDeleted(sess.id), false);
  s.deleteSession("1", sess.id);
  assert.equal(s.isSessionDeleted(sess.id), true);
  assert.equal(s.isSessionDeleted("999"), true, "不存在按已删除处理");
}));
