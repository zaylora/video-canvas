import assert from "node:assert/strict";
import { spawn, type ChildProcess } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { createModels } from "@earendil-works/pi-ai";
import { Harness, createRegistry } from "@earendil-works/pi-durable";
import { BACKGROUND_CONTEXT as context } from "@earendil-works/chord/context";
import { openNodeSqliteStorage } from "@earendil-works/pi-durable/storage/sqlite/node";
import { AgentEngine, type PublishedEvent } from "../src/engine/engine.ts";
import { createIdCodec } from "../src/ids.ts";
import { createPlatform } from "../src/platform-client.ts";
import { openStore, type AgentStore } from "../src/store/store.ts";
import { fakeGo, type FakeGoOptions } from "./fake-go.ts";

const TOKEN = "service-token-0123456789abcdef0123456789";
const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));
const never = () => new Promise<never>(() => {});

interface Files {
  dir: string;
  db: string;
  go: Awaited<ReturnType<typeof fakeGo>>;
  store: AgentStore;
  sessionId: string;
  events: PublishedEvent[];
  openEngine(over?: Partial<Parameters<typeof AgentEngine.open>[0]>): Promise<AgentEngine>;
  statuses(): string[];
  spawnChild(action?: "start" | "recover", message?: string, sessionId?: string): Promise<{ child: ChildProcess; runId: string }>;
}

/** 应用数据和 Pi 的数据在同一个文件里（表名前缀不同），和生产一样；假 Go 在父进程，子进程被杀后它还在 */
async function withFiles(go: FakeGoOptions, fn: (f: Files) => Promise<void>) {
  const dir = mkdtempSync(join(tmpdir(), "engine-recovery-"));
  const db = join(dir, "agent.sqlite");
  const g = await fakeGo(go);
  const store = openStore(db, { workerId: 1 });
  const session = store.createSession({ userId: "1", canvasId: "7", title: "新对话", mode: "all", modelKey: "" });
  const events: PublishedEvent[] = [];
  const engines: AgentEngine[] = [];
  const children: ChildProcess[] = [];
  const f: Files = {
    dir, db, go: g, store, sessionId: session.id, events,
    openEngine: async (over = {}) => {
      const e = await AgentEngine.open({
        store, platform: createPlatform({ baseUrl: g.url, serviceToken: TOKEN }), baseUrl: g.url, serviceToken: TOKEN,
        storage: await openNodeSqliteStorage(db, { prefix: "pi_" }), codec: createIdCodec("test-key"), onEvent: (ev) => events.push(ev), leaseIntervalMs: 30, ...over,
      });
      engines.push(e);
      return e;
    },
    statuses: () => store.listEvents(session.id, 0, 1000).filter((x) => x.type === "run.status").map((x) => (x.payload as { status: string }).status),
    spawnChild: async (action = "start", message, sessionId) => {
      const child = spawn(process.execPath, ["--disable-warning=ExperimentalWarning", "test/fixtures/engine-child.ts", db, g.url, sessionId ?? session.id, action, ...(message ? [message] : [])], { stdio: ["ignore", "pipe", "inherit"] });
      children.push(child);
      const runId = await new Promise<string>((resolve, reject) => {
        child.stdout!.on("data", (d: Buffer) => { const m = /started ([\d,]+)/.exec(d.toString()); if (m) resolve(m[1]); });
        child.on("exit", () => reject(new Error("子进程提前退出")));
      });
      return { child, runId };
    },
  };
  try {
    await fn(f);
  } finally {
    for (const c of children) c.kill("SIGKILL");
    for (const e of engines) await e.close().catch(() => {});
    store.close();
    await g.close();
    rmSync(dir, { recursive: true, force: true });
  }
}

async function until(cond: () => boolean, ms = 5000) {
  for (let i = 0; i < ms / 20 && !cond(); i++) await sleep(20);
  assert.ok(cond(), "等待的条件没有满足");
}
const kill = (c: ChildProcess) => new Promise<void>((r) => { c.on("exit", () => r()); c.kill("SIGKILL"); });
const toolCalls = (f: Files) => f.go.seen.filter((s) => s.path.includes("/tools/"));

/** 第一次工具调用卡住（进程在这里被杀），之后的调用正常返回 */
function hangFirst() {
  let n = 0;
  return async () => (++n === 1 ? never() : { content: "画布有 3 个节点" });
}

test("被杀后重启，窗口内：自动续跑——先发「恢复中」，工具用同一个幂等键重放，运行接着做完", () =>
  withFiles({ turns: [{ tools: [{ id: "gc_abcdefgh_" + Buffer.from("s".repeat(1732)).toString("base64url"), name: "canvas_get_state" }] }, { text: "接上了" }], tools: { canvas_get_state: hangFirst() } }, async (f) => {
    const { child, runId } = await f.spawnChild();
    await until(() => toolCalls(f).length === 1);
    await sleep(100); // 让租约至少续过一次
    await kill(child);

    const engine = await f.openEngine({ resumeWindowMs: 60_000 });
    await engine.idle();
    const run = f.store.getRun("1", runId);
    assert.equal(run.status, "succeeded");
    assert.equal(toolCalls(f).length, 2, "工具重放了一次");
    assert.equal(toolCalls(f)[1].headers["x-idempotency-key"], toolCalls(f)[0].headers["x-idempotency-key"], "两个进程里是同一个幂等键，Go 据此不会写两遍画布");
    assert.equal(toolCalls(f)[0].body.tool_call_id, toolCalls(f)[1].body.tool_call_id, "重放仍使用同一个短关联 id");
    assert.ok(typeof toolCalls(f)[0].body.tool_call_id === "string" && toolCalls(f)[0].body.tool_call_id.length <= 64);
    // 「恢复中」只是事件：库里的状态一直是 running，画布没有被放走
    assert.deepEqual(f.statuses(), ["queued", "running", "recovering", "running", "succeeded"]);
    const types = f.store.listEvents(f.sessionId, 0, 1000).map((e) => e.type);
    assert.equal(types.filter((t) => t === "tool.start").length, 1, "重放的工具不能再发一个 tool.start（前端按它建一行）");
    assert.equal(types.filter((t) => t === "tool.end").length, 1);
    assert.equal(f.store.getSession("1", f.sessionId).lastSeq, f.store.listEvents(f.sessionId, 0, 1000).length);
    assert.match(JSON.stringify(f.go.modelRequests.at(-1)!.body.messages), /画布有 3 个节点/);
    assert.equal(f.events.filter((e) => e.type === "run.status").length, f.statuses().length - 2, "重启后发出的事件");
  }));

test("被杀后重启，超过窗口：标为中断，Pi 里没跑完的任务收掉，不会自己跑起来；用户点继续后能做完", () =>
  withFiles({ turns: [{ tools: [{ id: "c1", name: "canvas_get_state" }] }, { text: "确认过了" }], tools: { canvas_get_state: hangFirst() } }, async (f) => {
    const { child, runId } = await f.spawnChild();
    await until(() => toolCalls(f).length === 1);
    await sleep(100);
    await kill(child);
    await sleep(30); // 停机时间 > 窗口（0）

    const engine = await f.openEngine({ resumeWindowMs: 0 });
    await engine.idle();
    assert.equal(f.store.getRun("1", runId).status, "interrupted");
    const last = f.store.listEvents(f.sessionId, 0, 1000).filter((e) => e.type === "run.status").at(-1)!.payload as Record<string, unknown>;
    assert.deepEqual([last.status, last.title], ["interrupted", "运行被中断"]);
    assert.equal(toolCalls(f).length, 1, "没有自己跑起来");
    assert.equal(f.go.modelRequests.length, 1, "没有悄悄多发一次模型请求");
    assert.equal(await engine.liveTaskCount(), 0, "Pi 里没跑完的任务收掉了");
    assert.equal(f.store.activeRun("7"), undefined, "画布空出来了");

    // 点继续：模型被提醒最后一步可能已经执行
    await engine.resume("1", runId);
    await engine.idle();
    assert.equal(f.store.getRun("1", runId).status, "succeeded");
    assert.match(JSON.stringify(f.go.modelRequests.at(-1)!.body.messages), /可能已经执行/);
    assert.equal(toolCalls(f).length, 1, "继续时也不重做那一步");
  }));

test("没有 Pi 任务可续的运行（刚创建就被杀）：直接标为中断", () =>
  withFiles({ turns: [] }, async (f) => {
    // 子进程来不及开始就没了：库里只有一个 queued 的运行
    const run = f.store.createRun({ sessionId: f.sessionId, canvasId: "7", userId: "1", status: "queued", mode: "all" });
    const engine = await f.openEngine({ resumeWindowMs: 60_000 });
    assert.equal(f.store.getRun("1", run.id).status, "interrupted");
    assert.equal(f.statuses().at(-1), "interrupted");
    await engine.close();
  }));

test("等审批的运行不受重启影响：重启后用户仍可决定，审批执行、结果交给模型、运行做完", () =>
  withFiles({
    turns: [{ tools: [{ id: "call_g", name: "generate_media" }] }, { text: "已开始生成" }],
    propose: { generate: { payload: { items: [], quote_credits: 5 }, quote_credits: 5, summary: "申请生成", content: "已提交，等待决定。" } },
  }, async (f) => {
    const { child, runId } = await f.spawnChild();
    await until(() => f.store.getRun("1", runId).status === "waiting_approval");
    await kill(child);

    const engine = await f.openEngine({ resumeWindowMs: 0 });
    assert.equal(f.store.getRun("1", runId).status, "waiting_approval", "窗口只管 queued / running 的运行");
    const [approval] = f.store.listApprovals(runId);
    await engine.decide("1", approval.id, { decision: "approve" });
    await engine.idle();
    assert.equal(f.store.getRun("1", runId).status, "succeeded");
    assert.match(JSON.stringify(f.go.modelRequests.at(-1)!.body.messages), /用户批准了generate/);
  }));

test("正常停机（不是被杀）：运行留在 running，不标中断也不标失败；重启后续跑；Pi 的任务没有被记成「被打断」", () =>
  withFiles({
    turns: [{ tools: [{ id: "c1", name: "canvas_get_state" }] }, { text: "接上了" }],
    tools: { canvas_get_state: hangFirst() },
  }, async (f) => {
    const first = await f.openEngine({ resumeWindowMs: 60_000 });
    const run = await first.startRun({ userId: "1", sessionId: f.sessionId, message: "拆分镜", modelKey: "claude" });
    await until(() => toolCalls(f).length === 1);
    await first.close(); // 停机：只关闭存储，不中止运行
    assert.equal(f.store.getRun("1", run.id).status, "running");
    assert.deepEqual(f.statuses(), ["queued", "running"], "停机时没有发任何状态事件");
    assert.ok(f.store.getRun("1", run.id).leaseUntil > 0, "停机前续过租约");

    // 另开一个 Harness 只看 Pi 里的任务，确认正常停机不计「被打断」的次数
    const peek = await Harness.open(await openNodeSqliteStorage(f.db, { prefix: "pi_" }), { models: createModels(), registry: createRegistry() }, context);
    const tasks = (await peek.inspect(context)).tasks;
    assert.ok(tasks.length > 0);
    assert.ok(tasks.every((t) => !t.record.restarts), `正常停机不应计重启次数：${JSON.stringify(tasks.map((t) => t.record.restarts))}`);
    await peek.close(context);

    const second = await f.openEngine({ resumeWindowMs: 60_000 });
    await second.idle();
    assert.equal(f.store.getRun("1", run.id).status, "succeeded");
    assert.deepEqual(f.statuses(), ["queued", "running", "recovering", "running", "succeeded"]);
  }));

test("连续被杀把任务推到重启上限：不再自动续跑，标为中断（哪怕停机时间在窗口内）", () =>
  withFiles({ turns: [{ tools: [{ id: "c1", name: "canvas_get_state" }] }], tools: { canvas_get_state: never as never } }, async (f) => {
    const { child, runId } = await f.spawnChild();
    await until(() => toolCalls(f).length === 1);
    await sleep(100);
    await kill(child);
    // 之后每次重启：续跑 → 工具又卡住 → 又被杀。Pi 每次把没完成的任务记一次「被打断」，到上限（5）就不再自动续跑
    for (let cycle = 2; cycle <= 5; cycle++) {
      const r = await f.spawnChild("recover");
      await until(() => toolCalls(f).length === cycle);
      await sleep(100);
      await kill(r.child);
    }
    const engine = await f.openEngine({ resumeWindowMs: 60_000 });
    await engine.idle();
    assert.equal(f.store.getRun("1", runId).status, "interrupted", "被重启上限挡住，不再自动续跑");
    assert.equal(toolCalls(f).length, 5, "没有第 6 次");
  }));

test("一个在窗口内、一个已过期：过期的任务要收掉，否则窗口内那个运行装上的工具会把它唤醒成「幽灵」继续跑", () =>
  withFiles({
    // 先是两个子进程各一次模型请求（都调工具），然后只有窗口内的那个会再请求；多出来的第 4 条是幽灵才会用到的
    turns: [{ tools: [{ id: "a1", name: "canvas_get_state" }] }, { tools: [{ id: "b1", name: "canvas_get_state" }] }, { text: "A 接上了" }, { text: "幽灵的回答" }],
    tools: { canvas_get_state: (() => { let n = 0; return async () => (++n <= 2 ? never() : { content: "ok" }); })() },
  }, async (f) => {
    const other = f.store.createSession({ userId: "1", canvasId: "8", title: "新对话", mode: "all", modelKey: "" });
    // 一个进程里同时有两个运行（现实中只有一个进程；两个进程共用一个文件，第二个会把第一个的运行当成待恢复的）
    const { child, runId } = await f.spawnChild("start", "拆分镜", `${f.sessionId},${other.id}`);
    const [aId, bId] = runId.split(",");
    await until(() => toolCalls(f).length === 2);
    await sleep(100);
    await kill(child);
    f.store.touchLease([bId], Date.now() - 3600_000); // B 的租约是一小时前的：停机时间超过窗口

    const engine = await f.openEngine({ resumeWindowMs: 60_000 });
    await engine.idle();
    await sleep(150);
    assert.equal(f.store.getRun("1", aId).status, "succeeded", "窗口内的接着做完");
    assert.equal(f.store.getRun("1", bId).status, "interrupted", "过期的标为中断");
    assert.equal(f.go.modelRequests.length, 3, "没有幽灵：过期那个运行没有悄悄多发模型请求");
    assert.equal(toolCalls(f).length, 3, "过期运行的工具没有被重做");
    assert.equal(await engine.liveTaskCount(), 0);
  }));

test("重启前交给 Pi 的插话：重启后续跑时靠插话 id 找回提交，读到时照样发 steer.read，只读一次", () =>
  withFiles({
    turns: [{ tools: [{ id: "c1", name: "canvas_get_state" }] }, { text: "接上了" }],
    tools: { canvas_get_state: hangFirst() },
  }, async (f) => {
    const first = await f.openEngine({ resumeWindowMs: 60_000 });
    const run = await first.startRun({ userId: "1", sessionId: f.sessionId, message: "拆分镜", modelKey: "claude" });
    await until(() => toolCalls(f).length === 1);
    await first.interject("1", run.id, "再暖一点");
    await first.close(); // 停机：插话还在 Pi 的收件箱里，没有被读到

    const second = await f.openEngine({ resumeWindowMs: 60_000 });
    await second.idle();
    const events = f.store.listEvents(f.sessionId, 0, 1000);
    const steer = events.find((e) => e.type === "message.user" && (e.payload as { steer?: boolean }).steer)!;
    const reads = events.filter((e) => e.type === "steer.read");
    assert.deepEqual(reads.map((e) => e.payload), [{ ids: [(steer.payload as { id: string }).id] }]);
    assert.equal(events.filter((e) => e.type === "steer.dropped").length, 0);
    const seen = f.go.modelRequests.map((q) => JSON.stringify(q.body.messages)).filter((m) => m.includes("再暖一点"));
    assert.ok(seen.length >= 1, "续跑后的请求带上了插话");
    assert.equal(f.store.getRun("1", run.id).status, "succeeded");
  }));
